package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/internal/store"
)

// Ticket #22 AC2: every one of the eight core catalog kinds can be created,
// and every created row carries the bootstrapped org_id.
func TestCreateEntity_AllKinds_CarryOrgID(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	kinds := []string{"Component", "API", "Resource", "System", "Domain", "Location", "User", "Group"}
	for _, kind := range kinds {
		e, err := q.CreateEntity(ctx, store.CreateEntityParams{
			OrgID:     org.ID,
			Kind:      kind,
			Namespace: "default",
			Name:      "example",
			Metadata:  mustMarshal(t, map[string]any{}),
			Spec:      mustMarshal(t, map[string]any{}),
		})
		require.NoError(t, err, "CreateEntity(%q)", kind)
		require.Equal(t, org.ID, e.OrgID, "CreateEntity(%q) (row must carry the bootstrapped org_id)", kind)
	}
}

// Ticket #22 AC4 (store level): annotations — including ones pavedway does
// not understand — are preserved on the entity after load, not dropped.
func TestEntity_AnnotationsSurviveRoundTrip(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	annotations := map[string]string{
		"backstage.io/techdocs-ref":        "dir:.", // TechDocs-specific: unknown to pavedway
		"backstage.io/managed-by-location": "url:https://github.com/example/example/blob/main/catalog-info.yaml",
		"example.com/custom-tag":           "keep-me",
	}
	metadata := mustMarshal(t, map[string]any{
		"annotations": annotations,
		"description": "A service",
	})

	_, err := q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		Metadata: metadata,
		Spec:     mustMarshal(t, map[string]any{}),
	})
	require.NoError(t, err, "CreateEntity()")

	got, err := q.GetEntity(ctx, store.GetEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	require.NoError(t, err, "GetEntity()")

	var meta struct {
		Annotations map[string]string `json:"annotations"`
	}
	mustUnmarshal(t, got.Metadata, &meta)
	for k, want := range annotations {
		require.Equal(t, want, meta.Annotations[k], "annotation %q (annotations must survive byte-for-byte)", k)
	}
}

// Ticket #22 AC2: update modifies the entity in place and the change is
// visible on the next read.
func TestUpdateEntity_ReflectedOnReadBack(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	_, err := q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		Metadata: mustMarshal(t, map[string]any{"annotations": map[string]string{"example.com/a": "one"}}),
		Spec:     mustMarshal(t, map[string]any{"type": "service"}),
	})
	require.NoError(t, err, "CreateEntity()")

	updated, err := q.UpdateEntity(ctx, store.UpdateEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		Metadata: mustMarshal(t, map[string]any{"annotations": map[string]string{"example.com/a": "two"}}),
		Spec:     mustMarshal(t, map[string]any{"type": "website"}),
	})
	require.NoError(t, err, "UpdateEntity()")

	var meta struct {
		Annotations map[string]string `json:"annotations"`
	}
	mustUnmarshal(t, updated.Metadata, &meta)
	require.Equal(t, "two", meta.Annotations["example.com/a"], "updated annotation")

	got, err := q.GetEntity(ctx, store.GetEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	require.NoError(t, err, "GetEntity() after update")
	mustUnmarshal(t, got.Metadata, &meta)
	require.Equal(t, "two", meta.Annotations["example.com/a"], "annotation after re-read")
}

// Ticket #22 AC2: delete removes the entity; reading it back is ErrNoRows.
func TestDeleteEntity_RemovesRow(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	_, err := q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		Metadata: mustMarshal(t, map[string]any{}),
		Spec:     mustMarshal(t, map[string]any{}),
	})
	require.NoError(t, err, "CreateEntity()")

	_, err = q.DeleteEntity(ctx, store.DeleteEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	require.NoError(t, err, "DeleteEntity()")

	_, err = q.GetEntity(ctx, store.GetEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "GetEntity() after delete")
}

// Ticket #22 AC6 (partitioning): the same entity ref may exist in two
// different organizations; reads and lists are scoped by org_id. Creating a
// second org via CreateOrganization is legal — the exactly-one rule lives in
// BootstrapOrganization, not the schema.
func TestEntityPartitioning_SameRefDifferentOrgs(t *testing.T) {
	q := newTestQueries(t)
	ctx := context.Background()
	orgA := bootstrap(t, q, "Acme Corp")
	orgB, err := q.CreateOrganization(ctx, "Globex")
	require.NoError(t, err, "CreateOrganization(Globex)")

	for _, c := range []struct {
		org        store.Organization
		annotation string
	}{
		{orgA, "a"},
		{orgB, "b"},
	} {
		_, err := q.CreateEntity(ctx, store.CreateEntityParams{
			OrgID: c.org.ID, Kind: "Component", Namespace: "default", Name: "svc",
			Metadata: mustMarshal(t, map[string]any{"annotations": map[string]string{"example.com/org": c.annotation}}),
			Spec:     mustMarshal(t, map[string]any{}),
		})
		require.NoError(t, err, "CreateEntity(org %q)", c.org.Name)
	}

	for _, c := range []struct {
		org        store.Organization
		annotation string
	}{
		{orgA, "a"},
		{orgB, "b"},
	} {
		got, err := q.GetEntity(ctx, store.GetEntityParams{
			OrgID: c.org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		})
		require.NoError(t, err, "GetEntity(org %q)", c.org.Name)
		var meta struct {
			Annotations map[string]string `json:"annotations"`
		}
		mustUnmarshal(t, got.Metadata, &meta)
		require.Equal(t, c.annotation, meta.Annotations["example.com/org"], "org %q entity annotation", c.org.Name)
	}

	rows, err := q.ListEntitiesByKind(ctx, store.ListEntitiesByKindParams{OrgID: orgA.ID, Kind: "Component"})
	require.NoError(t, err, "ListEntitiesByKind(org A)")
	require.Len(t, rows, 1, "ListEntitiesByKind(org A) (org A must not see org B's entities)")
	require.Equal(t, orgA.ID, rows[0].OrgID, "ListEntitiesByKind(org A) row OrgID")
}
