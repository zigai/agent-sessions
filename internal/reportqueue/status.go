package reportqueue

import (
	"context"
	"fmt"
	"path/filepath"
)

type StatusResult struct {
	Root       string `json:"root"`
	Pending    int    `json:"pending"`
	Processing int    `json:"processing"`
	Dead       int    `json:"dead"`
}

func (q Queue) Status(ctx context.Context) (StatusResult, error) {
	if err := ctx.Err(); err != nil {
		return StatusResult{}, fmt.Errorf("checking context: %w", err)
	}
	pending, err := countJSONFiles(q.pendingDir())
	if err != nil {
		return StatusResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return StatusResult{}, fmt.Errorf("checking context: %w", err)
	}
	processing, err := countJSONFiles(q.processingDir())
	if err != nil {
		return StatusResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return StatusResult{}, fmt.Errorf("checking context: %w", err)
	}
	dead, err := countJSONFiles(q.deadDir())
	if err != nil {
		return StatusResult{}, err
	}

	return StatusResult{Root: q.root, Pending: pending, Processing: processing, Dead: dead}, nil
}

func countJSONFiles(dir string) (int, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return 0, fmt.Errorf("listing queue status: %w", err)
	}

	return len(matches), nil
}
