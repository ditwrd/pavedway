package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/ditwrd/pavedway/internal/config"
)

// TestServeAppGraph validates the serve fx graph: every constructor and
// invoke parameter resolves, with no missing providers or cycles. fx
// ValidateApp runs the graph in dry-run mode, so no constructor executes —
// no Postgres connection, no OIDC discovery — keeping the test fast and
// hermetic.
func TestServeAppGraph(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		DatabaseURL: "postgres://localhost:5432/pavedway",
		Port:        "8080",
	}
	require.NoError(t, fx.ValidateApp(serveAppOptions(cfg, make(chan struct{}))), "serve fx graph invalid")
}
