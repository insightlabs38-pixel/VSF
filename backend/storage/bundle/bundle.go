// Package bundle exports a tenant's versions to one portable file and imports them elsewhere, refusing a damaged file before it writes anything.
package bundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/insightlabs38-pixel/vsf/backend/storage/artifact"
	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
)

var (
	ErrCorrupt  = errors.New("the bundle is damaged or was altered")
	ErrTenant   = errors.New("the bundle belongs to another tenant")
	ErrTooLarge = errors.New("the bundle exceeds the size limit")
	ErrCrashed  = errors.New("simulated crash")
)

const (
	magic        = "VSFBNDL1\n"
	formatV1     = 1
	tagHeader    = 'H'
	tagArtifact  = 'A'
	tagManifest  = 'M'
	tagRoot      = 'R'
	tagPin       = 'P'
	tagHold      = 'O'
	tagTrailer   = 'T'
	trailerBytes = 1 + 8 + sha256.Size
	maxMeta      = 1 << 20
	maxRecords   = 1 << 22
	maxJSON      = 4 << 20
)

type header struct {
	Format    int    `json:"format"`
	Tenant    string `json:"tenant"`
	Artifacts int    `json:"artifacts"`
	Manifests int    `json:"manifests"`
	Roots     int    `json:"roots"`
	Pins      int    `json:"pins"`
	Holds     int    `json:"holds"`
}

type artifactMeta struct {
	Digest     string          `json:"digest"`
	Size       int64           `json:"size"`
	Class      meta.Class      `json:"class"`
	Provenance json.RawMessage `json:"provenance"`
	// Sealed marks a private artifact carried as stored, encrypted; it opens only where the tenant's keys are held.
	Sealed bool `json:"sealed,omitempty"`
}

type manifestRec struct {
	ID  string `json:"id"`
	Doc []byte `json:"doc"`
}

type rootRec struct {
	Channel    string `json:"channel"`
	ManifestID string `json:"manifest_id"`
	Seq        int64  `json:"seq"`
}

type pinRec struct {
	ManifestID  string `json:"manifest_id"`
	Reason      string `json:"reason"`
	Entitlement string `json:"entitlement"`
}

type holdRec struct {
	ManifestID string `json:"manifest_id"`
	Holder     string `json:"holder"`
	Reason     string `json:"reason"`
}

// Source is what an export reads from.
type Source struct {
	Store *artifact.Store
	Meta  meta.Store
}

// Select names the versions to export; none means every version of the tenant. Each one brings its ancestors and artifacts.
type Select struct{ Manifests []string }

// Summary counts what a bundle holds.
type Summary struct {
	Artifacts int   `json:"artifacts"`
	Manifests int   `json:"manifests"`
	Roots     int   `json:"roots"`
	Pins      int   `json:"pins"`
	Holds     int   `json:"holds"`
	Bytes     int64 `json:"bytes"`
}

type recWriter struct {
	w io.Writer
	h interface {
		io.Writer
		Sum([]byte) []byte
	}
	n   int64
	err error
}

func (rw *recWriter) write(b []byte) {
	if rw.err != nil {
		return
	}
	if _, err := rw.w.Write(b); err != nil {
		rw.err = err
		return
	}
	rw.h.Write(b)
	rw.n += int64(len(b))
}

func (rw *recWriter) record(tag byte, parts ...[]byte) {
	var n uint64
	for _, p := range parts {
		n += uint64(len(p))
	}
	var head [9]byte
	head[0] = tag
	binary.BigEndian.PutUint64(head[1:], n)
	rw.write(head[:])
	for _, p := range parts {
		rw.write(p)
	}
}

func (rw *recWriter) json(tag byte, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		rw.err = err
		return
	}
	rw.record(tag, b)
}

func page[T any](fetch func(after string) ([]T, string, error)) ([]T, error) {
	var out []T
	for after := ""; ; {
		ps, last, err := fetch(after)
		if err != nil {
			return nil, err
		}
		out = append(out, ps...)
		if last == "" {
			return out, nil
		}
		after = last
	}
}

// Export writes a deterministic bundle: the same store state always yields the same bytes.
func Export(ctx context.Context, src Source, t meta.Tenant, sel Select, w io.Writer) (Summary, error) {
	var sum Summary
	all, err := page(func(after string) ([]meta.ManifestRecord, string, error) {
		ps, err := src.Meta.Manifests(ctx, t, after, 500)
		if err != nil || len(ps) < 500 {
			return ps, "", err
		}
		return ps, ps[len(ps)-1].ID, nil
	})
	if err != nil {
		return sum, err
	}
	by := make(map[string]meta.ManifestRecord, len(all))
	for _, m := range all {
		by[m.ID] = m
	}
	chosen := map[string]bool{}
	if len(sel.Manifests) == 0 {
		for id := range by {
			chosen[id] = true
		}
	}
	for _, id := range sel.Manifests {
		for ; id != "" && !chosen[id]; id = by[id].ParentID {
			if _, ok := by[id]; !ok {
				return sum, fmt.Errorf("%w: %s", meta.ErrNotFound, id)
			}
			chosen[id] = true
		}
	}
	ids := make([]string, 0, len(chosen))
	for id := range chosen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	digests := map[string]bool{}
	for _, id := range ids {
		ds, err := src.Meta.ManifestArtifacts(ctx, t, id)
		if err != nil {
			return sum, err
		}
		for _, d := range ds {
			digests[d] = true
		}
	}
	dl := make([]string, 0, len(digests))
	for d := range digests {
		dl = append(dl, d)
	}
	sort.Strings(dl)
	roots, err := src.Meta.Roots(ctx, t)
	if err != nil {
		return sum, err
	}
	pins, err := src.Meta.Pins(ctx, t)
	if err != nil {
		return sum, err
	}
	holds, err := src.Meta.Holds(ctx, t)
	if err != nil {
		return sum, err
	}
	var rr []rootRec
	for _, r := range roots {
		if chosen[r.ManifestID] {
			rr = append(rr, rootRec{r.Channel, r.ManifestID, r.Seq})
		}
	}
	sort.Slice(rr, func(i, j int) bool { return rr[i].Channel < rr[j].Channel })
	var pr []pinRec
	for _, p := range pins {
		if chosen[p.ManifestID] {
			pr = append(pr, pinRec{p.ManifestID, p.Reason, p.Entitlement})
		}
	}
	var hr []holdRec
	for _, h := range holds {
		if chosen[h.ManifestID] {
			hr = append(hr, holdRec{h.ManifestID, h.Holder, h.Reason})
		}
	}
	rw := &recWriter{w: w, h: sha256.New()}
	rw.write([]byte(magic))
	rw.json(tagHeader, header{Format: formatV1, Tenant: string(t), Artifacts: len(dl), Manifests: len(ids), Roots: len(rr), Pins: len(pr), Holds: len(hr)})
	for _, d := range dl {
		rec, err := src.Meta.Artifact(ctx, t, d)
		if err != nil {
			return sum, err
		}
		var data []byte
		sealed := rec.Class == meta.Private && src.Store.Sealed()
		if sealed {
			_, data, err = src.Store.SealedObject(ctx, t, d)
		} else {
			data, err = src.Store.Artifact(ctx, t, d)
		}
		if err != nil {
			return sum, fmt.Errorf("%s: %w", d, err)
		}
		mj, err := json.Marshal(artifactMeta{Digest: d, Size: rec.Size, Class: rec.Class, Provenance: json.RawMessage(rec.Provenance), Sealed: sealed})
		if err != nil {
			return sum, err
		}
		var ml [4]byte
		binary.BigEndian.PutUint32(ml[:], uint32(len(mj)))
		rw.record(tagArtifact, ml[:], mj, data)
	}
	for _, id := range ids {
		rw.json(tagManifest, manifestRec{ID: id, Doc: by[id].Doc})
	}
	for _, r := range rr {
		rw.json(tagRoot, r)
	}
	for _, p := range pr {
		rw.json(tagPin, p)
	}
	for _, h := range hr {
		rw.json(tagHold, h)
	}
	if rw.err != nil {
		return sum, rw.err
	}
	var tail [trailerBytes]byte
	tail[0] = tagTrailer
	binary.BigEndian.PutUint64(tail[1:], sha256.Size)
	copy(tail[9:], rw.h.Sum(nil))
	if _, err := w.Write(tail[:]); err != nil {
		return sum, err
	}
	return Summary{Artifacts: len(dl), Manifests: len(ids), Roots: len(rr), Pins: len(pr), Holds: len(hr), Bytes: rw.n + trailerBytes}, nil
}

// Limits bound what an import accepts; the zero value allows 1 GiB.
type Limits struct {
	MaxBytes int64
	TempDir  string
}

// Dest is what an import writes to.
type Dest struct {
	Store *artifact.Store
	Meta  meta.Store
	// CrashAt is for tests: returning true abandons the commit at a phase after done steps, as a dead process would.
	CrashAt func(phase string, done int) bool
}

// Result says what an import did; versions already present are counted, not rewritten.
type Result struct {
	Artifacts        int `json:"artifacts"`
	ArtifactsPresent int `json:"artifacts_present"`
	Manifests        int `json:"manifests"`
	ManifestsPresent int `json:"manifests_present"`
	Roots            int `json:"roots"`
	// RootsKept counts channels that already pointed elsewhere; an import never moves a live channel.
	RootsKept int `json:"roots_kept"`
	Pins      int `json:"pins"`
	Holds     int `json:"holds"`
}

type spot struct {
	meta artifactMeta
	off  int64
	n    int64
}

type parsed struct {
	hdr       header
	artifacts []spot
	manifests []manifestRec
	roots     []rootRec
	pins      []pinRec
	holds     []holdRec
	digest    string
	parent    map[string]string
}

func bad(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, args...))
}

// Import reads the whole bundle and proves it sound before it commits anything; a damaged bundle changes nothing.
func Import(ctx context.Context, dst Dest, t meta.Tenant, r io.Reader, lim Limits) (Result, error) {
	var res Result
	if lim.MaxBytes <= 0 {
		lim.MaxBytes = 1 << 30
	}
	f, err := os.CreateTemp(lim.TempDir, "vsf-bundle-*")
	if err != nil {
		return res, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(r, lim.MaxBytes+1))
	if err != nil {
		return res, err
	}
	if n > lim.MaxBytes {
		return res, ErrTooLarge
	}
	p, err := parse(ctx, dst, t, f, n)
	if err != nil {
		return res, err
	}
	return commit(ctx, dst, t, f, p)
}

func parse(ctx context.Context, dst Dest, t meta.Tenant, f *os.File, size int64) (*parsed, error) {
	if size < int64(len(magic))+trailerBytes {
		return nil, bad("too short")
	}
	body := size - trailerBytes
	var tail [trailerBytes]byte
	if _, err := f.ReadAt(tail[:], body); err != nil {
		return nil, err
	}
	if tail[0] != tagTrailer || binary.BigEndian.Uint64(tail[1:9]) != sha256.Size {
		return nil, bad("no trailer")
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, body)); err != nil {
		return nil, err
	}
	if !bytes.Equal(h.Sum(nil), tail[9:]) {
		return nil, bad("checksum mismatch")
	}
	rd := io.NewSectionReader(f, 0, body)
	m := make([]byte, len(magic))
	if _, err := io.ReadFull(rd, m); err != nil || string(m) != magic {
		return nil, bad("not a bundle")
	}
	p := &parsed{digest: hex.EncodeToString(tail[9:]), parent: map[string]string{}}
	have := map[string]artifactMeta{}
	byManifest := map[string]manifestRec{}
	pos := int64(len(magic))
	for count := 0; pos < body; count++ {
		if count > maxRecords {
			return nil, bad("too many records")
		}
		var head [9]byte
		if _, err := io.ReadFull(rd, head[:]); err != nil {
			return nil, bad("truncated record")
		}
		tag, n := head[0], binary.BigEndian.Uint64(head[1:])
		if n > uint64(body-pos-9) {
			return nil, bad("record overruns the bundle")
		}
		start := pos + 9
		pos = start + int64(n)
		if (count == 0) != (tag == tagHeader) {
			return nil, bad("header must come first and once")
		}
		if tag == tagArtifact {
			sp, err := readArtifact(rd, dst, t, start, int64(n))
			if err != nil {
				return nil, err
			}
			if _, dup := have[sp.meta.Digest]; dup {
				return nil, bad("artifact repeated")
			}
			p.artifacts = append(p.artifacts, *sp)
			have[sp.meta.Digest] = sp.meta
			continue
		}
		if n > maxJSON {
			return nil, bad("record too large")
		}
		raw := make([]byte, n)
		if _, err := io.ReadFull(rd, raw); err != nil {
			return nil, bad("truncated record")
		}
		if err := p.add(tag, raw); err != nil {
			return nil, err
		}
	}
	if p.hdr.Format != formatV1 {
		return nil, bad("unknown format")
	}
	if p.hdr.Tenant != string(t) {
		return nil, ErrTenant
	}
	if p.hdr.Artifacts != len(p.artifacts) || p.hdr.Manifests != len(p.manifests) || p.hdr.Roots != len(p.roots) || p.hdr.Pins != len(p.pins) || p.hdr.Holds != len(p.holds) {
		return nil, bad("counts do not match the header")
	}
	for _, mr := range p.manifests {
		if _, dup := byManifest[mr.ID]; dup {
			return nil, bad("manifest repeated")
		}
		byManifest[mr.ID] = mr
	}
	return p, p.check(ctx, dst, t, have, byManifest)
}

func readArtifact(rd *io.SectionReader, dst Dest, t meta.Tenant, start, n int64) (*spot, error) {
	if n < 4 {
		return nil, bad("artifact record too short")
	}
	var ml [4]byte
	if _, err := io.ReadFull(rd, ml[:]); err != nil {
		return nil, bad("truncated record")
	}
	mlen := int64(binary.BigEndian.Uint32(ml[:]))
	if mlen == 0 || mlen > maxMeta || 4+mlen > n {
		return nil, bad("artifact header")
	}
	mj := make([]byte, mlen)
	if _, err := io.ReadFull(rd, mj); err != nil {
		return nil, bad("truncated record")
	}
	sp := &spot{off: start + 4 + mlen, n: n - 4 - mlen}
	am := &sp.meta
	if err := strictJSON(mj, am); err != nil {
		return nil, bad("artifact header")
	}
	if !meta.ValidDigest(am.Digest) || (am.Class != meta.Public && am.Class != meta.Private) || am.Size < 0 {
		return nil, bad("artifact header")
	}
	if err := artifact.ValidProvenance(am.Provenance); err != nil {
		return nil, bad("artifact provenance")
	}
	if am.Size > int64(dst.Store.MaxArtifact()) {
		return nil, bad("artifact too large for this store")
	}
	wantSealed := am.Class == meta.Private && dst.Store.Sealed()
	if am.Sealed != wantSealed {
		return nil, bad("artifact %s is %s but this store keeps it %s", am.Digest, sealedWord(am.Sealed), sealedWord(wantSealed))
	}
	if am.Sealed {
		if sp.n != am.Size+int64(dst.Store.SealOverhead()) {
			return nil, bad("artifact header")
		}
		data := make([]byte, sp.n)
		if _, err := io.ReadFull(rd, data); err != nil {
			return nil, bad("truncated record")
		}
		if err := dst.Store.CheckSealed(t, am.Digest, am.Size, data); err != nil {
			return nil, bad("artifact %s does not open under this store's keys", am.Digest)
		}
		return sp, nil
	}
	if am.Size != sp.n {
		return nil, bad("artifact header")
	}
	h := sha256.New()
	if _, err := io.CopyN(h, rd, sp.n); err != nil {
		return nil, bad("truncated record")
	}
	if "sha256:"+hex.EncodeToString(h.Sum(nil)) != am.Digest {
		return nil, bad("artifact %s does not match its digest", am.Digest)
	}
	return sp, nil
}

func sealedWord(b bool) string {
	if b {
		return "sealed"
	}
	return "plain"
}

func strictJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func (p *parsed) add(tag byte, raw []byte) error {
	var err error
	switch tag {
	case tagHeader:
		err = strictJSON(raw, &p.hdr)
	case tagManifest:
		var m manifestRec
		if err = strictJSON(raw, &m); err == nil {
			p.manifests = append(p.manifests, m)
		}
	case tagRoot:
		var r rootRec
		if err = strictJSON(raw, &r); err == nil {
			p.roots = append(p.roots, r)
		}
	case tagPin:
		var x pinRec
		if err = strictJSON(raw, &x); err == nil {
			p.pins = append(p.pins, x)
		}
	case tagHold:
		var x holdRec
		if err = strictJSON(raw, &x); err == nil {
			p.holds = append(p.holds, x)
		}
	default:
		return bad("unknown record %q", tag)
	}
	if err != nil {
		return bad("record %q: %v", tag, err)
	}
	return nil
}

// check proves every record sound and every reference resolvable, so the commit can only fail on I/O.
func (p *parsed) check(ctx context.Context, dst Dest, t meta.Tenant, have map[string]artifactMeta, byManifest map[string]manifestRec) error {
	known := func(id string) (bool, error) {
		if _, ok := byManifest[id]; ok {
			return true, nil
		}
		if _, err := dst.Meta.Manifest(ctx, t, id); err != nil {
			if errors.Is(err, meta.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	for _, mr := range p.manifests {
		if !meta.ValidDigest(mr.ID) || artifact.Digest(mr.Doc) != mr.ID {
			return bad("manifest identity")
		}
		m, err := dst.Store.CheckManifest(t, mr.Doc)
		if err != nil {
			return fmt.Errorf("%w: manifest %s: %v", ErrCorrupt, mr.ID, err)
		}
		for _, a := range m.Artifacts {
			if am, ok := have[a.Digest]; ok {
				if am.Size != a.Size || am.Class != a.Class {
					return bad("manifest and artifact disagree on %s", a.Digest)
				}
				continue
			}
			rec, err := dst.Meta.Artifact(ctx, t, a.Digest)
			if err != nil || rec.Size != a.Size || rec.Class != a.Class {
				return bad("manifest %s needs artifact %s, which is neither bundled nor stored", mr.ID, a.Digest)
			}
		}
		p.parent[mr.ID] = m.Parent
		if m.Parent != "" {
			if ok, err := known(m.Parent); err != nil {
				return err
			} else if !ok {
				return bad("manifest %s has no parent in the bundle or the store", mr.ID)
			}
		}
	}
	seen := map[string]bool{}
	for _, r := range p.roots {
		if !meta.ValidName(r.Channel) || seen[r.Channel] || r.Seq < 1 {
			return bad("root")
		}
		seen[r.Channel] = true
		if ok, err := known(r.ManifestID); err != nil {
			return err
		} else if !ok {
			return bad("root %s names a manifest that is not there", r.Channel)
		}
	}
	for _, x := range p.pins {
		if !meta.ValidName(x.Reason) || len(x.Entitlement) > 256 {
			return bad("pin")
		}
		if ok, err := known(x.ManifestID); err != nil {
			return err
		} else if !ok {
			return bad("pin names a manifest that is not there")
		}
	}
	for _, x := range p.holds {
		if !meta.ValidName(x.Holder) || len(x.Reason) > 1024 {
			return bad("hold")
		}
		if ok, err := known(x.ManifestID); err != nil {
			return err
		} else if !ok {
			return bad("hold names a manifest that is not there")
		}
	}
	return nil
}

func (d Dest) crash(phase string, done int) bool { return d.CrashAt != nil && d.CrashAt(phase, done) }

// commit applies a proven bundle. Every step is idempotent and logged under one intent, so a dead process is answered by importing again.
func commit(ctx context.Context, dst Dest, t meta.Tenant, f *os.File, p *parsed) (Result, error) {
	var res Result
	if err := dst.Meta.EnsureTenant(ctx, t); err != nil {
		return res, err
	}
	intent, err := dst.Meta.BeginIntent(ctx, t, "import", p.digest)
	if err != nil {
		return res, err
	}
	for i, sp := range p.artifacts {
		if dst.crash("artifacts", i) {
			return res, ErrCrashed
		}
		present := false
		if _, err := dst.Meta.Artifact(ctx, t, sp.meta.Digest); err == nil {
			present = true
		}
		data := make([]byte, sp.n)
		if _, err := f.ReadAt(data, sp.off); err != nil && !(errors.Is(err, io.EOF) && sp.n == 0) {
			return res, err
		}
		if sp.meta.Sealed {
			err = dst.Store.AdoptSealed(ctx, t, sp.meta.Digest, sp.meta.Size, sp.meta.Provenance, data)
		} else {
			_, err = dst.Store.PutArtifact(ctx, t, data, artifact.ArtifactInput{Class: sp.meta.Class, Provenance: sp.meta.Provenance})
		}
		if err != nil {
			return res, err
		}
		if present {
			res.ArtifactsPresent++
		} else {
			res.Artifacts++
		}
	}
	for i, mr := range parentFirst(p.manifests, p.parent) {
		if dst.crash("manifests", i) {
			return res, ErrCrashed
		}
		_, lookup := dst.Meta.Manifest(ctx, t, mr.ID)
		if _, err := dst.Store.AdoptManifest(ctx, t, mr.Doc); err != nil {
			return res, err
		}
		if lookup == nil {
			res.ManifestsPresent++
		} else {
			res.Manifests++
		}
	}
	for _, r := range p.roots {
		cur, err := dst.Meta.Root(ctx, t, r.Channel)
		switch {
		case err == nil && cur.ManifestID == r.ManifestID:
			res.Roots++
		case err == nil:
			res.RootsKept++
		case errors.Is(err, meta.ErrNotFound):
			if _, err := dst.Meta.SetRoot(ctx, t, r.Channel, r.ManifestID, 0); err != nil {
				return res, err
			}
			res.Roots++
		default:
			return res, err
		}
	}
	for _, x := range p.pins {
		if err := dst.Meta.Pin(ctx, t, meta.Pin{ManifestID: x.ManifestID, Reason: x.Reason, Entitlement: x.Entitlement}); err != nil {
			return res, err
		}
		res.Pins++
	}
	for _, x := range p.holds {
		if err := dst.Meta.Hold(ctx, t, meta.Hold{ManifestID: x.ManifestID, Holder: x.Holder, Reason: x.Reason}); err != nil {
			return res, err
		}
		res.Holds++
	}
	return res, dst.Meta.FinishIntent(ctx, t, intent, meta.IntentDone)
}

// parentFirst orders manifests so each follows its parent when the parent travels in the same bundle.
func parentFirst(ms []manifestRec, parent map[string]string) []manifestRec {
	byID := make(map[string]manifestRec, len(ms))
	for _, m := range ms {
		byID[m.ID] = m
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
	out := make([]manifestRec, 0, len(ms))
	done := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		if done[id] {
			return
		}
		done[id] = true
		if _, ok := byID[parent[id]]; ok {
			visit(parent[id])
		}
		out = append(out, byID[id])
	}
	for _, m := range ms {
		visit(m.ID)
	}
	return out
}
