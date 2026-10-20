package meta

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Options tunes the local store; the zero value is the durable, safe choice.
type Options struct {
	Now func() int64
	// BusyTimeout is how long a writer waits for another before failing.
	BusyTimeout time.Duration
	// Synchronous trades durability for speed ("FULL" by default; "NORMAL" keeps WAL consistency but may lose the last commits on power loss).
	Synchronous string
	// MigrateTo stops migration at a version, for upgrade tests; zero means the newest.
	MigrateTo int
}

// SQLite keeps metadata in one WAL-mode file: one serialized writer and a pool of readers.
type SQLite struct {
	w, r *sql.DB
	now  func() int64
}

func dsn(path string, o Options, writer bool) string {
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", o.BusyTimeout.Milliseconds()))
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "synchronous("+o.Synchronous+")")
	if writer {
		q.Set("_txlock", "immediate")
	} else {
		q.Add("_pragma", "query_only(ON)")
	}
	return "file:" + path + "?" + q.Encode()
}

// OpenSQLite opens or creates the database, applies pending migrations and refuses a database from a newer version.
func OpenSQLite(ctx context.Context, path string, o Options) (*SQLite, error) {
	if o.Now == nil {
		o.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if o.BusyTimeout <= 0 {
		o.BusyTimeout = 10 * time.Second
	}
	switch strings.ToUpper(o.Synchronous) {
	case "", "FULL":
		o.Synchronous = "FULL"
	case "NORMAL":
		o.Synchronous = "NORMAL"
	default:
		return nil, fmt.Errorf("%w: synchronous must be FULL or NORMAL", ErrInvalid)
	}
	w, err := sql.Open("sqlite", dsn(path, o, true))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if err := migrate(ctx, w, o.Now, o.MigrateTo); err != nil {
		_ = w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn(path, o, false))
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	return &SQLite{w: w, r: r, now: o.Now}, nil
}

func (s *SQLite) Close() error {
	e1, e2 := s.r.Close(), s.w.Close()
	return errors.Join(e1, e2)
}

func (s *SQLite) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := s.r.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

func code(err error) int {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()
	}
	return 0
}

func mapErr(err error) error {
	switch code(err) {
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
		return ErrMissingRef
	case sqlite3.SQLITE_CONSTRAINT_CHECK, sqlite3.SQLITE_CONSTRAINT_NOTNULL:
		return ErrInvalid
	}
	return err
}

func checkTenant(t Tenant) error {
	if !ValidTenant(t) {
		return fmt.Errorf("%w: tenant", ErrInvalid)
	}
	return nil
}

func (s *SQLite) tx(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLite) EnsureTenant(ctx context.Context, t Tenant) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	_, err := s.w.ExecContext(ctx, `INSERT INTO tenants (id, created_at) VALUES (?, ?) ON CONFLICT (id) DO NOTHING`, string(t), s.now())
	return mapErr(err)
}

func (s *SQLite) Tenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id FROM tenants ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, Tenant(id))
	}
	return out, rows.Err()
}

func (s *SQLite) PutArtifact(ctx context.Context, t Tenant, a ArtifactRecord) (bool, error) {
	if err := checkTenant(t); err != nil {
		return false, err
	}
	if !ValidDigest(a.Digest) || a.Size < 0 || (a.Class != Public && a.Class != Private) {
		return false, fmt.Errorf("%w: artifact", ErrInvalid)
	}
	if a.Provenance == "" {
		a.Provenance = "{}"
	}
	created := false
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO artifacts (tenant_id, digest, size, class, provenance, created_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (tenant_id, digest) DO NOTHING`, string(t), a.Digest, a.Size, string(a.Class), a.Provenance, s.now())
		if err != nil {
			return mapErr(err)
		}
		n, _ := res.RowsAffected()
		if n == 1 {
			created = true
			return nil
		}
		var size int64
		var class string
		if err := tx.QueryRowContext(ctx, `SELECT size, class FROM artifacts WHERE tenant_id = ? AND digest = ?`, string(t), a.Digest).Scan(&size, &class); err != nil {
			return err
		}
		if size != a.Size || class != string(a.Class) {
			return fmt.Errorf("%w: digest already recorded with a different size or class", ErrConflict)
		}
		return nil
	})
	return created, err
}

func (s *SQLite) Artifact(ctx context.Context, t Tenant, digest string) (ArtifactRecord, error) {
	var a ArtifactRecord
	if err := checkTenant(t); err != nil {
		return a, err
	}
	var class string
	err := s.r.QueryRowContext(ctx, `SELECT digest, size, class, provenance, created_at FROM artifacts WHERE tenant_id = ? AND digest = ?`,
		string(t), digest).Scan(&a.Digest, &a.Size, &class, &a.Provenance, &a.CreatedAt)
	a.Class = Class(class)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func clampLimit(n int) int {
	if n <= 0 || n > 1000 {
		return 1000
	}
	return n
}

func (s *SQLite) Artifacts(ctx context.Context, t Tenant, after string, limit int) ([]ArtifactRecord, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT digest, size, class, provenance, created_at FROM artifacts WHERE tenant_id = ? AND digest > ? ORDER BY digest LIMIT ?`,
		string(t), after, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArtifactRecord
	for rows.Next() {
		var a ArtifactRecord
		var class string
		if err := rows.Scan(&a.Digest, &a.Size, &class, &a.Provenance, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Class = Class(class)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteArtifact(ctx context.Context, t Tenant, digest string) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	res, err := s.w.ExecContext(ctx, `DELETE FROM artifacts WHERE tenant_id = ? AND digest = ?`, string(t), digest)
	if err != nil {
		if code(err) == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
			return fmt.Errorf("%w: the artifact is still referenced by a manifest", ErrConflict)
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func manifestDigest(doc []byte) string {
	sum := sha256.Sum256(doc)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *SQLite) PublishManifest(ctx context.Context, t Tenant, m ManifestRecord, artifacts []string) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	if !ValidDigest(m.ID) || m.ID != manifestDigest(m.Doc) || !validName(m.Kind) || m.KeyID == "" || (m.ParentID != "" && !ValidDigest(m.ParentID)) {
		return fmt.Errorf("%w: manifest", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, d := range artifacts {
		if !ValidDigest(d) || seen[d] {
			return fmt.Errorf("%w: artifact list", ErrInvalid)
		}
		seen[d] = true
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var existing []byte
		err := tx.QueryRowContext(ctx, `SELECT doc FROM manifests WHERE tenant_id = ? AND id = ?`, string(t), m.ID).Scan(&existing)
		if err == nil {
			if string(existing) != string(m.Doc) {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if m.ParentID != "" {
			var one int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM manifests WHERE tenant_id = ? AND id = ?`, string(t), m.ParentID).Scan(&one); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrMissingRef
				}
				return err
			}
		}
		var parent any
		if m.ParentID != "" {
			parent = m.ParentID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO manifests (tenant_id, id, kind, doc, key_id, created_at, parent_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(t), m.ID, m.Kind, m.Doc, m.KeyID, s.now(), parent); err != nil {
			return mapErr(err)
		}
		for _, d := range artifacts {
			if _, err := tx.ExecContext(ctx, `INSERT INTO manifest_artifacts (tenant_id, manifest_id, artifact_digest) VALUES (?, ?, ?)`, string(t), m.ID, d); err != nil {
				return mapErr(err)
			}
		}
		return nil
	})
}

func (s *SQLite) Manifest(ctx context.Context, t Tenant, id string) (ManifestRecord, error) {
	var m ManifestRecord
	if err := checkTenant(t); err != nil {
		return m, err
	}
	var parent sql.NullString
	err := s.r.QueryRowContext(ctx, `SELECT id, kind, doc, key_id, parent_id, created_at FROM manifests WHERE tenant_id = ? AND id = ?`, string(t), id).
		Scan(&m.ID, &m.Kind, &m.Doc, &m.KeyID, &parent, &m.CreatedAt)
	m.ParentID = parent.String
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *SQLite) ManifestArtifacts(ctx context.Context, t Tenant, id string) ([]string, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT artifact_digest FROM manifest_artifacts WHERE tenant_id = ? AND manifest_id = ? ORDER BY artifact_digest`, string(t), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *SQLite) Manifests(ctx context.Context, t Tenant, after string, limit int) ([]ManifestRecord, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, kind, doc, key_id, parent_id, created_at FROM manifests WHERE tenant_id = ? AND id > ? ORDER BY id LIMIT ?`,
		string(t), after, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManifestRecord
	for rows.Next() {
		var m ManifestRecord
		var parent sql.NullString
		if err := rows.Scan(&m.ID, &m.Kind, &m.Doc, &m.KeyID, &parent, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.ParentID = parent.String
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteManifest(ctx context.Context, t Tenant, id string) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM manifest_artifacts WHERE tenant_id = ? AND manifest_id = ?`, `DELETE FROM annotations WHERE tenant_id = ? AND manifest_id = ?`} {
			if _, err := tx.ExecContext(ctx, q, string(t), id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM leases WHERE tenant_id = ? AND manifest_id = ? AND expires_at <= ?`, string(t), id, s.now()); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM manifests WHERE tenant_id = ? AND id = ?`, string(t), id)
		if err != nil {
			if code(err) == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
				return fmt.Errorf("%w: the manifest is a root, pinned, held, leased or a parent", ErrConflict)
			}
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *SQLite) SetRoot(ctx context.Context, t Tenant, channel, manifestID string, expectSeq int64) (RootRecord, error) {
	var out RootRecord
	if err := checkTenant(t); err != nil {
		return out, err
	}
	if !validName(channel) || !ValidDigest(manifestID) || expectSeq < 0 {
		return out, fmt.Errorf("%w: root", ErrInvalid)
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var cur int64
		err := tx.QueryRowContext(ctx, `SELECT seq FROM roots WHERE tenant_id = ? AND channel = ?`, string(t), channel).Scan(&cur)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if cur != expectSeq {
			return ErrConflict
		}
		now := s.now()
		out = RootRecord{Channel: channel, ManifestID: manifestID, Seq: cur + 1, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO root_history (tenant_id, channel, seq, manifest_id, at) VALUES (?, ?, ?, ?, ?)`, string(t), channel, out.Seq, manifestID, now); err != nil {
			return mapErr(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO roots (tenant_id, channel, manifest_id, seq, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (tenant_id, channel) DO UPDATE SET manifest_id = excluded.manifest_id, seq = excluded.seq, updated_at = excluded.updated_at`,
			string(t), channel, manifestID, out.Seq, now)
		return mapErr(err)
	})
	return out, err
}

func (s *SQLite) Root(ctx context.Context, t Tenant, channel string) (RootRecord, error) {
	var r RootRecord
	if err := checkTenant(t); err != nil {
		return r, err
	}
	err := s.r.QueryRowContext(ctx, `SELECT channel, manifest_id, seq, updated_at FROM roots WHERE tenant_id = ? AND channel = ?`, string(t), channel).
		Scan(&r.Channel, &r.ManifestID, &r.Seq, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *SQLite) Roots(ctx context.Context, t Tenant) ([]RootRecord, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT channel, manifest_id, seq, updated_at FROM roots WHERE tenant_id = ? ORDER BY channel`, string(t))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RootRecord
	for rows.Next() {
		var r RootRecord
		if err := rows.Scan(&r.Channel, &r.ManifestID, &r.Seq, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLite) RootHistory(ctx context.Context, t Tenant, channel string) ([]RootRecord, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT channel, manifest_id, seq, at FROM root_history WHERE tenant_id = ? AND channel = ? ORDER BY seq`, string(t), channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RootRecord
	for rows.Next() {
		var r RootRecord
		if err := rows.Scan(&r.Channel, &r.ManifestID, &r.Seq, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLite) Pin(ctx context.Context, t Tenant, p Pin) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	if !ValidDigest(p.ManifestID) || !validName(p.Reason) {
		return fmt.Errorf("%w: pin", ErrInvalid)
	}
	_, err := s.w.ExecContext(ctx, `INSERT INTO pins (tenant_id, manifest_id, reason, entitlement, created_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		string(t), p.ManifestID, p.Reason, p.Entitlement, s.now())
	return mapErr(err)
}

func (s *SQLite) Unpin(ctx context.Context, t Tenant, manifestID, reason string) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	res, err := s.w.ExecContext(ctx, `DELETE FROM pins WHERE tenant_id = ? AND manifest_id = ? AND reason = ?`, string(t), manifestID, reason)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) Pins(ctx context.Context, t Tenant) ([]Pin, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT manifest_id, reason, entitlement, created_at FROM pins WHERE tenant_id = ? ORDER BY manifest_id, reason`, string(t))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pin
	for rows.Next() {
		var p Pin
		if err := rows.Scan(&p.ManifestID, &p.Reason, &p.Entitlement, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) Hold(ctx context.Context, t Tenant, h Hold) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	if !ValidDigest(h.ManifestID) || !validName(h.Holder) {
		return fmt.Errorf("%w: hold", ErrInvalid)
	}
	_, err := s.w.ExecContext(ctx, `INSERT INTO holds (tenant_id, manifest_id, holder, reason, created_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		string(t), h.ManifestID, h.Holder, h.Reason, s.now())
	return mapErr(err)
}

func (s *SQLite) Release(ctx context.Context, t Tenant, manifestID, holder string) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	res, err := s.w.ExecContext(ctx, `DELETE FROM holds WHERE tenant_id = ? AND manifest_id = ? AND holder = ?`, string(t), manifestID, holder)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) Holds(ctx context.Context, t Tenant) ([]Hold, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT manifest_id, holder, reason, created_at FROM holds WHERE tenant_id = ? ORDER BY manifest_id, holder`, string(t))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hold
	for rows.Next() {
		var h Hold
		if err := rows.Scan(&h.ManifestID, &h.Holder, &h.Reason, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *SQLite) Annotate(ctx context.Context, t Tenant, manifestID, key, value string) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	if !ValidDigest(manifestID) || !validName(key) || len(value) > 4096 {
		return fmt.Errorf("%w: annotation", ErrInvalid)
	}
	_, err := s.w.ExecContext(ctx, `INSERT INTO annotations (tenant_id, manifest_id, key, value, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, manifest_id, key) DO UPDATE SET value = excluded.value`, string(t), manifestID, key, value, s.now())
	return mapErr(err)
}

func (s *SQLite) Annotations(ctx context.Context, t Tenant, manifestID string) (map[string]string, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT key, value FROM annotations WHERE tenant_id = ? AND manifest_id = ?`, string(t), manifestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *SQLite) Acquire(ctx context.Context, t Tenant, l Lease) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	if l.ID == "" || !validName(l.Owner) || !ValidDigest(l.ManifestID) || l.ExpiresAt <= s.now() {
		return fmt.Errorf("%w: lease", ErrInvalid)
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var owner string
		var exp int64
		err := tx.QueryRowContext(ctx, `SELECT owner, expires_at FROM leases WHERE id = ? AND tenant_id = ?`, l.ID, string(t)).Scan(&owner, &exp)
		switch {
		case err == nil && owner != l.Owner && exp > s.now():
			return ErrLeaseHeld
		case err == nil:
			_, err = tx.ExecContext(ctx, `UPDATE leases SET owner = ?, manifest_id = ?, expires_at = ? WHERE id = ? AND tenant_id = ?`, l.Owner, l.ManifestID, l.ExpiresAt, l.ID, string(t))
			return mapErr(err)
		case errors.Is(err, sql.ErrNoRows):
			_, err = tx.ExecContext(ctx, `INSERT INTO leases (id, tenant_id, manifest_id, owner, expires_at) VALUES (?, ?, ?, ?, ?)`, l.ID, string(t), l.ManifestID, l.Owner, l.ExpiresAt)
			if code(err) == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY {
				return ErrLeaseHeld
			}
			return mapErr(err)
		}
		return err
	})
}

func (s *SQLite) Renew(ctx context.Context, t Tenant, id string, expiresAt int64) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	res, err := s.w.ExecContext(ctx, `UPDATE leases SET expires_at = ? WHERE id = ? AND tenant_id = ? AND expires_at > ?`, expiresAt, id, string(t), s.now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) ReleaseLease(ctx context.Context, t Tenant, id string) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	res, err := s.w.ExecContext(ctx, `DELETE FROM leases WHERE id = ? AND tenant_id = ?`, id, string(t))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Leases returns the leases still in force; an expired one protects nothing.
func (s *SQLite) Leases(ctx context.Context, t Tenant) ([]Lease, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, manifest_id, owner, expires_at FROM leases WHERE tenant_id = ? AND expires_at > ? ORDER BY id`, string(t), s.now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lease
	for rows.Next() {
		var l Lease
		if err := rows.Scan(&l.ID, &l.ManifestID, &l.Owner, &l.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *SQLite) BeginIntent(ctx context.Context, t Tenant, kind, payload string) (int64, error) {
	if err := checkTenant(t); err != nil {
		return 0, err
	}
	if !validName(kind) {
		return 0, fmt.Errorf("%w: intent", ErrInvalid)
	}
	res, err := s.w.ExecContext(ctx, `INSERT INTO intents (tenant_id, kind, payload, state, created_at) VALUES (?, ?, ?, 'pending', ?)`, string(t), kind, payload, s.now())
	if err != nil {
		return 0, mapErr(err)
	}
	return res.LastInsertId()
}

func (s *SQLite) FinishIntent(ctx context.Context, t Tenant, id int64, state IntentState) error {
	if err := checkTenant(t); err != nil {
		return err
	}
	if state != IntentDone && state != IntentAbandoned {
		return fmt.Errorf("%w: intent state", ErrInvalid)
	}
	res, err := s.w.ExecContext(ctx, `UPDATE intents SET state = ?, finished_at = ? WHERE id = ? AND tenant_id = ? AND state = 'pending'`, string(state), s.now(), id, string(t))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) PendingIntents(ctx context.Context, t Tenant) ([]Intent, error) {
	if err := checkTenant(t); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT id, kind, payload, state, created_at FROM intents WHERE tenant_id = ? AND state = 'pending' ORDER BY id`, string(t))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Intent
	for rows.Next() {
		var i Intent
		var st string
		if err := rows.Scan(&i.ID, &i.Kind, &i.Payload, &st, &i.CreatedAt); err != nil {
			return nil, err
		}
		i.State = IntentState(st)
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *SQLite) PruneRootHistory(ctx context.Context, t Tenant, channel string, keep int) (int, error) {
	if err := checkTenant(t); err != nil {
		return 0, err
	}
	if keep < 1 {
		return 0, fmt.Errorf("%w: keep", ErrInvalid)
	}
	res, err := s.w.ExecContext(ctx, `DELETE FROM root_history WHERE tenant_id = ? AND channel = ? AND seq NOT IN
		(SELECT seq FROM root_history WHERE tenant_id = ? AND channel = ? ORDER BY seq DESC LIMIT ?)`, string(t), channel, string(t), channel, keep)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLite) PublicDigestInUse(ctx context.Context, digest string) (bool, error) {
	if !ValidDigest(digest) {
		return false, fmt.Errorf("%w: digest", ErrInvalid)
	}
	var one int
	err := s.r.QueryRowContext(ctx, `SELECT 1 FROM artifacts WHERE digest = ? AND class = 'public' LIMIT 1`, digest).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

var _ Store = (*SQLite)(nil)
var _ Maintenance = (*SQLite)(nil)
