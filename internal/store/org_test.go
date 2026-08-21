package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ditwrd/pavedway/internal/store"
)

// Ticket #22 AC1: on first boot against an empty database, the bootstrap
// wizard step creates exactly one Organization.
func TestBootstrapOrganization_FirstRun_CreatesExactlyOne(t *testing.T) {
	q := newTestQueries(t)

	org, err := q.BootstrapOrganization(context.Background(), "Acme Corp")
	if err != nil {
		t.Fatalf("BootstrapOrganization() error = %v, want nil", err)
	}
	if org.Name != "Acme Corp" {
		t.Fatalf("org.Name = %q, want %q", org.Name, "Acme Corp")
	}
}

// Ticket #22 AC1: exactly one Organization — a second bootstrap is refused
// and the first org is neither duplicated nor replaced.
func TestBootstrapOrganization_SecondCall_Refused(t *testing.T) {
	q := newTestQueries(t)
	ctx := context.Background()

	if _, err := q.BootstrapOrganization(ctx, "Acme Corp"); err != nil {
		t.Fatalf("first BootstrapOrganization() error = %v, want nil", err)
	}

	_, err := q.BootstrapOrganization(ctx, "Globex")
	if !errors.Is(err, store.ErrOrganizationExists) {
		t.Fatalf("second BootstrapOrganization() error = %v, want store.ErrOrganizationExists", err)
	}

	count, err := q.CountOrganizations(ctx)
	if err != nil {
		t.Fatalf("CountOrganizations() error = %v, want nil", err)
	}
	if count != 1 {
		t.Fatalf("organization count = %d, want 1", count)
	}
}

// Ticket #22 AC1: no silent auto-creation — an empty database stays empty
// until the wizard step runs.
func TestNoSilentAutoCreation_EmptyOrgTable(t *testing.T) {
	q := newTestQueries(t)

	count, err := q.CountOrganizations(context.Background())
	if err != nil {
		t.Fatalf("CountOrganizations() error = %v, want nil", err)
	}
	if count != 0 {
		t.Fatalf("organization count = %d, want 0 (no silent auto-creation)", count)
	}
}
