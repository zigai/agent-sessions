package reportqueue

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

type EnqueueResult struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type EnqueueOptions struct {
	Now func() time.Time
}

func (q Queue) Enqueue(ctx context.Context, envelope Envelope, options EnqueueOptions) (EnqueueResult, error) {
	if err := ctx.Err(); err != nil {
		return EnqueueResult{}, fmt.Errorf("checking context: %w", err)
	}
	if err := ensureQueueDirs(q); err != nil {
		return EnqueueResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return EnqueueResult{}, fmt.Errorf("checking context: %w", err)
	}
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}
	if envelope.Version == 0 {
		envelope.Version = EnvelopeVersion
	}
	if envelope.Kind == "" {
		envelope.Kind = KindReport
	}
	if envelope.CreatedAt.IsZero() {
		envelope.CreatedAt = now
	}
	if envelope.ID == "" {
		envelope.ID = NewEnvelopeID(envelope.CreatedAt)
	}
	path := filepath.Join(q.pendingDir(), envelope.ID+".json")
	if err := writeJSONAtomic(path, envelope); err != nil {
		return EnqueueResult{}, fmt.Errorf("enqueueing report: %w", err)
	}

	return EnqueueResult{ID: envelope.ID, Path: path}, nil
}
