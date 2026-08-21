-- +goose Up
CREATE TABLE relations (
  id BIGSERIAL PRIMARY KEY,
  org_id BIGINT NOT NULL,
  source_kind TEXT NOT NULL,
  source_namespace TEXT NOT NULL,
  source_name TEXT NOT NULL,
  relation_type TEXT NOT NULL,
  target_kind TEXT NOT NULL,
  target_namespace TEXT NOT NULL,
  target_name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now (),
  UNIQUE (
    org_id,
    source_kind,
    source_namespace,
    source_name,
    relation_type,
    target_kind,
    target_namespace,
    target_name
  ),
  FOREIGN KEY (
    org_id,
    source_kind,
    source_namespace,
    source_name
  ) REFERENCES entities (org_id, kind, namespace, name) ON DELETE CASCADE,
  FOREIGN KEY (
    org_id,
    target_kind,
    target_namespace,
    target_name
  ) REFERENCES entities (org_id, kind, namespace, name) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE relations;
