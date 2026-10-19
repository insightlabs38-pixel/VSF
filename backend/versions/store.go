package versions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/insightlabs38-pixel/vsf/backend/ir"
)

var (
	ErrLocked  = errors.New("another process holds this store")
	ErrCrashed = errors.New("simulated crash")
)

// Refusal is a promotion or lookup the store will not honour; nothing was changed.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return "refused: " + r.Reason }

func refuse(format string, a ...any) error { return &Refusal{Reason: fmt.Sprintf(format, a...)} }

// Config injects everything the store may not decide for itself.
type Config struct {
	Verifier Verifier
	// Trust is the signing-key set, supplied from outside the store.
	Trust []byte
	Now   func() int64
	// CrashAt is for tests: returning true at a named point abandons the operation as a dead process would.
	CrashAt func(point string) bool
}

type mark struct {
	Sequence int64  `json:"sequence"`
	Digest   string `json:"digest"`
}

type pointer struct {
	ID    string          `json:"id"`
	Marks map[string]mark `json:"marks"`
}

type intent struct {
	Txn     int    `json:"txn"`
	State   string `json:"state"`
	Channel string `json:"channel,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	KeyID   string `json:"key_id,omitempty"`
	Mark    *mark  `json:"mark,omitempty"`
}

// Store keeps immutable manifests and artifacts by digest and one head pointer per channel.
type Store struct {
	dir  string
	cfg  Config
	mu   sync.Mutex
	lock *os.File
	txn  int
}

type Request struct {
	Doc []byte
	// Blobs carries artifacts the store does not hold yet, keyed by digest.
	Blobs map[string][]byte
}

func name(digest string) string { return strings.ReplaceAll(digest, ":", "-") }

func Open(dir string, cfg Config) (*Store, error) {
	if cfg.Verifier == nil || cfg.Now == nil {
		return nil, errors.New("a verifier and a clock are required")
	}
	for _, sub := range []string{"blobs", "manifests", "channels", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, ErrLocked
	}
	s := &Store{dir: dir, cfg: cfg, lock: lock}
	if err := s.recover(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	return s.lock.Close()
}

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.dir}, parts...)...)
}

func (s *Store) crash(point string) bool { return s.cfg.CrashAt != nil && s.cfg.CrashAt(point) }

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// writeAtomic leaves either the old file or the whole new one, never a mixture.
func (s *Store) writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(s.path("tmp"), "w-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// artifactOK checks a blob against its reference: IR documents by typed digest, other artifacts by content hash.
func artifactOK(a ArtifactRef, blob []byte) error {
	if int64(len(blob)) != a.Bytes {
		return refuse("artifact_size_mismatch:%s", a.Digest)
	}
	switch a.Role {
	case "observation", "semantic", "operation_contract", "presentation":
		un, err := ir.DecodeDocument(blob)
		if err != nil {
			return refuse("artifact_undecodable:%s", a.Digest)
		}
		want := map[string]string{"observation": "observation", "semantic": "semantic", "operation_contract": "operation", "presentation": "presentation"}[a.Role]
		if un.Unsafe().Family != want {
			return refuse("artifact_wrong_family:%s", a.Digest)
		}
		if un.Digest() != a.Digest {
			return refuse("artifact_digest_mismatch:%s", a.Digest)
		}
	default:
		if sha(blob) != a.Digest {
			return refuse("artifact_digest_mismatch:%s", a.Digest)
		}
	}
	return nil
}

func (s *Store) readBlob(digest string) ([]byte, error) {
	return os.ReadFile(s.path("blobs", name(digest)))
}

func (s *Store) head(channel string) (pointer, bool, error) {
	raw, err := os.ReadFile(s.path("channels", channel+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return pointer{Marks: map[string]mark{}}, false, nil
	}
	if err != nil {
		return pointer{}, false, err
	}
	var p pointer
	if err := json.Unmarshal(raw, &p); err != nil {
		return pointer{}, false, fmt.Errorf("channel %s pointer is corrupt: %w", channel, err)
	}
	if p.Marks == nil {
		p.Marks = map[string]mark{}
	}
	return p, true, nil
}

// Head is the manifest a channel currently points at.
func (s *Store) Head(channel string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok, err := s.head(channel)
	if err != nil || !ok {
		return "", false
	}
	return p.ID, true
}

func (s *Store) Manifest(id string) ([]byte, bool) {
	b, err := os.ReadFile(s.path("manifests", name(id)+".json"))
	return b, err == nil
}

func (s *Store) Blob(digest string) ([]byte, bool) {
	b, err := s.readBlob(digest)
	return b, err == nil
}

func (s *Store) hasManifest(id string) bool {
	_, err := os.Stat(s.path("manifests", name(id)+".json"))
	return err == nil
}

func (s *Store) appendIntent(i intent) error {
	f, err := os.OpenFile(s.path("txn.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	line, _ := json.Marshal(i)
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// verdict asks Rust twice only when a watermark applies: once to learn the key, once with the mark in force.
func (s *Store) verdict(doc []byte) (Report, *mark, error) {
	now := s.cfg.Now()
	rep, err := s.cfg.Verifier.VerifyPromotion(doc, now, s.cfg.Trust, nil)
	if err != nil {
		return Report{}, nil, err
	}
	if !rep.Verified || rep.KeyID == "" {
		return rep, nil, nil
	}
	p, _, err := s.head(rep.Channel)
	if err != nil {
		return Report{}, nil, err
	}
	m, ok := p.Marks[rep.KeyID]
	if !ok {
		return rep, nil, nil
	}
	again, err := s.cfg.Verifier.VerifyPromotion(doc, now, s.cfg.Trust, &Accepted{Sequence: m.Sequence, Digest: m.Digest})
	return again, &m, err
}

// Promote stores a verified manifest and its artifacts atomically; stable and pinned ones then become the channel head.
func (s *Store) Promote(req Request) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep, _, err := s.verdict(req.Doc)
	if err != nil {
		return "", err
	}
	if rep.ID == "" {
		return "", refuse("undecodable")
	}
	headable := rep.Channel == "stable" || rep.Channel == "pinned"
	switch {
	case headable && !rep.Verified:
		return "", refuse("unverified:%s", rep.Reason)
	case !headable && !(rep.Valid && !rep.Promoted) && !rep.Verified:
		return "", refuse("unverified:%s", rep.Reason)
	}
	if reason, revoked := s.revokedManifest(rep); revoked {
		return "", refuse("revoked:%s", reason)
	}
	cur, had, err := s.head(rep.Channel)
	if err != nil {
		return "", err
	}
	if headable && had && cur.ID == rep.ID {
		return rep.ID, nil
	}
	for _, parent := range rep.Lineage {
		if !s.hasManifest(parent) {
			return "", refuse("missing_parent:%s", parent)
		}
	}
	if headable {
		switch {
		case had && rep.Previous != cur.ID:
			return "", refuse("stale_base:head %s", cur.ID)
		case !had && rep.Previous != "":
			return "", refuse("unknown_base:%s", rep.Previous)
		}
	}
	blobs := make(map[string][]byte, len(rep.Artifacts))
	for _, a := range rep.Artifacts {
		blob, ok := req.Blobs[a.Digest]
		if !ok {
			held, err := s.readBlob(a.Digest)
			if err != nil {
				return "", refuse("artifact_missing:%s", a.Digest)
			}
			blob = held
		}
		if err := artifactOK(a, blob); err != nil {
			return "", err
		}
		blobs[a.Digest] = blob
	}
	for digest, blob := range blobs {
		if err := s.writeAtomic(s.path("blobs", name(digest)), blob); err != nil {
			return "", err
		}
	}
	if s.crash("blobs") {
		return "", ErrCrashed
	}
	if err := s.writeAtomic(s.path("manifests", name(rep.ID)+".json"), req.Doc); err != nil {
		return "", err
	}
	if s.crash("manifest") {
		return "", ErrCrashed
	}
	if !headable {
		return rep.ID, nil
	}
	next := pointer{ID: rep.ID, Marks: cur.Marks}
	m := mark{Sequence: rep.Sequence, Digest: rep.SignedDigest}
	next.Marks[rep.KeyID] = m
	s.txn++
	in := intent{Txn: s.txn, State: "intent", Channel: rep.Channel, From: cur.ID, To: rep.ID, KeyID: rep.KeyID, Mark: &m}
	if err := s.appendIntent(in); err != nil {
		return "", err
	}
	if s.crash("intent") {
		return "", ErrCrashed
	}
	raw, _ := json.Marshal(next)
	if err := s.writeAtomic(s.path("channels", rep.Channel+".json"), raw); err != nil {
		return "", err
	}
	if s.crash("pointer") {
		return "", ErrCrashed
	}
	if err := s.appendIntent(intent{Txn: s.txn, State: "done"}); err != nil {
		return "", err
	}
	return rep.ID, nil
}

// recover finishes or abandons the last promotion a dead process left half done.
func (s *Store) recover() error {
	raw, err := os.ReadFile(s.path("txn.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var open *intent
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var i intent
		if json.Unmarshal(line, &i) != nil {
			continue
		}
		if i.Txn > s.txn {
			s.txn = i.Txn
		}
		switch i.State {
		case "intent":
			cp := i
			open = &cp
		case "done", "aborted":
			open = nil
		}
	}
	if open == nil {
		return nil
	}
	cur, _, err := s.head(open.Channel)
	if err != nil {
		return err
	}
	if cur.ID == open.To {
		return s.appendIntent(intent{Txn: open.Txn, State: "done"})
	}
	if s.completeFor(open.To) == nil {
		next := pointer{ID: open.To, Marks: cur.Marks}
		next.Marks[open.KeyID] = *open.Mark
		data, _ := json.Marshal(next)
		if err := s.writeAtomic(s.path("channels", open.Channel+".json"), data); err != nil {
			return err
		}
		return s.appendIntent(intent{Txn: open.Txn, State: "done"})
	}
	return s.appendIntent(intent{Txn: open.Txn, State: "aborted"})
}

// completeFor proves a stored manifest and every artifact it names are present and intact.
func (s *Store) completeFor(id string) error {
	doc, ok := s.Manifest(id)
	if !ok {
		return refuse("manifest_missing:%s", id)
	}
	un, err := ir.DecodeDocument(doc)
	if err != nil || un.Digest() != id {
		return refuse("manifest_corrupt:%s", id)
	}
	rep, err := s.cfg.Verifier.VerifyPromotion(doc, s.cfg.Now(), s.cfg.Trust, nil)
	if err != nil {
		return err
	}
	for _, a := range rep.Artifacts {
		blob, err := s.readBlob(a.Digest)
		if err != nil {
			return refuse("artifact_missing:%s", a.Digest)
		}
		if err := artifactOK(a, blob); err != nil {
			return err
		}
	}
	return nil
}

// Verify walks every channel head and checks that what it points at is whole.
func (s *Store) Verify() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.path("channels"))
	if err != nil {
		return err
	}
	var problems []string
	for _, e := range entries {
		ch := strings.TrimSuffix(e.Name(), ".json")
		p, ok, err := s.head(ch)
		if err != nil || !ok {
			problems = append(problems, fmt.Sprintf("%s: unreadable pointer", ch))
			continue
		}
		if err := s.completeFor(p.ID); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", ch, err))
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}
