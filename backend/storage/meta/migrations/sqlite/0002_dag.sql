ALTER TABLE manifests ADD COLUMN parent_id TEXT;

CREATE TABLE annotations (
  tenant_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  key TEXT NOT NULL CHECK (length(key) BETWEEN 1 AND 64),
  value TEXT NOT NULL CHECK (length(value) <= 4096),
  created_at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, manifest_id, key),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
) STRICT;
CREATE INDEX manifests_by_parent ON manifests (tenant_id, parent_id);
