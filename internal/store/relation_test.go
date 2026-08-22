package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/internal/store"
)

func mustCreateEntity(t *testing.T, q *store.Queries, org store.Organization, kind, namespace, name string) {
	t.Helper()
	_, err := q.CreateEntity(context.Background(), store.CreateEntityParams{
		OrgID: org.ID, Kind: kind, Namespace: namespace, Name: name,
		Metadata: mustMarshal(t, map[string]any{}),
		Spec:     mustMarshal(t, map[string]any{}),
	})
	require.NoError(t, err, "CreateEntity(%s:%s/%s)", kind, namespace, name)
}

// Ticket #22 AC5: a relation created once is queryable from both sides —
// ownedBy from the Component, ownerOf from the owning Group.
func TestRelation_QueryableFromEitherSide(t *testing.T) {
	q := newTestQueries(t)
	org := bootstrap(t, q, "Acme Corp")
	ctx := context.Background()

	mustCreateEntity(t, q, org, "Component", "default", "svc")
	mustCreateEntity(t, q, org, "Group", "default", "team-a")

	_, err := q.CreateRelation(ctx, store.CreateRelationParams{
		OrgID:      org.ID,
		SourceKind: "Component", SourceNamespace: "default", SourceName: "svc",
		RelationType: "ownedBy",
		TargetKind:   "Group", TargetNamespace: "default", TargetName: "team-a",
	})
	require.NoError(t, err, "CreateRelation()")

	// From the source (Component) side: the relation reads ownedBy -> Group.
	fromSource, err := q.ListRelationsBySource(ctx, store.ListRelationsBySourceParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	require.NoError(t, err, "ListRelationsBySource()")
	require.Len(t, fromSource, 1, "ListRelationsBySource()")
	require.Equal(t, "ownedBy", fromSource[0].RelationType, "source-side relation type")
	require.Equal(t, "Group", fromSource[0].TargetKind, "source-side relation target kind")
	require.Equal(t, "team-a", fromSource[0].TargetName, "source-side relation target name")

	// From the target (Group) side: the same relation resolves as ownerOf.
	fromTarget, err := q.ListRelationsByTarget(ctx, store.ListRelationsByTargetParams{
		OrgID: org.ID, Kind: "Group", Namespace: "default", Name: "team-a",
	})
	require.NoError(t, err, "ListRelationsByTarget()")
	require.Len(t, fromTarget, 1, "ListRelationsByTarget()")
	require.Equal(t, "ownerOf", fromTarget[0].RelationType, "target-side relation type (inverted from ownedBy)")
	require.Equal(t, "Component", fromTarget[0].SourceKind, "target-side relation source kind")
	require.Equal(t, "svc", fromTarget[0].SourceName, "target-side relation source name")
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

	_, err := q.CreateRelation(ctx, store.CreateRelationParams{
		OrgID:      org.ID,
		SourceKind: "Component", SourceNamespace: "default", SourceName: "svc",
		RelationType: "partOf",
		TargetKind:   "System", TargetNamespace: "default", TargetName: "backstage",
	})
	require.NoError(t, err, "CreateRelation()")

	// Source side keeps the original label.
	fromSource, err := q.ListRelationsBySource(ctx, store.ListRelationsBySourceParams{
		OrgID: org.ID, Kind: "Component", Namespace: "default", Name: "svc",
	})
	require.NoError(t, err, "ListRelationsBySource()")
	require.Len(t, fromSource, 1, "source-side relation (want one partOf row)")
	require.Equal(t, "partOf", fromSource[0].RelationType, "source-side relation type (want one partOf row)")

	// Target side must see the inverted label hasPart.
	fromTarget, err := q.ListRelationsByTarget(ctx, store.ListRelationsByTargetParams{
		OrgID: org.ID, Kind: "System", Namespace: "default", Name: "backstage",
	})
	require.NoError(t, err, "ListRelationsByTarget()")
	require.Len(t, fromTarget, 1, "ListRelationsByTarget()")
	require.Equal(t, "hasPart", fromTarget[0].RelationType, "target-side relation type (inverted from partOf)")
	require.Equal(t, "Component", fromTarget[0].SourceKind, "target-side source kind")
	require.Equal(t, "svc", fromTarget[0].SourceName, "target-side source name")
}

// Ticket #22 AC6: two entities with different org_id cannot be related —
// the relation layer is fully partitioned, no cross-org relations.
func TestRelation_CrossOrg_Rejected(t *testing.T) {
	q := newTestQueries(t)
	ctx := context.Background()
	orgA := bootstrap(t, q, "Acme Corp")
	orgB, err := q.CreateOrganization(ctx, "Globex")
	require.NoError(t, err, "CreateOrganization(Globex)")

	mustCreateEntity(t, q, orgA, "Component", "default", "svc")
	mustCreateEntity(t, q, orgB, "Group", "default", "team-a")

	_, err = q.CreateRelation(ctx, store.CreateRelationParams{
		OrgID:      orgA.ID,
		SourceKind: "Component", SourceNamespace: "default", SourceName: "svc",
		RelationType: "ownedBy",
		TargetKind:   "Group", TargetNamespace: "default", TargetName: "team-a",
	})
	require.Error(t, err, "CreateRelation() across orgs succeeded (no cross-org relations)")
}
