# Go Library Guide

AHT exposes four public Go packages for integrating agent tracking, registry storage, and harness management into Go tools and applications.

## Packages Overview

| Package | Import Path | Description |
|---|---|---|
| [`client`](https://pkg.go.dev/github.com/zigai/aht/pkg/client) | `github.com/zigai/aht/pkg/client` | High-level client for reading and watching sessions through the local realtime broker with durable fallback. |
| [`registry`](https://pkg.go.dev/github.com/zigai/aht/pkg/registry) | `github.com/zigai/aht/pkg/registry` | Core domain models (`Session`, `Presence`, `Activity`), filters, and storage engines (`FileStore`, `MemoryStore`). |
| [`manage`](https://pkg.go.dev/github.com/zigai/aht/pkg/manage) | `github.com/zigai/aht/pkg/manage` | Programmatic hook installation, removal, and background tracker daemon service control (`systemd` / `launchd`). |
| [`harness`](https://pkg.go.dev/github.com/zigai/aht/pkg/harness) | `github.com/zigai/aht/pkg/harness` | Supported harness catalog, alias normalization, and process-to-harness command matching. |

---

## 1. `pkg/client`

The `client` package provides a unified API to query and stream agent sessions. It connects to the realtime broker over a Unix domain socket, automatically falling back to direct disk reads against the durable `sessions.json` registry file when the broker daemon is not running.

### Reading and Filtering Sessions

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/zigai/aht/pkg/client"
	"github.com/zigai/aht/pkg/registry"
)

func main() {
	ctx := context.Background()

	// Connect to the local AHT instance (uses default store and socket paths)
	aht := client.New(client.Config{})

	// List all live sessions for Claude and Codex
	sessions, err := aht.List(ctx, registry.Filter{
		Presence: registry.PresenceLive,
		Harnesses: []registry.Harness{
			registry.HarnessClaude,
			registry.HarnessCodex,
		},
	})
	if err != nil {
		log.Fatalf("failed to list sessions: %v", err)
	}

	for _, session := range sessions {
		activity := "unknown"
		if session.Activity != nil {
			activity = string(*session.Activity)
		}
		fmt.Printf("[%s] %s (%s) — Activity: %s\n",
			session.Presence, session.Harness, session.SessionID, activity)
	}
}
```

### Streaming Realtime Updates

`Watch` requires an active broker and yields incremental state snapshots as sessions start, report activity, change presence, or terminate.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/zigai/aht/pkg/client"
	"github.com/zigai/aht/pkg/registry"
)

func main() {
	ctx := context.Background()
	aht := client.New(client.Config{})

	err := aht.Watch(ctx, registry.Filter{Presence: registry.PresenceLive}, func(snapshot registry.StateSnapshot) error {
		fmt.Printf("State revision %d (total sessions: %d):\n", snapshot.Revision, len(snapshot.Sessions))
		for _, s := range snapshot.Sessions {
			fmt.Printf("  • %s (%s) is %s\n", s.Harness, s.SessionID, s.Presence)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("watch failed: %v", err)
	}
}
```

---

## 2. `pkg/registry`

The `registry` package defines the core domain model and storage backends.

### Storage Engines

- `FileStore`: Thread-safe, durable file storage backed by JSON and file locking.
- `MemoryStore`: Fast, in-memory store ideal for unit tests or ephemeral tracking.

### In-Memory Unit Testing Example

```go
package myapp_test

import (
	"context"
	"testing"
	"time"

	"github.com/zigai/aht/pkg/registry"
)

func TestAgentTracking(t *testing.T) {
	ctx := context.Background()
	store := registry.NewMemoryStore()

	presence := registry.PresenceLive
	activity := registry.ActivityWorking

	// Record an observation
	session, err := store.Observe(ctx, registry.Observation{
		Harness:  registry.HarnessCodex,
		Source:   registry.ObservationSourceNative,
		Evidence: registry.ObservationEvidenceNativeEvent,
		Identity: registry.ObservationIdentity{
			SessionID: "test-session-1",
			Cwd:       "/workspace/repo",
		},
		Presence:   &presence,
		Activity:   &activity,
		ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("observe failed: %v", err)
	}

	if session.Presence != registry.PresenceLive {
		t.Errorf("expected presence live, got %s", session.Presence)
	}
}
```

---

## 3. `pkg/manage`

The `manage` package allows Go applications to programmatically install integrations and control the background tracker service.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/zigai/aht/pkg/manage"
	"github.com/zigai/aht/pkg/registry"
)

func main() {
	ctx := context.Background()
	mgr := manage.New(manage.Config{})

	// 1. Install Claude Code hooks
	result, err := mgr.InstallIntegration(ctx, registry.HarnessClaude, manage.IntegrationOptions{
		Force: true,
	})
	if err != nil {
		log.Fatalf("failed to install integration: %v", err)
	}
	fmt.Printf("Installed %s integration at %s (changed: %t)\n", result.Harness, result.Path, result.Changed)

	// 2. Inspect integration status
	status, err := mgr.IntegrationStatus(ctx, registry.HarnessClaude, manage.IntegrationOptions{})
	if err != nil {
		log.Fatalf("failed to inspect status: %v", err)
	}
	fmt.Printf("Claude integration status: %s (version %s)\n", status.Status, status.InstalledVersion)

	// 3. Ensure the background tracker daemon is active
	serviceStatus, err := mgr.TrackerStatus(ctx)
	if err != nil {
		log.Fatalf("failed to get tracker status: %v", err)
	}
	fmt.Printf("Tracker active: %t, enabled: %t\n", serviceStatus.Active, serviceStatus.Enabled)
}
```

---

## 4. `pkg/harness`

The `harness` package provides discovery, alias normalization, and process matching for all supported coding agents.

```go
package main

import (
	"fmt"

	"github.com/zigai/aht/pkg/harness"
	"github.com/zigai/aht/pkg/registry"
)

func main() {
	// Parse user input or CLI args into canonical harness IDs
	id, err := harness.Parse("kimi-code")
	if err == nil {
		fmt.Printf("Parsed harness: %s\n", id) // "kimi"
	}

	// Identify harness from process executable path
	if h, ok := harness.FromCommand("/usr/local/bin/codex"); ok {
		fmt.Printf("Identified harness from command: %s\n", h) // "codex"
	}

	// List all supported harnesses
	for _, supported := range harness.Supported() {
		fmt.Printf("Supported harness: %s\n", supported)
	}
}
```
