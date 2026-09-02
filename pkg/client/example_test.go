package client_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zigai/aht/pkg/client"
	"github.com/zigai/aht/pkg/registry"
)

func ExampleClient_List() {
	directory, err := os.MkdirTemp("", "aht-client-example")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(directory) }()

	storePath := filepath.Join(directory, "sessions.json")
	presence := registry.PresenceLive
	activity := registry.ActivityRunning
	if _, err := registry.NewFileStore(storePath).Observe(context.Background(), registry.Observation{
		Source:     registry.ObservationSourceNative,
		Evidence:   registry.ObservationEvidenceNativeEvent,
		Harness:    registry.HarnessCodex,
		Identity:   registry.ObservationIdentity{SessionID: "example"},
		Presence:   &presence,
		Activity:   &activity,
		ObservedAt: time.Now().UTC(),
	}); err != nil {
		panic(err)
	}

	aht := client.New(client.Config{
		StorePath:  storePath,
		SocketPath: filepath.Join(directory, "offline.sock"),
	})
	sessions, err := aht.List(
		context.Background(),
		registry.Filter{Presence: registry.PresenceLive},
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s: %s\n", sessions[0].Harness, *sessions[0].Activity)
	// Output: codex: running
}
