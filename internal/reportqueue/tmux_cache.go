package reportqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	defaultTmuxCacheTTL = 30 * time.Second
	maxTmuxCacheEntries = 256
)

type tmuxCacheFile struct {
	Version int                       `json:"version"`
	Panes   map[string]tmuxCacheEntry `json:"panes"`
}

type tmuxCacheEntry struct {
	UpdatedAt time.Time            `json:"updated_at"`
	Tmux      registry.TmuxContext `json:"tmux"`
}

func (q Queue) StoreTmuxContext(ctx context.Context, tmux registry.TmuxContext, now time.Time) error {
	if tmux.PaneID == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checking context: %w", err)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := ensureQueueDirs(q); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checking context: %w", err)
	}
	lock, err := tryOpenQueueLock(q.tmuxCacheLockPath())
	if err != nil {
		return fmt.Errorf("locking tmux cache: %w", err)
	}
	cache, err := q.loadTmuxCache()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return closeTmuxCacheLock(lock, fmt.Errorf("loading tmux cache: %w", err))
	}
	if cache.Panes == nil {
		cache.Panes = make(map[string]tmuxCacheEntry)
	}
	pruneTmuxCache(&cache, now)
	cache.Version = 1
	cache.Panes[tmuxCacheKey(tmux)] = tmuxCacheEntry{UpdatedAt: now.UTC(), Tmux: tmux}
	pruneTmuxCache(&cache, now)

	if err := ctx.Err(); err != nil {
		return closeTmuxCacheLock(lock, fmt.Errorf("checking context: %w", err))
	}

	return closeTmuxCacheLock(lock, writeJSONAtomic(q.tmuxCachePath(), cache))
}

func closeTmuxCacheLock(lock *queueLock, operationErr error) error {
	if closeErr := lock.Close(); closeErr != nil {
		return errors.Join(operationErr, closeErr)
	}

	return operationErr
}

func pruneTmuxCache(cache *tmuxCacheFile, now time.Time) {
	for key, entry := range cache.Panes {
		if now.Sub(entry.UpdatedAt) > defaultTmuxCacheTTL {
			delete(cache.Panes, key)
		}
	}
	if len(cache.Panes) <= maxTmuxCacheEntries {
		return
	}

	keys := make([]string, 0, len(cache.Panes))
	for key := range cache.Panes {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i int, j int) bool {
		left := cache.Panes[keys[i]]
		right := cache.Panes[keys[j]]
		if left.UpdatedAt.Equal(right.UpdatedAt) {
			return keys[i] < keys[j]
		}

		return left.UpdatedAt.Before(right.UpdatedAt)
	})
	for _, key := range keys[:len(cache.Panes)-maxTmuxCacheEntries] {
		delete(cache.Panes, key)
	}
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
