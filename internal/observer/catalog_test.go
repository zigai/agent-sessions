package observer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeCatalogArrayAndEnvelope(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`[{"harness":"claude","session_id":"one","current":true}]`,
		`{"sessions":[{"harness":"goose","session_id":"two","current":false}]}`,
	} {
		entries, err := decodeCatalog([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].SessionID == "" {
			t.Fatalf("unexpected entries: %#v", entries)
		}
	}
}

func TestDefaultCatalogListRejectsUnknownHarness(t *testing.T) {
	t.Setenv(catalogJSONEnv, `[{"harness":"unknown","session_id":"bad"}]`)
	t.Setenv(catalogFileEnv, "")
	if _, err := DefaultCatalogList(context.Background()); err == nil {
		t.Fatal("expected unknown harness error")
	}
}

func TestDefaultCatalogListEmptyWithoutConfiguration(t *testing.T) {
	t.Setenv(catalogJSONEnv, "")
	t.Setenv(catalogFileEnv, "")
	entries, err := DefaultCatalogList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if entries != nil {
		t.Fatalf("expected nil catalog, got %#v", entries)
	}
}

func TestDecodeCatalogRejectsNull(t *testing.T) {
	t.Parallel()

	if _, err := decodeCatalog([]byte("null")); !errors.Is(err, errCatalogSessionsType) {
		t.Fatalf("decodeCatalog() error = %v, want %v", err, errCatalogSessionsType)
	}
}

func TestDefaultCatalogListRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxCatalogBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(catalogJSONEnv, "")
	t.Setenv(catalogFileEnv, path)

	_, err := DefaultCatalogList(context.Background())
	if !errors.Is(err, errCatalogTooLarge) {
		t.Fatalf("DefaultCatalogList() error = %v, want %v", err, errCatalogTooLarge)
	}
}
