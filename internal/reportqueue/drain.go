package reportqueue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultLeaseTimeout    = 5 * time.Minute
	maxQueueErrorTextBytes = 512
)

var errDrainRequiresProcessor = errors.New("queue drain requires a processor")

type Processor func(context.Context, Envelope) error

type DrainOptions struct {
	MaxItems     int
	LeaseTimeout time.Duration
	Now          func() time.Time
	Processor    Processor
}

type DrainResult struct {
	Processed int  `json:"processed"`
	Succeeded int  `json:"succeeded"`
	Retried   int  `json:"retried"`
	Dead      int  `json:"dead"`
	Recovered int  `json:"recovered"`
	Locked    bool `json:"locked"`
}

type drainConfig struct {
	maxItems     int
	leaseTimeout time.Duration
	now          time.Time
	processor    Processor
}

type PermanentError struct {
	Err error
}

func (e PermanentError) Error() string {
	if e.Err == nil {
		return "permanent queue item error"
	}

	return e.Err.Error()
}

func (e PermanentError) Unwrap() error { return e.Err }

func Permanent(err error) error {
	if err == nil {
		return nil
	}

	return PermanentError{Err: err}
}

func (q Queue) Drain(ctx context.Context, options DrainOptions) (DrainResult, error) {
	config, err := newDrainConfig(options)
	if err != nil {
		return newDrainResult(), err
	}
	if err := ensureQueueDirs(q); err != nil {
		return newDrainResult(), err
	}
	lock, err := tryOpenQueueLock(q.lockPath())
	if err != nil {
		if errors.Is(err, ErrLocked) {
			result := newDrainResult()
			result.Locked = true

			return result, nil
		}

		return newDrainResult(), err
	}

	result, drainErr := q.drainLocked(ctx, config)
	if closeErr := lock.Close(); closeErr != nil {
		return result, errors.Join(drainErr, closeErr)
	}

	return result, drainErr
}

func newDrainConfig(options DrainOptions) (drainConfig, error) {
	if options.Processor == nil {
		var config drainConfig

		return config, errDrainRequiresProcessor
	}
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}
	leaseTimeout := options.LeaseTimeout
	if leaseTimeout <= 0 {
		leaseTimeout = defaultLeaseTimeout
	}

	return drainConfig{
		maxItems:     options.MaxItems,
		leaseTimeout: leaseTimeout,
		now:          now,
		processor:    options.Processor,
	}, nil
}

func newDrainResult() DrainResult {
	var result DrainResult

	return result
}

func (q Queue) drainLocked(ctx context.Context, config drainConfig) (DrainResult, error) {
	result := newDrainResult()
	recovered, err := q.recoverStaleProcessing(config.now, config.leaseTimeout)
	if err != nil {
		return result, err
	}
	result.Recovered = recovered

	for !config.maxReached(result.Processed) {
		workDone, err := q.drainPendingPass(ctx, config, &result)
		if err != nil {
			return result, err
		}
		if !workDone {
			break
		}
	}

	return result, nil
}

func (q Queue) drainPendingPass(ctx context.Context, config drainConfig, result *DrainResult) (bool, error) {
	pending, err := q.pendingItems()
	if err != nil {
		return false, err
	}
	workDone := false
	for _, path := range pending {
		if err := ctx.Err(); err != nil {
			return workDone, fmt.Errorf("draining report queue: %w", err)
		}
		if config.maxReached(result.Processed) {
			break
		}
		itemDone, err := q.drainPendingItem(ctx, path, config, result)
		if err != nil {
			return workDone, err
		}
		if itemDone {
			workDone = true
		}
	}

	return workDone, nil
}

func (q Queue) drainPendingItem(
	ctx context.Context,
	path string,
	config drainConfig,
	result *DrainResult,
) (bool, error) {
	envelope, err := readEnvelope(path)
	if err != nil {
		if moveErr := q.moveUnreadableToDead(path, err); moveErr != nil {
			return false, moveErr
		}
		result.Dead++

		return true, nil
	}
	if !envelope.NextAttemptAt.IsZero() && envelope.NextAttemptAt.After(config.now) {
		return false, nil
	}

	processingPath, envelope, err := q.claim(path, envelope, config.now)
	if err != nil {
		return false, err
	}
	result.Processed++
	processErr := config.processor(ctx, envelope)
	if processErr == nil {
		if err := removeDurable(processingPath); err != nil {
			return true, err
		}
		result.Succeeded++

		return true, nil
	}
	if isPermanent(processErr) {
		if err := q.deadLetter(processingPath, envelope, processErr); err != nil {
			return true, err
		}
		result.Dead++

		return true, nil
	}
	if err := q.retry(processingPath, envelope, processErr, config.now); err != nil {
		return true, err
	}
	result.Retried++

	return true, nil
}

func (c drainConfig) maxReached(processed int) bool {
	return c.maxItems > 0 && processed >= c.maxItems
}

func (q Queue) recoverStaleProcessing(now time.Time, leaseTimeout time.Duration) (int, error) {
	entries, err := filepath.Glob(filepath.Join(q.processingDir(), "*.json"))
	if err != nil {
		return 0, fmt.Errorf("listing processing queue: %w", err)
	}
	recovered := 0
	for _, path := range entries {
		envelope, err := readEnvelope(path)
		if err != nil {
			if moveErr := q.moveUnreadableToDead(path, err); moveErr != nil {
				return recovered, moveErr
			}
			continue
		}
		if !processingLeaseExpired(envelope, now, leaseTimeout) {
			continue
		}
		envelope.ProcessingStartedAt = time.Time{}
		envelope.WorkerPID = 0
		dest := filepath.Join(q.pendingDir(), filepath.Base(path))
		if err := writeJSONAtomic(path, envelope); err != nil {
			return recovered, err
		}
		if err := renameDurable(path, dest); err != nil {
			return recovered, err
		}
		recovered++
	}

	return recovered, nil
}

func processingLeaseExpired(envelope Envelope, now time.Time, leaseTimeout time.Duration) bool {
	if envelope.ProcessingStartedAt.IsZero() {
		return true
	}
	if envelope.WorkerPID > 0 && !processExists(envelope.WorkerPID) {
		return true
	}

	return now.Sub(envelope.ProcessingStartedAt) >= leaseTimeout
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscallKillZero(pid)
	return err == nil || os.IsPermission(err)
}

func (q Queue) pendingItems() ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(q.pendingDir(), "*.json"))
	if err != nil {
		return nil, fmt.Errorf("listing pending queue: %w", err)
	}
	sort.Strings(entries)

	return entries, nil
}

func (q Queue) claim(path string, envelope Envelope, now time.Time) (string, Envelope, error) {
	envelope.Attempt++
	envelope.ProcessingStartedAt = now
	envelope.WorkerPID = os.Getpid()
	envelope.LastError = ""
	envelope.NextAttemptAt = time.Time{}
	if err := writeJSONAtomic(path, envelope); err != nil {
		return "", envelope, err
	}
	processingPath := filepath.Join(q.processingDir(), filepath.Base(path))
	if err := renameDurable(path, processingPath); err != nil {
		return "", envelope, err
	}

	return processingPath, envelope, nil
}

func (q Queue) retry(path string, envelope Envelope, processErr error, now time.Time) error {
	envelope.LastError = safeError(processErr)
	envelope.ProcessingStartedAt = time.Time{}
	envelope.WorkerPID = 0
	envelope.NextAttemptAt = now.Add(retryDelay(envelope.Attempt))
	if err := writeJSONAtomic(path, envelope); err != nil {
		return err
	}

	return renameDurable(path, filepath.Join(q.pendingDir(), filepath.Base(path)))
}

func (q Queue) deadLetter(path string, envelope Envelope, processErr error) error {
	envelope.LastError = safeError(processErr)
	envelope.ProcessingStartedAt = time.Time{}
	envelope.WorkerPID = 0
	if err := writeJSONAtomic(path, envelope); err != nil {
		return err
	}

	return renameDurable(path, filepath.Join(q.deadDir(), filepath.Base(path)))
}

func (q Queue) moveUnreadableToDead(path string, readErr error) error {
	dest := filepath.Join(q.deadDir(), filepath.Base(path)+".invalid")
	if err := os.WriteFile(dest, []byte(safeError(readErr)+"\n"), fileMode); err != nil {
		return fmt.Errorf("writing dead queue item: %w", err)
	}
	if err := syncDir(q.deadDir()); err != nil {
		return err
	}

	return removeDurable(path)
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(attempt) * time.Second
	if delay > time.Minute {
		return time.Minute
	}

	return delay
}

func isPermanent(err error) bool {
	var permanent PermanentError
	return errors.As(err, &permanent)
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if len(text) > maxQueueErrorTextBytes {
		text = text[:maxQueueErrorTextBytes]
	}

	return text
}
