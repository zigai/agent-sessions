// Package cli implements command-line entrypoints for agent-sessions.
package cli

// Managed hook commands are hidden entrypoints for harness-native request/response
// hooks. These hooks must write protocol JSON such as {"decision":"allow"} while
// recording session state, so they cannot use the one-way `agent-sessions report`
// command directly. Keep this file as CLI transport glue; harness protocol rules
// belong in pkg/harness. The agy-hook command remains as a compatibility alias
// for already-installed Antigravity plugins.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
	"github.com/zigai/agent-sessions/pkg/registry"
	"github.com/zigai/agent-sessions/pkg/tmuxctx"
)

var errUnsupportedManagedHook = errors.New("harness does not support managed hooks")

type managedHookOptions struct {
	event string
}

func (app *application) newHookCommand() *cobra.Command {
	options := managedHookOptions{}

	cmd := &cobra.Command{
		Use:    hookCommandName + " <harness>",
		Short:  "Run a managed native harness hook",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.runManagedHook(cmd.Context(), cmd.InOrStdin(), args[0], options)
		},
	}
	cmd.Flags().StringVar(&options.event, "event", "", "native hook event name")

	return cmd
}

func (app *application) newAgyHookCommand() *cobra.Command {
	options := managedHookOptions{}

	cmd := &cobra.Command{
		Use:    "agy-hook",
		Short:  "Run the managed Antigravity CLI session hook",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.runManagedHook(cmd.Context(), cmd.InOrStdin(), string(registry.HarnessAgy), options)
		},
	}
	cmd.Flags().StringVar(&options.event, "event", "", "Antigravity hook event name")

	return cmd
}

func (app *application) runManagedHook(
	ctx context.Context,
	stdin io.Reader,
	harnessName string,
	options managedHookOptions,
) error {
	harness, err := harnesspkg.Normalize(harnessName)
	if err != nil {
		return fmt.Errorf("normalizing hook harness: %w", err)
	}

	data, _ := io.ReadAll(stdin)
	rawPayload := rawPayloadFromHookBytes(data)
	payload := hookPayloadObject(rawPayload)
	result, ok := harnesspkg.HandleHook(harness, options.event, rawPayload, payload, parentProcessArgs(ctx))
	if !ok {
		return fmt.Errorf("%w: %s", errUnsupportedManagedHook, harness)
	}

	reportManagedHook(ctx, app.registryStore(), result)

	return app.writeJSON(result.Response)
}

func reportManagedHook(
	ctx context.Context,
	store registry.Store,
	result harnesspkg.HookResult,
) {
	if !result.ReportOK {
		return
	}

	report := result.Report
	if collected, err := tmuxctx.Current(ctx); err == nil {
		report.Tmux = collected
	}
	_, _ = store.Report(ctx, report)
}

func rawPayloadFromHookBytes(data []byte) json.RawMessage {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return nil
	}
	if json.Valid(data) {
		return json.RawMessage(data)
	}

	wrapped, err := json.Marshal(string(data))
	if err != nil {
		return nil
	}

	return json.RawMessage(wrapped)
}

func hookPayloadObject(rawPayload json.RawMessage) map[string]any {
	if len(rawPayload) == 0 {
		return map[string]any{}
	}

	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil || payload == nil {
		return map[string]any{}
	}

	return payload
}
