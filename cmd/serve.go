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
	"log/slog"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
	"github.com/spf13/cobra"
	"go.uber.org/fx"

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

		// Watch loads the config and starts hot-reloading it: every file
		// change delivers the new config on restart and pings trigger. The
		// trigger exists because the fx app's restart watcher must not
		// consume the config value itself — RunE reads it after the app
		// stops.
		restart := make(chan config.Config, 1)
		trigger := make(chan struct{}, 1)
		cfg, err := config.Watch(v, cfgFile, func(newCfg config.Config) {
			select {
			case restart <- newCfg:
			default: // a restart is already pending; it already carries a later edit
			}

			select {
			case trigger <- struct{}{}:
			default:
			}
		})
		if err != nil {
			return err
		}

		// One fx app per boot: New runs the invokes (migrations, server
		// construction), Start opens the HTTP listener, Wait blocks until
		// a signal, a config-triggered shutdown, or a serve failure, and
		// Stop drains gracefully. A pending config on restart means the
		// app stopped to reboot; anything else is process exit.
		for {
			app := fx.New(serveAppOptions(cfg, trigger))
			if err := app.Start(context.Background()); err != nil {
				return err
			}

			<-app.Wait()

			if err := app.Stop(context.Background()); err != nil {
				return err
			}

			select {
			case cfg = <-restart:
			default:
				return nil
			}
		}
	},
}

// serveAppOptions is the fx wiring for one serve boot: the loaded config,
// the Postgres pool, migrations, the HTTP server lifecycle, and the
// config-restart watcher. Only the long-running serve service lives in fx;
// the short-lived CLI commands stay out of it.
func serveAppOptions(cfg config.Config, trigger <-chan struct{}) fx.Option {
	return fx.Options(
		fx.Supply(cfg, trigger),
		fx.Provide(newPool, newStore, store.New, buildServer),
		fx.Invoke(runMigrations, registerHTTP, watchForRestart),
		fx.NopLogger, // fx event spam would break the silent-boot contract
	)
}

// newStore adapts the connection pool to store.New's DBTX parameter. dig
// matches parameter types exactly and does not auto-satisfy interfaces, so
// the pool is bound to DBTX explicitly rather than relying on the
// *pgxpool.Pool result implementing it.
func newStore(pool *pgxpool.Pool) store.DBTX {
	return pool
}

// newPool creates the Postgres connection pool. pgxpool.New is lazy — no
// connection is attempted until the first query, which happens in
// runMigrations — so this is safe during graph construction. The pool is
// closed by its OnStop hook, which fx runs after the HTTP server's OnStop
// (reverse dependency order), so the pool never closes mid-drain.
func newPool(lc fx.Lifecycle, cfg config.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			pool.Close()

			return nil
		},
	})

	return pool, nil
}

// runMigrations keeps the "running migrations:" error prefix from the
// pre-fx serve flow.
func runMigrations(pool *pgxpool.Pool) error {
	if err := db.RunMigrations(pool); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

// buildServer keeps the "building server:" error prefix from the pre-fx
// serve flow. server.New performs OIDC discovery (bounded 10s), so it runs
// during graph construction, before the listener opens — the same
// fail-fast boot as the pre-fx code.
func buildServer(q *store.Queries, cfg config.Config) (*echo.Echo, error) {
	srv, err := server.New(q, cfg)
	if err != nil {
		return nil, fmt.Errorf("building server: %w", err)
	}

	return srv, nil
}

// registerHTTP wires the echo server into the fx lifecycle. OnStart binds
// the listener synchronously so a port conflict fails boot fast, then
// serves in a goroutine until serveCtx is cancelled. OnStop cancels it and
// waits for the graceful drain to finish before returning, so the pool's
// OnStop (which runs after) never closes under a serving server. If the
// server dies on its own, the app is shut down and the failure surfaces
// from OnStop as the boot error.
func registerHTTP(lc fx.Lifecycle, shutdowner fx.Shutdowner, srv *echo.Echo, cfg config.Config) {
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ":"+cfg.Port)
			if err != nil {
				return fmt.Errorf("starting http server: %w", err)
			}

			sc := echo.StartConfig{Address: ":" + cfg.Port, Listener: ln}
			go func() {
				// Always report the result: echo returns nil after a
				// graceful ctx-cancel, which is what unblocks OnStop's
				// drain wait — without this send, a config-restart or
				// signal would hang Stop forever.
				err := sc.Start(serveCtx, srv)
				serveErr <- err

				if err != nil {
					slog.Error("http server failed", "err", err)

					_ = shutdowner.Shutdown()
				}
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			cancelServe()

			select {
			case err := <-serveErr:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
}

// watchForRestart shuts the app down when the config watcher pings the
// trigger, letting RunE reboot with the new config. The trigger is a
// separate channel from restart so the config value stays in restart for
// RunE to read. The goroutine exits when the app stops (its OnStop closes
// done), so repeated reboots don't accumulate watchers.
func watchForRestart(trigger <-chan struct{}, shutdowner fx.Shutdowner, lc fx.Lifecycle) {
	done := make(chan struct{})

	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			close(done)

			return nil
		},
	})

	go func() {
		select {
		case <-trigger:
			_ = shutdowner.Shutdown()
		case <-done:
		}
	}()
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().String("database-url", "", "Postgres connection string (env PAVEDWAY_DATABASE_URL)")
	serveCmd.Flags().String("port", "8080", "HTTP listen port (env PAVEDWAY_PORT)")
}
