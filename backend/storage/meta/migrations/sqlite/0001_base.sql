CREATE TABLE tenants (
  id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 63),
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE artifacts (
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  digest TEXT NOT NULL CHECK (length(digest) = 71 AND digest LIKE 'sha256:%'),
  size INTEGER NOT NULL CHECK (size >= 0),
  class TEXT NOT NULL CHECK (class IN ('public', 'private')),
  provenance TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, digest)
) STRICT;

CREATE TABLE manifests (
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  id TEXT NOT NULL CHECK (length(id) = 71 AND id LIKE 'sha256:%'),
  kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
  doc BLOB NOT NULL,
  key_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, id)
) STRICT;

CREATE TABLE manifest_artifacts (
  tenant_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  artifact_digest TEXT NOT NULL,
  PRIMARY KEY (tenant_id, manifest_id, artifact_digest),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id),
  FOREIGN KEY (tenant_id, artifact_digest) REFERENCES artifacts(tenant_id, digest)
) STRICT;
CREATE INDEX manifest_artifacts_by_artifact ON manifest_artifacts (tenant_id, artifact_digest);

CREATE TABLE roots (
  tenant_id TEXT NOT NULL,
  channel TEXT NOT NULL CHECK (length(channel) BETWEEN 1 AND 64),
  manifest_id TEXT NOT NULL,
  seq INTEGER NOT NULL CHECK (seq >= 1),
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, channel),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
) STRICT;

CREATE TABLE root_history (
  tenant_id TEXT NOT NULL,
  channel TEXT NOT NULL,
  seq INTEGER NOT NULL,
  manifest_id TEXT NOT NULL,
  at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, channel, seq),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
) STRICT;

CREATE TABLE pins (
  tenant_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 128),
  entitlement TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, manifest_id, reason),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
) STRICT;

CREATE TABLE holds (
  tenant_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  holder TEXT NOT NULL CHECK (length(holder) BETWEEN 1 AND 128),
  reason TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, manifest_id, holder),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
) STRICT;

CREATE TABLE leases (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL CHECK (length(id) BETWEEN 1 AND 128),
  manifest_id TEXT NOT NULL,
  owner TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, manifest_id) REFERENCES manifests(tenant_id, id)
) STRICT;
CREATE INDEX leases_by_tenant ON leases (tenant_id, expires_at);

CREATE TABLE intents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tenant_id TEXT NOT NULL REFERENCES tenants(id),
  kind TEXT NOT NULL,
  payload TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('pending', 'done', 'abandoned')),
  created_at INTEGER NOT NULL,
  finished_at INTEGER
) STRICT;
CREATE INDEX intents_pending ON intents (state, tenant_id);
