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
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/spf13/cobra"

	"github.com/ditwrd/pavedway/internal/config"
	"github.com/ditwrd/pavedway/internal/db"
	"github.com/ditwrd/pavedway/internal/server"
	"github.com/ditwrd/pavedway/internal/store"
)

// noArgs rejects positional arguments, tagged as a usage error (exit 2).
func noArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return &usageError{err}
	}

	return nil
}

// serveCmd boots the pavedway HTTP server: connects to Postgres, runs
// pending migrations, then serves until the process is killed or the
// config file changes on disk — a change triggers a graceful shutdown and
// reboot with the new values (new DSN gets a fresh pool + re-run
// migrations; new port gets a fresh listener).
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the pavedway HTTP server",
	Args:  noArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Bind this command's own flags into the shared viper instance so
		// --database-url/--port participate in viper's normal precedence
		// (flag > env > config file > default) instead of being ignored.
		if err := v.BindPFlag("database_url", cmd.Flags().Lookup("database-url")); err != nil {
			return err
		}
		if err := v.BindPFlag("port", cmd.Flags().Lookup("port")); err != nil {
			return err
		}

		restart := make(chan config.Config, 1)
		cfg, err := config.Watch(v, cfgFile, func(newCfg config.Config) {
			select {
			case restart <- newCfg:
			default: // a restart is already pending; it already carries a later edit
			}
		})
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		for {
			next, err := runOnce(ctx, cfg, restart)
			if err != nil {
				return err
			}
			if next == nil || ctx.Err() != nil {
				return nil
			}
			cfg = *next
		}
	},
}

// runOnce connects to Postgres, runs migrations, and serves HTTP until ctx
// is cancelled (process shutdown) or a new config arrives on restart. On a
// config-triggered restart it shuts the server down gracefully and returns
// the new config for the caller to reboot with; on process shutdown it
// returns (nil, nil).
func runOnce(ctx context.Context, cfg config.Config, restart <-chan config.Config) (*config.Config, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	if err := db.RunMigrations(pool); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	nextCfg := make(chan config.Config, 1)
	go func() {
		select {
		case newCfg := <-restart:
			nextCfg <- newCfg
			cancel()
		case <-runCtx.Done():
		}
	}()

	srv, err := server.New(store.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("building server: %w", err)
	}

	sc := echo.StartConfig{Address: ":" + cfg.Port}
	if err := sc.Start(runCtx, srv); err != nil {
		return nil, err
	}

	select {
	case newCfg := <-nextCfg:
		return &newCfg, nil
	default:
		return nil, nil
	}
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().String("database-url", "", "Postgres connection string (env PAVEDWAY_DATABASE_URL)")
	serveCmd.Flags().String("port", "8080", "HTTP listen port (env PAVEDWAY_PORT)")
}
