package registry

import (
	"context"
	"fmt"
	"time"
)

type Store interface {
	Report(ctx context.Context, report Report) (Session, error)
	List(ctx context.Context, filter Filter) ([]Session, error)
	Get(ctx context.Context, id string) (Session, error)
	SummaryByTmuxSession(ctx context.Context, filter Filter) ([]Summary, error)
	SummaryByTmuxSessionWithOptions(ctx context.Context, options SummaryOptions) ([]Summary, error)
	GC(ctx context.Context, deleteAfter time.Duration) (GCResult, error)
}

func OpenDefaultStore() Store {
	return NewFileStore("")
}

func SummaryByTmuxSession(ctx context.Context, filter Filter) ([]Summary, error) {
	summaries, err := OpenDefaultStore().SummaryByTmuxSession(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("summarizing default store: %w", err)
	}

	return summaries, nil
}

func SummaryByTmuxSessionWithOptions(ctx context.Context, options SummaryOptions) ([]Summary, error) {
	summaries, err := OpenDefaultStore().SummaryByTmuxSessionWithOptions(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("summarizing default store with options: %w", err)
	}

	return summaries, nil
}
