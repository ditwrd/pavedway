-- name: CreateEntity :one
INSERT INTO entities(
  org_id,
  kind,
  namespace,
  name,
  metadata,
  spec
)
VALUES ($1,$2,$3,$4,$5,$6) 
RETURNING *;

-- name: GetEntity :one
SELECT * FROM entities 
WHERE org_id = $1 AND kind = $2 and namespace = $3 and name = $4
LIMIT 1;

-- name: UpdateEntity :one
UPDATE entities
SET metadata = $5, spec = $6, updated_at = now()
WHERE org_id = $1 AND kind = $2 and namespace = $3 and name = $4
RETURNING *;

-- name: DeleteEntity :execrows
DELETE FROM entities
  WHERE org_id = $1 AND kind = $2 AND namespace = $3 and name = $4;

-- name: ListEntitiesByKind :many
SELECT * FROM entities
WHERE org_id = $1 and kind = $2
ORDER BY namespace, name;
