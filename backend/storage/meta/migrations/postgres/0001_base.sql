DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'vsf_app') THEN
    CREATE ROLE vsf_app NOLOGIN;
  END IF;
END $$;

CREATE TABLE tenants (
  id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 63),
  created_at BIGINT NOT NULL
);

CREATE TABLE artifacts (
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  digest TEXT NOT NULL CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
  size BIGINT NOT NULL CHECK (size >= 0),
  class TEXT NOT NULL CHECK (class IN ('public', 'private')),
  provenance JSONB NOT NULL DEFAULT '{}',
  created_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, digest)
);

CREATE TABLE manifests (
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  id TEXT NOT NULL CHECK (id ~ '^sha256:[0-9a-f]{64}$'),
  kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
  doc BYTEA NOT NULL,
  key_id TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, id)
);

CREATE TABLE manifest_artifacts (
  tenant_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  artifact_digest TEXT NOT NULL,
  PRIMARY KEY (tenant_id, manifest_id, artifact_digest),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id),
  FOREIGN KEY (tenant_id, artifact_digest) REFERENCES artifacts(tenant_id, digest)
);
CREATE INDEX manifest_artifacts_by_artifact ON manifest_artifacts (tenant_id, artifact_digest);

CREATE TABLE roots (
  tenant_id TEXT NOT NULL,
  channel TEXT NOT NULL CHECK (length(channel) BETWEEN 1 AND 64),
  manifest_id TEXT NOT NULL,
  seq BIGINT NOT NULL CHECK (seq >= 1),
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, channel),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
);

CREATE TABLE root_history (
  tenant_id TEXT NOT NULL,
  channel TEXT NOT NULL,
  seq BIGINT NOT NULL,
  manifest_id TEXT NOT NULL,
  at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, channel, seq),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
);

CREATE TABLE pins (
  tenant_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 128),
  entitlement TEXT NOT NULL DEFAULT '',
  created_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, manifest_id, reason),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
);

CREATE TABLE holds (
  tenant_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  holder TEXT NOT NULL CHECK (length(holder) BETWEEN 1 AND 128),
  reason TEXT NOT NULL DEFAULT '',
  created_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, manifest_id, holder),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
);

CREATE TABLE leases (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL CHECK (length(id) BETWEEN 1 AND 128),
  manifest_id TEXT NOT NULL,
  owner TEXT NOT NULL,
  expires_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
);
CREATE INDEX leases_by_tenant ON leases (tenant_id, expires_at);

CREATE TABLE intents (
  id BIGSERIAL PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  kind TEXT NOT NULL,
  payload TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('pending', 'done', 'abandoned')),
  created_at BIGINT NOT NULL,
  finished_at BIGINT
);
CREATE INDEX intents_pending ON intents (state, tenant_id);

-- Every row belongs to one tenant; the application role sees only the tenant named in vsf.tenant.
DO $$
DECLARE t TEXT;
BEGIN
  FOREACH t IN ARRAY ARRAY['artifacts','manifests','manifest_artifacts','roots','root_history','pins','holds','leases','intents'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''vsf.tenant'', true)) WITH CHECK (tenant_id = current_setting(''vsf.tenant'', true))', t);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO vsf_app', t);
  END LOOP;
END $$;
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenants USING (id = current_setting('vsf.tenant', true)) WITH CHECK (id = current_setting('vsf.tenant', true));
GRANT SELECT, INSERT ON tenants TO vsf_app;
GRANT USAGE ON SEQUENCE intents_id_seq TO vsf_app;
