package frontend_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/frontend"
)

// Dist() must expose the built frontend assets, rooted at dist/, so a
// caller (internal/server) can serve them without knowing the on-disk path.
func TestDistContainsIndex(t *testing.T) {
	dist, err := frontend.Dist()
	require.NoError(t, err, "Dist()")
	_, err = fs.Stat(dist, "index.html")
	require.NoError(t, err, "dist has no index.html")
}
