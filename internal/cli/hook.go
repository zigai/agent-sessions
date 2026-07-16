package cli

// Managed hook commands are integration entrypoints for harness-native
// request/response hooks. These hooks must write protocol JSON such as
// {"decision":"allow"} while recording session state, so they cannot use the
// one-way `agent-sessions report` command directly. Keep this file as CLI
// transport glue; harness protocol rules belong in pkg/harness.
//
// The agy-hook command remains hidden as a compatibility alias for
// already-installed Antigravity plugins.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/v2/internal/reportqueue"
	harnesspkg "github.com/zigai/agent-sessions/v2/pkg/harness"
	"github.com/zigai/agent-sessions/v2/pkg/registry"
	"github.com/zigai/agent-sessions/v2/pkg/tmuxctx"
)

var errUnsupportedManagedHook = errors.New("harness does not support managed hooks")

type managedHookOptions struct {
	event string
	queue bool
}

func (app *application) newHookCommand() *cobra.Command {
	options := managedHookOptions{}

	cmd := &cobra.Command{
		Use:   hookCommandName + " <harness>",
		Short: "Run a request/response hook for a harness",
		Long:  "Run a request/response hook for a harness. Hook stdout is a JSON protocol response, so --json is required.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.runManagedHook(cmd.Context(), cmd.InOrStdin(), args[0], options)
		},
	}
	cmd.Flags().StringVar(&options.event, "event", "", "native hook event name")
	cmd.Flags().BoolVar(&options.queue, "queue", false, "durably queue the report side effect")
	_ = cmd.Flags().MarkHidden("queue")

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
	cmd.Flags().BoolVar(&options.queue, "queue", false, "durably queue the report side effect")
	_ = cmd.Flags().MarkHidden("queue")

	return cmd
}

func (app *application) runManagedHook(
	ctx context.Context,
	stdin io.Reader,
	harnessName string,
	options managedHookOptions,
) error {
	if !app.outputJSON {
		return errManagedHookJSONRequired
	}
	harness, err := harnesspkg.Normalize(harnessName)
	if err != nil {
		return fmt.Errorf("normalizing hook harness: %w", err)
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading hook payload: %w", err)
	}
	rawPayload := rawPayloadFromHookBytes(data)
	payload := hookPayloadObject(rawPayload)
	parentArgs := parentProcessArgs(ctx)
	result, ok := harnesspkg.HandleHook(harness, options.event, rawPayload, payload, parentArgs)
	if !ok {
		return fmt.Errorf("%w: %s", errUnsupportedManagedHook, harness)
	}

	if options.queue {
		app.queueManagedHook(ctx, result, data, parentArgs)
	} else if err := reportManagedHook(ctx, app.registryStore(), result); err != nil {
		app.warnf("warning: %v\n", err)
	}

	return app.writeJSON(result.Response)
}

func (app *application) queueManagedHook(
	ctx context.Context,
	result harnesspkg.HookResult,
	stdin []byte,
	parentArgs []string,
) {
	if !result.ReportOK {
		return
	}
	now := time.Now().UTC()
	observation := result.Report
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = now
	}
	storePath := app.store().Path()
	queue := reportqueue.New(storePath)
	tmuxEnv := tmuxctx.Env{TMUX: os.Getenv("TMUX"), TMUXPane: os.Getenv("TMUX_PANE")}
	minimalTmux := tmuxctx.ContextFromEnv(tmuxEnv)
	cachedTmux := minimalTmux
	if cached, ok := queue.LookupTmuxContext(minimalTmux, now, 0); ok {
		cachedTmux = cached
	}
	_, err := queue.Enqueue(ctx, reportqueue.Envelope{
		Version:       reportqueue.EnvelopeVersion,
		CreatedAt:     now,
		StorePath:     storePath,
		Kind:          reportqueue.KindReport,
		Report:        reportqueue.ReportFromRegistry(observation),
		RawPayloadSet: len(observation.RawPayload) > 0,
		Stdin:         []byte(strings.TrimSpace(string(stdin))),
		Runtime: reportqueue.RuntimeContext{
			CWD:        defaultCWD(),
			ParentArgs: parentArgs,
			Env: map[string]string{
				"TMUX":      tmuxEnv.TMUX,
				"TMUX_PANE": tmuxEnv.TMUXPane,
				"PWD":       os.Getenv("PWD"),
			},
		},
		CachedTmux: cachedTmux,
	}, reportqueue.EnqueueOptions{Now: func() time.Time { return now }})
	if err != nil {
		app.warnf("warning: queueing managed hook report failed: %v\n", err)
		if err := reportManagedHook(ctx, app.registryStore(), result); err != nil {
			app.warnf("warning: %v\n", err)
		}

		return
	}
	app.kickQueueDrainer(ctx, storePath)
}

func reportManagedHook(ctx context.Context, store registry.Store, result harnesspkg.HookResult) error {
	if !result.ReportOK {
		return nil
	}
	observation := result.Report
	if collected, err := tmuxctx.Current(ctx); err == nil {
		observation.Tmux = &collected
	}
	if _, err := store.Observe(ctx, observation); err != nil {
		return fmt.Errorf("recording managed hook observation: %w", err)
	}
	return nil
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
