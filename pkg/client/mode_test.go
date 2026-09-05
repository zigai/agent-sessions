package client_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/zigai/aht/pkg/client"
	"github.com/zigai/aht/pkg/registry"
)

func TestInvalidModeRejectsOperations(t *testing.T) {
	c := client.New(client.Config{Mode: "typo", StorePath: filepath.Join(t.TempDir(), "missing.json")})
	ctx := t.Context()
	_, listErr := c.List(ctx, registry.Filter{})
	_, getErr := c.Get(ctx, "missing")
	_, observeErr := c.Observe(ctx, registry.Observation{})
	_, batchErr := c.ObserveBatch(ctx, nil)
	_, summaryErr := c.Summary(ctx, registry.Filter{})
	_, gcErr := c.GC(ctx, 0)
	_, subscribeErr := c.Subscribe(ctx, registry.Filter{})
	for operation, err := range map[string]error{
		"list": listErr, "get": getErr, "observe": observeErr,
		"batch": batchErr, "summary": summaryErr, "gc": gcErr,
		"subscribe": subscribeErr, "ping": c.Ping(ctx),
	} {
		if !errors.Is(err, client.ErrInvalidMode) {
			t.Errorf("%s error = %v, want ErrInvalidMode", operation, err)
		}
	}
}
