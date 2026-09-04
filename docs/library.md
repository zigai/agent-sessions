# Go Library Guide

AHT exposes public Go packages for integrating agent tracking, realtime IPC, multiplexer discovery, registry storage, and harness management into Go tools and applications.

## Packages Overview

| Package | Import Path | Description |
|---|---|---|
| [`aht`](https://pkg.go.dev/github.com/zigai/aht/pkg/aht) | `github.com/zigai/aht/pkg/aht` | **Canonical entrypoint**: primary client and domain models in a single, clean import. |
| [`broker`](https://pkg.go.dev/github.com/zigai/aht/pkg/broker) | `github.com/zigai/aht/pkg/broker` | Lightweight, dependency-free Unix domain socket client & protocol for direct realtime broker IPC. |
| [`tmux`](https://pkg.go.dev/github.com/zigai/aht/pkg/tmux) | `github.com/zigai/aht/pkg/tmux` | Inspect and discover tmux environment, current pane, and session topology. |
| [`client`](https://pkg.go.dev/github.com/zigai/aht/pkg/client) | `github.com/zigai/aht/pkg/client` | High-level client package supporting realtime, durable, or auto-fallback modes. |
| [`registry`](https://pkg.go.dev/github.com/zigai/aht/pkg/registry) | `github.com/zigai/aht/pkg/registry` | Core domain storage engines (`FileStore`, `MemoryStore`) and interfaces. |
| [`zellij`](https://pkg.go.dev/github.com/zigai/aht/pkg/zellij) | `github.com/zigai/aht/pkg/zellij` | Inspect and discover Zellij sessions, panes, and screen snapshots. |
| [`mux`](https://pkg.go.dev/github.com/zigai/aht/pkg/mux) | `github.com/zigai/aht/pkg/mux` | Common polymorphic types and helpers for terminal multiplexers. |
| [`manage`](https://pkg.go.dev/github.com/zigai/aht/pkg/manage) | `github.com/zigai/aht/pkg/manage` | Programmatic hook installation, removal, and background tracker daemon service control. |
| [`harness`](https://pkg.go.dev/github.com/zigai/aht/pkg/harness) | `github.com/zigai/aht/pkg/harness` | Supported harness catalog, alias normalization, and process-to-harness command matching. |

---

## 1. `pkg/aht` (Canonical API)

The `aht` package is the recommended entrypoint for Go applications. It bundles the primary client, operating modes, and all core domain types into a **single import** so you never have to juggle multiple packages.

### Querying Live Sessions

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/zigai/aht/pkg/aht"
)

func main() {
	ctx := context.Background()

	// Connect to AHT (single import, no "client" name collision)
	client := aht.New(aht.Config{
		Mode: aht.ModeRealtimeOnly, // or aht.ModeAuto
	})

	// Query live sessions
	sessions, err := client.List(ctx, aht.Filter{
		Presence:  aht.PresenceLive,
		Harnesses: []aht.Harness{aht.HarnessClaude, aht.HarnessPi},
	})
	if err != nil {
		log.Fatalf("list sessions failed: %v", err)
	}

	for _, s := range sessions {
		activity := "unknown"
		if s.Activity != nil {
			activity = string(*s.Activity)
		}
		fmt.Printf("[%s] %s (%s) — Activity: %s\n",
			s.Presence, s.Harness, s.SessionID, activity)
	}
}
```

### Channel Streaming

```go
sub, err := client.Subscribe(ctx, aht.Filter{Presence: aht.PresenceLive})
if err != nil {
	log.Fatal(err)
}
defer sub.Close()

for snapshot := range sub.Snapshots {
	fmt.Printf("Revision %d: %d live agents\n", snapshot.Revision, len(snapshot.Sessions))
}
```

---

## 2. `pkg/client`

The `client` package provides a unified API supporting three operating modes:

- `client.ModeAuto` (default): queries the broker socket first; falls back to reading `sessions.json` on disk if the broker daemon is offline.
- `client.ModeRealtimeOnly`: strictly dials the broker socket; returns `client.ErrUnavailable` immediately if the broker is stopped (never touches disk or takes file locks).
- `client.ModeDurableOnly`: reads and writes directly to the durable registry file, bypassing the broker daemon.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/zigai/aht/pkg/client"
)

func main() {
	ctx := context.Background()

	aht := client.New(client.Config{
		Mode: client.ModeRealtimeOnly,
	})

	sessions, err := aht.List(ctx, client.Filter{Presence: client.PresenceLive})
	if err != nil {
		log.Fatalf("failed to list sessions: %v", err)
	}

	for _, session := range sessions {
		fmt.Printf("[%s] %s (%s)\n", session.Presence, session.Harness, session.SessionID)
	}
}
```

---

## 3. `pkg/broker`

The `broker` package provides a pure, dependency-free client that speaks line-delimited JSON directly to AHT's Unix domain socket.

Use `broker` when you are building an integration (like an editor extension, status line, or companion daemon) that requires:
- Zero third-party dependencies (depends only on the Go standard library and `pkg/registry`).
- Immediate `ErrUnavailable` failures when AHT is stopped, with zero filesystem locking or disk fallback.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/zigai/aht/pkg/broker"
	"github.com/zigai/aht/pkg/registry"
)

func main() {
	ctx := context.Background()
	client := broker.NewClient(broker.DefaultSocketPath())

	// Stream live snapshots over the Unix socket
	sub, err := client.Subscribe(ctx, registry.Filter{Presence: registry.PresenceLive})
	if err != nil {
		if broker.IsUnavailable(err) {
			log.Println("AHT broker is not running")
			return
		}
		log.Fatalf("subscribe failed: %v", err)
	}
	defer sub.Close()

	for snapshot := range sub.Snapshots {
		for _, s := range snapshot.Sessions {
			fmt.Printf("[%s] %s in pane %s\n", s.Harness, s.SessionID, s.Tmux.PaneID)
		}
	}
}
```

---

## 4. `pkg/tmux`

The `tmux` package discovers tmux environment variables and pane metadata for terminal integrations.

```go
package main

import (
	"context"
	"fmt"

	"github.com/zigai/aht/pkg/tmux"
)

func main() {
	ctx := context.Background()

	// Discover the current tmux pane, window, and server socket
	current, err := tmux.Current(ctx)
	if err != nil {
		fmt.Println("Not running inside tmux")
		return
	}

	fmt.Printf("Tmux Server: %s, Session: %s, Window: %s, Pane: %s\n",
		current.ServerSocket, current.SessionName, current.WindowName, current.PaneID)

	// List all panes across the server
	panes, err := tmux.ListPanes(ctx)
	if err != nil {
		fmt.Println("Failed to list panes:", err)
		return
	}
	fmt.Printf("Total active panes: %d\n", len(panes))
}
```

---

## 5. `pkg/registry`

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

## 6. `pkg/manage`

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

## 7. `pkg/harness`

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
