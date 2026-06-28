package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zigai/agent-sessions/internal/install"
	harnesspkg "github.com/zigai/agent-sessions/pkg/harness"
)

func (app *application) newInstallHooksCommand() *cobra.Command {
	binary := defaultInstallBinary()

	var targetBinary string

	var dryRun bool

	var force bool

	var useShim bool

	cmd := &cobra.Command{
		Use:   "install-hooks <harness|all>",
		Short: "Install harness reporting hooks or shims",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if strings.EqualFold(args[0], "all") {
				return app.runInstallAll(binary, targetBinary, dryRun, force, useShim)
			}

			harness, err := harnesspkg.Normalize(args[0])
			if err != nil {
				return fmt.Errorf("normalizing harness: %w", err)
			}

			result, err := install.Run(install.Options{
				Harness:      harness,
				Binary:       binary,
				TargetBinary: targetBinary,
				DryRun:       dryRun,
				Force:        force,
				UseShim:      useShim,
			})
			if err != nil {
				return fmt.Errorf("installing hooks: %w", err)
			}

			if app.outputJSON {
				return app.writeJSON(result)
			}

			if err := app.writef("%s\npath: %s\n", result.Message, result.Path); err != nil {
				return err
			}

			if dryRun && result.Snippet != "" {
				if err := app.writeln(); err != nil {
					return err
				}

				if err := app.writeln(result.Snippet); err != nil {
					return err
				}
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&binary, "binary", binary, "agent-sessions binary used by installed hooks")
	cmd.Flags().StringVar(&targetBinary, "target-binary", "", "real harness binary path for shim installs")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print planned integration without writing files")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing integration file that is not managed by agent-sessions")
	cmd.Flags().BoolVar(&useShim, "shim", false, "install PATH shim fallback when supported")

	return cmd
}

func (app *application) runInstallAll(binary string, targetBinary string, dryRun bool, force bool, useShim bool) error {
	results, err := install.RunAll(install.Options{
		Harness:      "",
		Binary:       binary,
		TargetBinary: targetBinary,
		DryRun:       dryRun,
		Force:        force,
		UseShim:      useShim,
	})
	if app.outputJSON {
		err := app.writeJSON(results)
		if err != nil {
			return err
		}
	}

	if !app.outputJSON {
		for _, result := range results {
			if result.Error != "" {
				err := app.writef("%s: %s\n", result.Harness, result.Error)
				if err != nil {
					return err
				}

				continue
			}

			err := app.writef("%s: %s\npath: %s\n", result.Harness, result.Message, result.Path)
			if err != nil {
				return err
			}
		}
	}

	if err != nil {
		return fmt.Errorf("installing all hooks: %w", err)
	}

	return nil
}

func defaultInstallBinary() string {
	executable, err := os.Executable()
	if err != nil {
		return "agent-sessions"
	}

	resolved, err := filepath.EvalSymlinks(executable)
	if err == nil && resolved != "" {
		executable = resolved
	}

	absolute, err := filepath.Abs(executable)
	if err != nil {
		return executable
	}

	return absolute
}
