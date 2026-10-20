package blob

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

// SQLite keeps every object as a row of one WAL-mode database; it is the single-file, transactional option.
type SQLite struct {
	w, r *sql.DB
}

const sqliteSchema = `CREATE TABLE IF NOT EXISTS objects (key TEXT PRIMARY KEY, size INTEGER NOT NULL, data BLOB NOT NULL) WITHOUT ROWID`

// OpenSQLite opens or creates the object database; synchronous may be "FULL" (default) or "NORMAL".
func OpenSQLite(path, synchronous string) (*SQLite, error) {
	if synchronous == "" {
		synchronous = "FULL"
	}
	if synchronous != "FULL" && synchronous != "NORMAL" {
		return nil, fmt.Errorf("synchronous must be FULL or NORMAL")
	}
	dsn := func(writer bool) string {
		q := url.Values{}
		q.Add("_pragma", "busy_timeout(10000)")
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous("+synchronous+")")
		if writer {
			q.Set("_txlock", "immediate")
		} else {
			q.Add("_pragma", "query_only(ON)")
		}
		return "file:" + path + "?" + q.Encode()
	}
	w, err := sql.Open("sqlite", dsn(true))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if _, err := w.Exec(sqliteSchema); err != nil {
		_ = w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn(false))
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(8)
	return &SQLite{w: w, r: r}, nil
}

func (s *SQLite) Close() error { return errors.Join(s.r.Close(), s.w.Close()) }
func (s *SQLite) Name() string { return "sqlite" }

func (s *SQLite) Put(ctx context.Context, key string, data []byte) error {
	if !ValidKey(key) {
		return ErrBadKey
	}
	if len(data) > MaxObject {
		return ErrTooLarge
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO objects (key, size, data) VALUES (?, ?, ?) ON CONFLICT (key) DO NOTHING`, key, len(data), data)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var old []byte
		if err := tx.QueryRowContext(ctx, `SELECT data FROM objects WHERE key = ?`, key).Scan(&old); err != nil {
			_ = tx.Rollback()
			return err
		}
		_ = tx.Rollback()
		if bytes.Equal(old, data) {
			return nil
		}
		return ErrOverwrite
	}
	return tx.Commit()
}

func (s *SQLite) Get(ctx context.Context, key string) ([]byte, error) {
	if !ValidKey(key) {
		return nil, ErrBadKey
	}
	var b []byte
	err := s.r.QueryRowContext(ctx, `SELECT data FROM objects WHERE key = ?`, key).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s *SQLite) Stat(ctx context.Context, key string) (Info, error) {
	if !ValidKey(key) {
		return Info{}, ErrBadKey
	}
	var n int64
	err := s.r.QueryRowContext(ctx, `SELECT size FROM objects WHERE key = ?`, key).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return Info{}, ErrNotFound
	}
	return Info{Key: key, Size: n}, err
}

func (s *SQLite) Delete(ctx context.Context, key string) error {
	if !ValidKey(key) {
		return ErrBadKey
	}
	res, err := s.w.ExecContext(ctx, `DELETE FROM objects WHERE key = ?`, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) List(ctx context.Context, prefix, after string, limit int) ([]Info, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	hi := prefix + "\U0010FFFF"
	rows, err := s.r.QueryContext(ctx, `SELECT key, size FROM objects WHERE key >= ? AND key < ? AND key > ? ORDER BY key LIMIT ?`, prefix, hi, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Info
	for rows.Next() {
		var i Info
		if err := rows.Scan(&i.Key, &i.Size); err != nil {
			return nil, err
		}
		if strings.HasPrefix(i.Key, prefix) {
			out = append(out, i)
		}
	}
	return out, rows.Err()
}

// Checkpoint folds the write-ahead log into the database file; a benchmark calls it before measuring size.
func (s *SQLite) Checkpoint() error {
	_, err := s.w.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// Vacuum rewrites the file without the pages deleted objects left behind.
func (s *SQLite) Vacuum() error {
	if _, err := s.w.Exec(`VACUUM`); err != nil {
		return err
	}
	return s.Checkpoint()
}

var _ Backend = (*SQLite)(nil)
