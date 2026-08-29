package cli

// Managed hook commands are integration entrypoints for harness-native
// request/response hooks. These hooks must write protocol JSON such as
// {"decision":"allow"} while recording session state, so they cannot use the
// one-way `aht report` command directly. Keep this file as CLI
// transport glue; harness protocol rules belong in pkg/harness.
//
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	harnesspkg "github.com/zigai/aht/pkg/harness"
	"github.com/zigai/aht/pkg/registry"
	"github.com/zigai/aht/pkg/tmuxctx"
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
		Short: "Integration protocol endpoint; not intended for manual use",
		Long:  "Integration protocol endpoint; not intended for manual use. Hook stdout is a JSON protocol response, so --json is required.",
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

	data, err := readPayloadInput(stdin)
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
	if result.ReportOK {
		result.Report.Process = reportProcessIdentity(harness, reportProcessAncestors(ctx, 0))
	}

	if err := reportManagedHook(ctx, app.registryStore(), result); err != nil {
		app.warnf("warning: %v\n", err)
	}

	return app.writeJSON(result.Response)
}

func reportManagedHook(ctx context.Context, store registry.Store, result harnesspkg.HookResult) error {
	if !result.ReportOK {
		return nil
	}
	observation := result.Report
	if collected, err := tmuxctx.Current(ctx); err == nil {
		observation.Tmux = &collected
	}
	if collected := reportMultiplexerContext(); !collected.Empty() {
		observation.Multiplexer = &collected
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
