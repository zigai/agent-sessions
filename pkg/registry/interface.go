package registry

import (
	"context"
	"time"
)

type Store interface {
	Observe(ctx context.Context, observation Observation) (Session, error)
	ObserveBatch(ctx context.Context, observations []Observation) ([]Session, error)
	List(ctx context.Context, filter Filter) ([]Session, error)
	Get(ctx context.Context, id string) (Session, error)
	SummaryByTmuxSession(ctx context.Context, filter Filter) ([]Summary, error)
	SummaryByTmuxSessionWithOptions(ctx context.Context, options SummaryOptions) ([]Summary, error)
	GC(ctx context.Context, deleteAfter time.Duration) (GCResult, error)
}
