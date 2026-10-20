ALTER TABLE manifests ADD COLUMN parent_id TEXT;
ALTER TABLE manifests ADD CONSTRAINT manifests_parent_fk FOREIGN KEY (tenant_id, parent_id) REFERENCES manifests(tenant_id, id);

CREATE TABLE annotations (
  tenant_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  key TEXT NOT NULL CHECK (length(key) BETWEEN 1 AND 64),
  value TEXT NOT NULL CHECK (length(value) <= 4096),
  created_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, manifest_id, key),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
);
CREATE INDEX manifests_by_parent ON manifests (tenant_id, parent_id);
ALTER TABLE annotations ENABLE ROW LEVEL SECURITY;
ALTER TABLE annotations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON annotations USING (tenant_id = current_setting('vsf.tenant', true)) WITH CHECK (tenant_id = current_setting('vsf.tenant', true));
GRANT SELECT, INSERT, UPDATE, DELETE ON annotations TO vsf_app;
