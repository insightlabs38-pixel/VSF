package meta

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFS embed.FS

// Migration is one numbered, immutable schema step; its checksum is recorded when it is applied.
type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

// Migrations lists the shipped steps of one dialect in order.
func Migrations(dialect string) ([]Migration, error) {
	dir := "migrations/" + dialect
	entries, err := fs.ReadDir(migrationFS, dir)
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		name := e.Name()
		num, rest, ok := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v < 1 {
			return nil, fmt.Errorf("migration %s: name must be NNNN_name.sql", name)
		}
		b, err := fs.ReadFile(migrationFS, dir+"/"+name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		out = append(out, Migration{Version: v, Name: rest, SQL: string(b), Checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrations must be numbered 1..n without gaps; found %d at position %d", m.Version, i+1)
		}
	}
	return out, nil
}

const createMigrations = `CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL
) STRICT`

// migrate brings the database to the newest shipped step, refusing one that is newer or whose history differs.
func migrate(ctx context.Context, db *sql.DB, now func() int64, upTo int) error {
	all, err := Migrations("sqlite")
	if err != nil {
		return err
	}
	if upTo <= 0 || upTo > len(all) {
		upTo = len(all)
	}
	if _, err := db.ExecContext(ctx, createMigrations); err != nil {
		return err
	}
	applied := map[int]string{}
	rows, err := db.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return err
		}
		applied[v] = sum
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for v := range applied {
		if v > len(all) {
			return ErrSchemaTooNew
		}
	}
	for _, m := range all {
		if sum, ok := applied[m.Version]; ok && sum != m.Checksum {
			return fmt.Errorf("%w: version %d", ErrMigrationDrift, m.Version)
		}
	}
	for _, m := range all[:upTo] {
		if _, ok := applied[m.Version]; ok {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var already int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version = ?`, m.Version).Scan(&already); err != nil {
			_ = tx.Rollback()
			return err
		}
		if already > 0 {
			_ = tx.Rollback()
			continue
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d %s: %w", m.Version, m.Name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`, m.Version, m.Name, m.Checksum, now()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
