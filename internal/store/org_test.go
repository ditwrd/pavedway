package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/internal/store"
)

// Ticket #22 AC1: on first boot against an empty database, the bootstrap
// wizard step creates exactly one Organization.
func TestBootstrapOrganization_FirstRun_CreatesExactlyOne(t *testing.T) {
	q := newTestQueries(t)

	org, err := q.BootstrapOrganization(context.Background(), "Acme Corp")
	require.NoError(t, err, "BootstrapOrganization()")
	require.Equal(t, "Acme Corp", org.Name, "org.Name")
}

// Ticket #22 AC1: exactly one Organization — a second bootstrap is refused
// and the first org is neither duplicated nor replaced.
func TestBootstrapOrganization_SecondCall_Refused(t *testing.T) {
	q := newTestQueries(t)
	ctx := context.Background()

	_, err := q.BootstrapOrganization(ctx, "Acme Corp")
	require.NoError(t, err, "first BootstrapOrganization()")

	_, err = q.BootstrapOrganization(ctx, "Globex")
	require.ErrorIs(t, err, store.ErrOrganizationExists, "second BootstrapOrganization()")

	count, err := q.CountOrganizations(ctx)
	require.NoError(t, err, "CountOrganizations()")
	require.Equal(t, int64(1), count, "organization count")
}

// Ticket #22 AC1: no silent auto-creation — an empty database stays empty
// until the wizard step runs.
func TestNoSilentAutoCreation_EmptyOrgTable(t *testing.T) {
	q := newTestQueries(t)

	count, err := q.CountOrganizations(context.Background())
	require.NoError(t, err, "CountOrganizations()")
	require.Equal(t, int64(0), count, "organization count (no silent auto-creation)")
}
