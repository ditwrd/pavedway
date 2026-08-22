package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version metadata — overridden at build time via ldflags, e.g.:
//
//	go build -ldflags "-X github.com/ditwrd/pavedway/cmd.version=1.2.3 \
//	  -X github.com/ditwrd/pavedway/cmd.commit=$(git rev-parse --short HEAD) \
//	  -X github.com/ditwrd/pavedway/cmd.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" .
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Args:  noArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pavedway %s (commit: %s, built: %s)\n", version, commit, date)

		if info, ok := debug.ReadBuildInfo(); ok {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "go: %s\n", info.GoVersion)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
