-- name: CreateOrganization :one
INSERT INTO organizations (name)
VALUES ($1)
RETURNING *;

-- name: CountOrganizations :one
SELECT COUNT(*) FROM organizations;

-- name: GetOrganization :one
SELECT * FROM organizations
LIMIT 1;
