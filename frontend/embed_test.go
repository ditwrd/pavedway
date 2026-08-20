package frontend_test

import (
	"io/fs"
	"testing"

	"github.com/ditwrd/pavedway/frontend"
)

// Dist() must expose the built frontend assets, rooted at dist/, so a
// caller (internal/server) can serve them without knowing the on-disk path.
func TestDistContainsIndex(t *testing.T) {
	dist, err := frontend.Dist()
	if err != nil {
		t.Fatalf("Dist() error = %v, want nil", err)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		t.Fatalf("dist has no index.html: %v", err)
	}
}
