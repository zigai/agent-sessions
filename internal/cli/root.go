package cli

import (
	"os"
	"sync"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "agent-sessions",
	Short: "A Go CLI project",
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},
	Run: func(cmd *cobra.Command, args []string) {
		if v, _ := cmd.Flags().GetBool("version"); v {
			cmd.Printf("agent-sessions %s (commit: %s, built: %s)\n", version, commit, date)
			return
		}

		_ = cmd.Help()
	},
}

var setupCommandsOnce sync.Once

func setupCommands() {
	setupCommandsOnce.Do(func() {
		rootCmd.Flags().BoolP("version", "v", false, "Print version")
	})
}

func Execute() {
	setupCommands()

	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
