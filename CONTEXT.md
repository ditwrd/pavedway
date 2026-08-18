# Pavedway

A self-hostable, Kubernetes/Crossplane-native Internal Developer Platform: a Backstage-compatible software catalog plus first-class Kubernetes/Crossplane discovery and self-service provisioning.

## Language

**Organization**:
The single top-level tenancy scope. A first-class catalog entity kind; every other catalog entity (Component, API, Group, User, etc.) carries an `org_id` and is fully partitioned by it — no cross-org relations exist. v0.1 runs exactly one Organization, created via a first-run bootstrap wizard.
_Avoid_: Tenant, Account, Workspace — "Organization" is the one name for this scope.

**Team**:
Not a distinct kind — a Team *is* Backstage's existing `Group` entity kind, reused as-is (members, `parentGroup` nesting, ownership target for Components). "Team" is the name used in conversation for org-internal member groupings; the catalog stores it as `Group`.
_Avoid_: Introducing a separate `Team` entity kind.

**Tenant isolation**:
The `org_id` column present on every entity/row from v0.1 onward, per [ADR 0001](docs/adr/0001-open-core-tenant-isolation-boundary.md). Ships in OSS core even though v0.1 itself is single-tenant, so enabling real multi-tenancy later is a flag flip, not a schema migration.
_Avoid_: "Multi-tenancy" to describe this column's presence — multi-tenancy is the *deferred* capability of running more than one Organization; the isolation primitive ships now, using it for more than one org does not.
