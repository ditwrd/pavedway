package store_test

import (
	"context"
	"testing"

	"github.com/ditwrd/pavedway/internal/store"
)

func mustCreateEntity(t *testing.T, q *store.Queries, org store.Organization, kind, namespace, name string) {
	t.Helper()
	if _, err := q.CreateEntity(context.Background(), store.CreateEntityParams{
		OrgID: org.ID, Kind: kind, Namespace: namespace, Name: name,
		Metadata: mustMarshal(t, map[string]any{}),
		Spec:     mustMarshal(t, map[string]any{}),
	}); err != nil {
		t.Fatalf("CreateEntity(%s:%s/%s) error = %v, want nil", kind, namespace, name, err)
	}
}

// Ticket #22 AC5: a relation created once is queryable from both sides —
// ownedBy from the Component, ownerOf from the owning Group.
func TestRelation_QueryableFromEitherSide(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	mustCreateEntity(t, q, org, "Component", "default", "svc")
	mustCreateEntity(t, q, org, "Group", "default", "team-a")

	if _, err := q.CreateRelation(ctx, store.CreateRelationParams{
		OrgID:        org.ID,
		SourceKind:   "Component", SourceNamespace: "default", SourceName: "svc",
		RelationType: "ownedBy",
		TargetKind:   "Group", TargetNamespace: "default", TargetName: "team-a",
	}); err != nil {
		t.Fatalf("CreateRelation() error = %v, want nil", err)
	}

	// From the source (Component) side: the relation reads ownedBy -> Group.
	fromSource, err := q.ListRelationsBySource(ctx, store.ListRelationsBySourceParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	if err != nil {
		t.Fatalf("ListRelationsBySource() error = %v, want nil", err)
	}
	if len(fromSource) != 1 {
		t.Fatalf("ListRelationsBySource() = %d rows, want 1", len(fromSource))
	}
	if fromSource[0].RelationType != "ownedBy" {
		t.Fatalf("source-side relation type = %q, want %q", fromSource[0].RelationType, "ownedBy")
	}
	if fromSource[0].TargetKind != "Group" || fromSource[0].TargetName != "team-a" {
		t.Fatalf("source-side relation target = %s:%s/%s, want Group:default/team-a",
			fromSource[0].TargetKind, fromSource[0].TargetNamespace, fromSource[0].TargetName)
	}

	// From the target (Group) side: the same relation resolves as ownerOf.
	fromTarget, err := q.ListRelationsByTarget(ctx, store.ListRelationsByTargetParams{
		OrgID: org.ID, Kind: "Group", Namespace: "default", Name: "team-a",
	})
	if err != nil {
		t.Fatalf("ListRelationsByTarget() error = %v, want nil", err)
	}
	if len(fromTarget) != 1 {
		t.Fatalf("ListRelationsByTarget() = %d rows, want 1", len(fromTarget))
	}
	if fromTarget[0].RelationType != "ownerOf" {
		t.Fatalf("target-side relation type = %q, want %q (inverted from ownedBy)", fromTarget[0].RelationType, "ownerOf")
	}
	if fromTarget[0].SourceKind != "Component" || fromTarget[0].SourceName != "svc" {
		t.Fatalf("target-side relation source = %s:%s/%s, want Component:default/svc",
			fromTarget[0].SourceKind, fromTarget[0].SourceNamespace, fromTarget[0].SourceName)
	}
}

// Ticket #22 AC5: the either-side query inverts ALL five standard Backstage
// relation pairs, not just ownedBy/ownerOf — a partOf relation read from
// the target side must come back as hasPart.
func TestRelation_TargetSide_InvertsAllPairs(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	mustCreateEntity(t, q, org, "Component", "default", "svc")
	mustCreateEntity(t, q, org, "System", "default", "backstage")

	if _, err := q.CreateRelation(ctx, store.CreateRelationParams{
		OrgID:        org.ID,
		SourceKind:   "Component", SourceNamespace: "default", SourceName: "svc",
		RelationType: "partOf",
		TargetKind:   "System", TargetNamespace: "default", TargetName: "backstage",
	}); err != nil {
		t.Fatalf("CreateRelation() error = %v, want nil", err)
	}

	// Source side keeps the original label.
	fromSource, err := q.ListRelationsBySource(ctx, store.ListRelationsBySourceParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	if err != nil {
		t.Fatalf("ListRelationsBySource() error = %v, want nil", err)
	}
	if len(fromSource) != 1 || fromSource[0].RelationType != "partOf" {
		t.Fatalf("source-side relation = %+v, want one partOf row", fromSource)
	}

	// Target side must see the inverted label hasPart.
	fromTarget, err := q.ListRelationsByTarget(ctx, store.ListRelationsByTargetParams{
		OrgID: org.ID, Kind: "System", Namespace: "default", Name: "backstage",
	})
	if err != nil {
		t.Fatalf("ListRelationsByTarget() error = %v, want nil", err)
	}
	if len(fromTarget) != 1 {
		t.Fatalf("ListRelationsByTarget() = %d rows, want 1", len(fromTarget))
	}
	if fromTarget[0].RelationType != "hasPart" {
		t.Fatalf("target-side relation type = %q, want %q (inverted from partOf)", fromTarget[0].RelationType, "hasPart")
	}
	if fromTarget[0].SourceKind != "Component" || fromTarget[0].SourceName != "svc" {
		t.Fatalf("target-side source = %s:%s, want Component:svc", fromTarget[0].SourceKind, fromTarget[0].SourceName)
	}
}

// Ticket #22 AC6: two entities with different org_id cannot be related —
// the relation layer is fully partitioned, no cross-org relations.
func TestRelation_CrossOrg_Rejected(t *testing.T) {
	q := newTestQueries(t)
	ctx := context.Background()
	orgA := bootstrap(t, q, "Acme Corp")
	orgB, err := q.CreateOrganization(ctx, "Globex")
	if err != nil {
		t.Fatalf("CreateOrganization(Globex) error = %v, want nil", err)
	}

	mustCreateEntity(t, q, orgA, "Component", "default", "svc")
	mustCreateEntity(t, q, orgB, "Group", "default", "team-a")

	_, err = q.CreateRelation(ctx, store.CreateRelationParams{
		OrgID:        orgA.ID,
		SourceKind:   "Component", SourceNamespace: "default", SourceName: "svc",
		RelationType: "ownedBy",
		TargetKind:   "Group", TargetNamespace: "default", TargetName: "team-a",
	})
	if err == nil {
		t.Fatal("CreateRelation() across orgs succeeded, want error (no cross-org relations)")
	}
}
