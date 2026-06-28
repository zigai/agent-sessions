package reportqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zigai/agent-sessions/pkg/registry"
)

const defaultTmuxCacheTTL = 30 * time.Second

type tmuxCacheFile struct {
	Version int                       `json:"version"`
	Panes   map[string]tmuxCacheEntry `json:"panes"`
}

type tmuxCacheEntry struct {
	UpdatedAt time.Time            `json:"updated_at"`
	Tmux      registry.TmuxContext `json:"tmux"`
}

func (q Queue) StoreTmuxContext(_ context.Context, tmux registry.TmuxContext, now time.Time) error {
	if tmux.PaneID == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cache, _ := q.loadTmuxCache()
	if cache.Panes == nil {
		cache.Panes = make(map[string]tmuxCacheEntry)
	}
	cache.Version = 1
	cache.Panes[tmuxCacheKey(tmux)] = tmuxCacheEntry{UpdatedAt: now.UTC(), Tmux: tmux}

	return writeJSONAtomic(q.tmuxCachePath(), cache)
}

func (q Queue) LookupTmuxContext(reference registry.TmuxContext, now time.Time, ttl time.Duration) (registry.TmuxContext, bool) {
	if reference.PaneID == "" {
		return emptyTmuxContext(), false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		ttl = defaultTmuxCacheTTL
	}
	cache, err := q.loadTmuxCache()
	if err != nil {
		return emptyTmuxContext(), false
	}
	entry, ok := cache.Panes[tmuxCacheKey(reference)]
	if !ok {
		return emptyTmuxContext(), false
	}
	if now.Sub(entry.UpdatedAt) > ttl {
		return emptyTmuxContext(), false
	}

	return entry.Tmux, true
}

func (q Queue) tmuxCachePath() string {
	return filepath.Join(q.root, "tmux-cache.json")
}

func (q Queue) loadTmuxCache() (tmuxCacheFile, error) {
	var cache tmuxCacheFile
	envelope, err := readGenericJSON[tmuxCacheFile](q.tmuxCachePath())
	if err != nil {
		return cache, err
	}
	if envelope.Panes == nil {
		envelope.Panes = make(map[string]tmuxCacheEntry)
	}

	return envelope, nil
}

func tmuxCacheKey(tmux registry.TmuxContext) string {
	return tmux.ServerSocket + "\x00" + tmux.PaneID
}

func emptyTmuxContext() registry.TmuxContext {
	var tmux registry.TmuxContext

	return tmux
}

func readGenericJSON[T any](path string) (T, error) {
	var value T
	data, err := os.ReadFile(path)
	if err != nil {
		return value, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("parsing %s: %w", path, err)
	}

	return value, nil
}
