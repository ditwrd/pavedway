-- name: CreateRelation :one
INSERT INTO
relations (
    org_id,
    source_kind,
    source_namespace,
    source_name,
    relation_type,
    target_kind,
    target_namespace,
    target_name
  )
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListRelationsBySource :many
SELECT *
  FROM relations
  WHERE org_id = $1 AND source_kind = @kind AND source_namespace = @namespace AND source_name = @name;

-- name: ListRelationsByTarget :many
SELECT
    id,
    org_id,
    source_kind,
    source_namespace,
    source_name,
    CASE relation_type
        WHEN 'ownedBy' THEN 'ownerOf'
        WHEN 'ownerOf' THEN 'ownedBy'
        WHEN 'dependsOn' THEN 'dependencyOf'
        WHEN 'dependencyOf' THEN 'dependsOn'
        WHEN 'partOf' THEN 'hasPart'
        WHEN 'hasPart' THEN 'partOf'
        WHEN 'providesApi' THEN 'apiConsumedBy'
        WHEN 'apiConsumedBy' THEN 'providesApi'
        WHEN 'memberOf' THEN 'hasMember'
        WHEN 'hasMember' THEN 'memberOf'
        ELSE relation_type
    END::text AS relation_type,
    target_kind,
    target_namespace,
    target_name,
    created_at
FROM relations
WHERE org_id = $1 AND target_kind = @kind AND target_namespace = @namespace AND target_name = @name;
