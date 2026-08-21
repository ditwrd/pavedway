-- +goose Up
CREATE TABLE entities(
  id BIGSERIAL PRIMARY KEY,
  org_id BIGINT NOT NULL REFERENCES organizations(id)
ON DELETE CASCADE,
  kind TEXT NOT NULL,
  namespace TEXT NOT NULL DEFAULT 'default',
  name TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}',
  spec JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(
    org_id,
    kind,
    namespace,
    name
  )
);
-- +goose Down
DROP TABLE entities;
