/*
Copyright © 2026 Aditya Wardianto <hi@ditwrd.dev>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package cmd wires the pavedway CLI commands.
package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// v is shared by every subcommand so their flags all resolve through the
// same precedence (flag > env > config file > default).
var (
	cfgFile string
	v       = viper.New()
)

// usageError marks a flag or argument validation failure: the process must
// exit 2 (usage error per Unix convention), not 1 (runtime failure).
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "pavedway",
	Short: "pavedway is a self-service internal developer platform",
	Long: `pavedway serves the pavedway web UI and API, connects to Postgres,
runs its schema migrations on boot, and serves the embedded frontend.`,
	// Don't dump the full help text on every error — that's what --help is
	// for — and let Execute print errors itself so exit codes can
	// distinguish usage errors (2) from runtime failures (1).
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI and exits with the process status. Errors go to
// stderr (stdout stays clean for pipeable program output); usage errors
// exit 2, runtime failures exit 1.
func Execute() {
	err := rootCmd.Execute()
	if err == nil {
		return
	}

	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintln(os.Stderr, "Error:", ue.err)
		fmt.Fprintln(os.Stderr, "Run 'pavedway --help' for usage.")
		os.Exit(2)
	}

	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./pavedway.yaml)")

	// Flag parse failures are usage errors (exit 2), not runtime failures.
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err}
	})
}
