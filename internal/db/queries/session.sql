-- name: UpsertRefreshToken :one
INSERT INTO refresh_tokens (org_id, user_id, provider, subject, refresh_token)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (org_id, user_id) DO UPDATE SET
  provider = EXCLUDED.provider,
  subject = EXCLUDED.subject,
  refresh_token = EXCLUDED.refresh_token,
  updated_at = now()
RETURNING *;

-- name: GetRefreshToken :one
SELECT * FROM refresh_tokens
WHERE org_id = $1 AND user_id = $2;

-- name: DeleteRefreshToken :execrows
DELETE FROM refresh_tokens
WHERE org_id = $1 AND user_id = $2;
