package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

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
		if err != nil {
			t.Fatalf("CreateEntity(%q) error = %v, want nil", kind, err)
		}
		if e.OrgID != org.ID {
			t.Fatalf("CreateEntity(%q).OrgID = %d, want %d (row must carry the bootstrapped org_id)", kind, e.OrgID, org.ID)
		}
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

	if _, err := q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID:     org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		Metadata:  metadata,
		Spec:      mustMarshal(t, map[string]any{}),
	}); err != nil {
		t.Fatalf("CreateEntity() error = %v, want nil", err)
	}

	got, err := q.GetEntity(ctx, store.GetEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	if err != nil {
		t.Fatalf("GetEntity() error = %v, want nil", err)
	}

	var meta struct {
		Annotations map[string]string `json:"annotations"`
	}
	mustUnmarshal(t, got.Metadata, &meta)
	for k, want := range annotations {
		if got := meta.Annotations[k]; got != want {
			t.Fatalf("annotation %q = %q, want %q (annotations must survive byte-for-byte)", k, got, want)
		}
	}
}

// Ticket #22 AC2: update modifies the entity in place and the change is
// visible on the next read.
func TestUpdateEntity_ReflectedOnReadBack(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	if _, err := q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		Metadata: mustMarshal(t, map[string]any{"annotations": map[string]string{"example.com/a": "one"}}),
		Spec:     mustMarshal(t, map[string]any{"type": "service"}),
	}); err != nil {
		t.Fatalf("CreateEntity() error = %v, want nil", err)
	}

	updated, err := q.UpdateEntity(ctx, store.UpdateEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		Metadata: mustMarshal(t, map[string]any{"annotations": map[string]string{"example.com/a": "two"}}),
		Spec:     mustMarshal(t, map[string]any{"type": "website"}),
	})
	if err != nil {
		t.Fatalf("UpdateEntity() error = %v, want nil", err)
	}

	var meta struct {
		Annotations map[string]string `json:"annotations"`
	}
	mustUnmarshal(t, updated.Metadata, &meta)
	if meta.Annotations["example.com/a"] != "two" {
		t.Fatalf("updated annotation = %q, want %q", meta.Annotations["example.com/a"], "two")
	}

	got, err := q.GetEntity(ctx, store.GetEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	if err != nil {
		t.Fatalf("GetEntity() after update error = %v, want nil", err)
	}
	mustUnmarshal(t, got.Metadata, &meta)
	if meta.Annotations["example.com/a"] != "two" {
		t.Fatalf("annotation after re-read = %q, want %q", meta.Annotations["example.com/a"], "two")
	}
}

// Ticket #22 AC2: delete removes the entity; reading it back is ErrNoRows.
func TestDeleteEntity_RemovesRow(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	if _, err := q.CreateEntity(ctx, store.CreateEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
		Metadata: mustMarshal(t, map[string]any{}),
		Spec:     mustMarshal(t, map[string]any{}),
	}); err != nil {
		t.Fatalf("CreateEntity() error = %v, want nil", err)
	}

	if _, err := q.DeleteEntity(ctx, store.DeleteEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	}); err != nil {
		t.Fatalf("DeleteEntity() error = %v, want nil", err)
	}

	if _, err := q.GetEntity(ctx, store.GetEntityParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetEntity() after delete error = %v, want pgx.ErrNoRows", err)
	}
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
	if err != nil {
		t.Fatalf("CreateOrganization(Globex) error = %v, want nil", err)
	}

	for _, c := range []struct {
		org        store.Organization
		annotation string
	}{
		{orgA, "a"},
		{orgB, "b"},
	} {
		if _, err := q.CreateEntity(ctx, store.CreateEntityParams{
			OrgID: c.org.ID, Kind: "Component", Namespace: "default", Name: "svc",
			Metadata: mustMarshal(t, map[string]any{"annotations": map[string]string{"example.com/org": c.annotation}}),
			Spec:     mustMarshal(t, map[string]any{}),
		}); err != nil {
			t.Fatalf("CreateEntity(org %q) error = %v, want nil", c.org.Name, err)
		}
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
		if err != nil {
			t.Fatalf("GetEntity(org %q) error = %v, want nil", c.org.Name, err)
		}
		var meta struct {
			Annotations map[string]string `json:"annotations"`
		}
		mustUnmarshal(t, got.Metadata, &meta)
		if meta.Annotations["example.com/org"] != c.annotation {
			t.Fatalf("org %q entity annotation = %q, want %q", c.org.Name, meta.Annotations["example.com/org"], c.annotation)
		}
	}

	rows, err := q.ListEntitiesByKind(ctx, store.ListEntitiesByKindParams{OrgID: orgA.ID, Kind: "Component"})
	if err != nil {
		t.Fatalf("ListEntitiesByKind(org A) error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListEntitiesByKind(org A) returned %d rows, want 1 (org A must not see org B's entities)", len(rows))
	}
	if rows[0].OrgID != orgA.ID {
		t.Fatalf("ListEntitiesByKind(org A) row OrgID = %d, want %d", rows[0].OrgID, orgA.ID)
	}
}
