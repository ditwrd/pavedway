-- +goose Up
-- Server-side OIDC refresh tokens (issue #23): the pavedway session JWT in
-- the browser cookie is short-lived and stateless; when it expires, the
-- refresh endpoint exchanges the stored provider refresh token for a new
-- session without forcing a full re-login. One refresh token per user —
-- the last login wins, and deleting a User (or its Organization) cascades
-- the token away with it.
CREATE TABLE refresh_tokens(
  id BIGSERIAL PRIMARY KEY,
  org_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id BIGINT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  subject TEXT NOT NULL,
  refresh_token TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(org_id, user_id)
);
-- +goose Down
DROP TABLE refresh_tokens;
