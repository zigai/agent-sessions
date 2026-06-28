//go:build linux || darwin

package processinfo

import (
	"context"
	"os"
	"testing"
)

func TestCurrentProcessInfo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pid := os.Getpid()
	if got := StartIdentity(ctx, pid); got == "" {
		t.Fatal("expected current process start identity")
	}

	command, err := CommandName(ctx, pid)
	if err != nil {
		t.Fatalf("current process command name returned error: %v", err)
	}
	if command == "" {
		t.Fatal("expected current process command name")
	}
}
