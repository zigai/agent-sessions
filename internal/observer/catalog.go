package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zigai/agent-sessions/v2/pkg/registry"
)

const (
	catalogJSONEnv = "AGENT_SESSIONS_CATALOG_JSON"
	catalogFileEnv = "AGENT_SESSIONS_CATALOG_FILE"
)

var (
	errCatalogEntryHarness = errors.New("catalog entry has no harness")
	errCatalogSessionsType = errors.New("catalog sessions must be an array")
)

// DefaultCatalogList reads the optional machine-readable catalog configured by
// AGENT_SESSIONS_CATALOG_JSON or AGENT_SESSIONS_CATALOG_FILE. The payload is
// either an array of catalog entries or an object with a sessions array.
func DefaultCatalogList(ctx context.Context) ([]CatalogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("catalog context: %w", err)
	}
	payload := strings.TrimSpace(os.Getenv(catalogJSONEnv))
	if payload == "" {
		path := strings.TrimSpace(os.Getenv(catalogFileEnv))
		if path == "" {
			return nil, nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // the catalog path is explicitly supplied by the user
		if err != nil {
			return nil, fmt.Errorf("reading catalog file %q: %w", path, err)
		}
		payload = string(data)
	}
	entries, err := decodeCatalog([]byte(payload))
	if err != nil {
		return nil, err
	}
	for index := range entries {
		if entries[index].Harness == "" {
			return nil, fmt.Errorf("catalog entry %d: %w", index, errCatalogEntryHarness)
		}
		if _, err := registry.NormalizeHarness(string(entries[index].Harness)); err != nil {
			return nil, fmt.Errorf("catalog entry %d: %w", index, err)
		}
	}
	return entries, nil
}

func decodeCatalog(data []byte) ([]CatalogEntry, error) {
	var entries []CatalogEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		return entries, nil
	}
	var envelope struct {
		Sessions []CatalogEntry `json:"sessions"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decoding catalog JSON: %w", err)
	}
	if envelope.Sessions == nil {
		return nil, fmt.Errorf("decoding catalog JSON: %w", errCatalogSessionsType)
	}
	return envelope.Sessions, nil
}
