package reportqueue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type StatusResult struct {
	Root               string    `json:"root"`
	Pending            int       `json:"pending"`
	Ready              int       `json:"ready"`
	Deferred           int       `json:"deferred"`
	Processing         int       `json:"processing"`
	StaleLeases        int       `json:"stale_leases"`
	Retries            int       `json:"retries"`
	NextRetryAt        time.Time `json:"next_retry_at,omitzero"`
	Dead               int       `json:"dead"`
	Invalid            int       `json:"invalid"`
	OldestPendingAt    time.Time `json:"oldest_pending_at,omitzero"`
	OldestProcessingAt time.Time `json:"oldest_processing_at,omitzero"`
	OldestBacklogAt    time.Time `json:"oldest_backlog_at,omitzero"`
	OldestDeadAt       time.Time `json:"oldest_dead_at,omitzero"`
}

func (q Queue) Status(ctx context.Context) (StatusResult, error) {
	if err := ctx.Err(); err != nil {
		return StatusResult{}, fmt.Errorf("checking context: %w", err)
	}
	now := time.Now().UTC()
	status := StatusResult{Root: q.root, Pending: 0, Ready: 0, Deferred: 0, Processing: 0, StaleLeases: 0, Retries: 0, NextRetryAt: time.Time{}, Dead: 0, Invalid: 0, OldestPendingAt: time.Time{}, OldestProcessingAt: time.Time{}, OldestBacklogAt: time.Time{}, OldestDeadAt: time.Time{}}
	if err := q.inspectPendingStatus(ctx, now, &status); err != nil {
		return StatusResult{}, err
	}
	if err := q.inspectProcessingStatus(ctx, now, &status); err != nil {
		return StatusResult{}, err
	}
	if err := q.inspectDeadStatus(ctx, &status); err != nil {
		return StatusResult{}, err
	}
	status.OldestBacklogAt = minNonZero(status.OldestPendingAt, status.OldestProcessingAt)

	return status, nil
}

func (q Queue) inspectPendingStatus(ctx context.Context, now time.Time, status *StatusResult) error {
	matches, err := filepath.Glob(filepath.Join(q.pendingDir(), "*.json"))
	if err != nil {
		return fmt.Errorf("listing pending queue status: %w", err)
	}
	for _, path := range matches {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("checking context: %w", err)
		}
		status.Pending++
		envelope, readErr := readEnvelope(path)
		if readErr != nil {
			updateOldest(status, "pending", fileModTime(path))
			continue
		}
		timestamp := envelope.CreatedAt
		if timestamp.IsZero() {
			timestamp = fileModTime(path)
		}
		updateOldest(status, "pending", timestamp)
		if envelope.Attempt > 0 {
			status.Retries++
		}
		if envelope.NextAttemptAt.IsZero() || !envelope.NextAttemptAt.After(now) {
			status.Ready++
			continue
		}
		status.Deferred++
		if status.NextRetryAt.IsZero() || envelope.NextAttemptAt.Before(status.NextRetryAt) {
			status.NextRetryAt = envelope.NextAttemptAt
		}
	}

	return nil
}

func (q Queue) inspectProcessingStatus(ctx context.Context, now time.Time, status *StatusResult) error {
	matches, err := filepath.Glob(filepath.Join(q.processingDir(), "*.json"))
	if err != nil {
		return fmt.Errorf("listing processing queue status: %w", err)
	}
	for _, path := range matches {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("checking context: %w", err)
		}
		status.Processing++
		envelope, readErr := readEnvelope(path)
		if readErr != nil {
			updateOldest(status, "processing", fileModTime(path))
			continue
		}
		timestamp := envelope.CreatedAt
		if timestamp.IsZero() {
			timestamp = fileModTime(path)
		}
		updateOldest(status, "processing", timestamp)
		if envelope.Attempt > 0 {
			status.Retries++
		}
		if processingLeaseExpired(envelope, now, defaultLeaseTimeout) {
			status.StaleLeases++
		}
	}

	return nil
}

func (q Queue) inspectDeadStatus(ctx context.Context, status *StatusResult) error {
	entries, err := filepath.Glob(filepath.Join(q.deadDir(), "*"))
	if err != nil {
		return fmt.Errorf("listing dead queue status: %w", err)
	}
	for _, path := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("checking context: %w", err)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("statting dead queue item: %w", statErr)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		extension := filepath.Ext(path)
		if extension != ".json" && extension != ".invalid" {
			continue
		}
		status.Dead++
		if filepath.Ext(path) == ".invalid" {
			status.Invalid++
		}
		updateOldest(status, "dead", info.ModTime())
	}

	return nil
}

func updateOldest(status *StatusResult, queue string, timestamp time.Time) {
	if timestamp.IsZero() {
		return
	}
	switch queue {
	case "pending":
		if status.OldestPendingAt.IsZero() || timestamp.Before(status.OldestPendingAt) {
			status.OldestPendingAt = timestamp
		}
	case "processing":
		if status.OldestProcessingAt.IsZero() || timestamp.Before(status.OldestProcessingAt) {
			status.OldestProcessingAt = timestamp
		}
	case "dead":
		if status.OldestDeadAt.IsZero() || timestamp.Before(status.OldestDeadAt) {
			status.OldestDeadAt = timestamp
		}
	}
}

func minNonZero(values ...time.Time) time.Time {
	var minimum time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if minimum.IsZero() || value.Before(minimum) {
			minimum = value
		}
	}

	return minimum
}

func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}

	return info.ModTime()
}
