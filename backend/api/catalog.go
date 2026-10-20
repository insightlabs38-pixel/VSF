package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/insightlabs38-pixel/vsf/backend/execution"
	"github.com/insightlabs38-pixel/vsf/backend/ir"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

// IndexEntry is what a catalog knows about a contract without decoding it.
type IndexEntry struct {
	App         string
	OperationID string
	Version     string
	Digest      string
}

// ContractSource holds operation contract documents; documents are fetched only when an entry is needed.
type ContractSource interface {
	Index() ([]IndexEntry, error)
	Document(digest string) ([]byte, bool)
}

// Validator is the authority on whether a contract document is valid; it is the Rust validator in production.
type Validator interface {
	Valid(doc []byte) (bool, error)
}

// WorkerValidator asks the native worker, the single implementation of the invariants.
type WorkerValidator struct{ W *execution.Worker }

func (v WorkerValidator) Valid(doc []byte) (bool, error) {
	out, err := v.W.Raw('V', doc)
	if err != nil {
		return false, err
	}
	var rep struct {
		Valid  bool `json:"valid"`
		Decode struct {
			OK bool `json:"ok"`
		} `json:"decode"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		return false, err
	}
	return rep.Valid && rep.Decode.OK, nil
}

// MemorySource keeps documents in memory, indexed once at construction.
type MemorySource struct {
	docs  map[string][]byte
	index []IndexEntry
}

func NewMemorySource(docs ...[]byte) (*MemorySource, error) {
	m := &MemorySource{docs: map[string][]byte{}}
	for _, d := range docs {
		un, err := ir.DecodeDocument(d)
		if err != nil || un.Unsafe().Operation == nil {
			return nil, fmt.Errorf("not an operation contract document")
		}
		c := un.Unsafe().Operation
		e := IndexEntry{App: string(c.Source.AppID), OperationID: string(c.OperationID), Version: string(c.ContractVersion), Digest: un.Digest()}
		if _, dup := m.docs[e.Digest]; dup {
			continue
		}
		m.docs[e.Digest] = append([]byte(nil), d...)
		m.index = append(m.index, e)
	}
	return m, nil
}

func (m *MemorySource) Index() ([]IndexEntry, error) {
	return append([]IndexEntry(nil), m.index...), nil
}

func (m *MemorySource) Document(digest string) ([]byte, bool) {
	d, ok := m.docs[digest]
	return d, ok
}

// Pins restricts the served contract set: only the listed versions are discoverable, whatever else exists.
type Pins map[string]string

type entry struct {
	IndexEntry
	name string
	ver  rt.Version
}

type built struct {
	spec     *Spec
	withheld string
}

// Catalog is the lazily evaluated, deterministically ordered set of operations that may be emitted.
type Catalog struct {
	src     ContractSource
	val     Validator
	rev     execution.Revocations
	pins    Pins
	byName  map[string][]*entry
	names   []string
	byDig   map[string]*entry
	apps    map[string]int
	mu      sync.Mutex
	cache   map[string]built
	order   []string
	maxKeep int
	// Evaluations counts documents decoded and validated since construction; it makes laziness measurable.
	evaluations int
}

type CatalogConfig struct {
	Source      ContractSource
	Validator   Validator
	Revocations execution.Revocations
	Pins        Pins
	// CacheEntries bounds how many built specs stay in memory.
	CacheEntries int
}

func NewCatalog(cfg CatalogConfig) (*Catalog, error) {
	if cfg.Source == nil || cfg.Validator == nil {
		return nil, errors.New("a contract source and a validator are required")
	}
	idx, err := cfg.Source.Index()
	if err != nil {
		return nil, err
	}
	c := &Catalog{
		src: cfg.Source, val: cfg.Validator, rev: cfg.Revocations, pins: cfg.Pins,
		byName: map[string][]*entry{}, byDig: map[string]*entry{}, apps: map[string]int{},
		cache: map[string]built{}, maxKeep: cfg.CacheEntries,
	}
	if c.maxKeep <= 0 {
		c.maxKeep = 512
	}
	for _, ie := range idx {
		v, ok := rt.ParseVersion(ie.Version)
		if !ok {
			continue
		}
		e := &entry{IndexEntry: ie, name: OperationName(ie.App, ie.OperationID), ver: v}
		if len(e.name) > MaxNameChars {
			continue
		}
		c.byName[e.name] = append(c.byName[e.name], e)
		c.byDig[ie.Digest] = e
	}
	for n, es := range c.byName {
		sort.Slice(es, func(i, j int) bool { return less(es[j].ver, es[i].ver) })
		c.names = append(c.names, n)
		c.apps[es[0].App]++
	}
	sort.Strings(c.names)
	return c, nil
}

func less(a, b rt.Version) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Evaluations returns how many contract documents have been decoded and validated.
func (c *Catalog) Evaluations() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evaluations
}

// Size is the number of distinct operation names, known without decoding anything.
func (c *Catalog) Size() int { return len(c.names) }

func (c *Catalog) pinned(e *entry) bool {
	if c.pins == nil {
		return true
	}
	v, ok := c.pins[e.name]
	return ok && v == e.Version
}

func (c *Catalog) revoked(e *entry) (string, bool) {
	if c.rev == nil {
		return "", false
	}
	return c.rev.RevokedContract(e.Digest, e.OperationID, e.Version)
}

// build decodes, validates and describes one entry once, remembering why an entry was withheld.
func (c *Catalog) build(e *entry) built {
	c.mu.Lock()
	if b, ok := c.cache[e.Digest]; ok {
		c.mu.Unlock()
		return b
	}
	c.evaluations++
	c.mu.Unlock()
	res := c.evaluate(e)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.cache[e.Digest]; !ok {
		if len(c.order) >= c.maxKeep {
			delete(c.cache, c.order[0])
			c.order = c.order[1:]
		}
		c.cache[e.Digest] = res
		c.order = append(c.order, e.Digest)
	}
	return res
}

func (c *Catalog) evaluate(e *entry) built {
	doc, ok := c.src.Document(e.Digest)
	if !ok {
		return built{withheld: "document_missing"}
	}
	un, err := ir.DecodeDocument(doc)
	if err != nil || un.Unsafe().Operation == nil {
		return built{withheld: "undecodable"}
	}
	if un.Digest() != e.Digest {
		return built{withheld: "digest_mismatch"}
	}
	co := un.Unsafe().Operation
	if co.Lifecycle != ir.LifecycleActive {
		return built{withheld: "lifecycle_" + string(co.Lifecycle)}
	}
	ok, err = c.val.Valid(doc)
	if err != nil {
		return built{withheld: "validator_unavailable"}
	}
	if !ok {
		return built{withheld: "invalid_contract"}
	}
	s, err := specOf(*co, e.Digest)
	if err != nil {
		return built{withheld: err.Error()}
	}
	return built{spec: &s}
}

// Emitted reports whether an entry may be exposed, and why not if it may not.
func (c *Catalog) emitted(e *entry) (*Spec, string) {
	b := c.build(e)
	return b.spec, b.withheld
}

func (c *Catalog) latest(name string) *entry {
	for _, e := range c.byName[name] {
		if !c.pinned(e) {
			continue
		}
		if _, rev := c.revoked(e); rev {
			continue
		}
		return e
	}
	return nil
}

// Versions lists what is known about a name, newest first, with the reason for any entry not served.
type VersionInfo struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

func (c *Catalog) Versions(name string) []VersionInfo {
	var out []VersionInfo
	for _, e := range c.byName[name] {
		vi := VersionInfo{Version: e.Version, Digest: e.Digest, Status: "served"}
		if why, rev := c.revoked(e); rev {
			vi.Status, vi.Reason = "revoked", why
		} else if _, w := c.emitted(e); w != "" {
			vi.Status, vi.Reason = "withheld", w
		} else if !c.pinned(e) {
			vi.Status = "not_pinned"
		}
		out = append(out, vi)
	}
	return out
}

// Resolve finds the served entry for a name and an exact contract version ("" means the newest served version).
func (c *Catalog) Resolve(name, version string) (*Spec, error) {
	es, ok := c.byName[name]
	if !ok {
		return nil, newError(CodeOperationNotFound, "")
	}
	var e *entry
	if version == "" {
		e = c.latest(name)
	} else {
		for _, x := range es {
			if x.Version == version {
				e = x
			}
		}
	}
	if e == nil {
		return nil, c.unsupported(name, version)
	}
	if why, rev := c.revoked(e); rev {
		return nil, errWith(CodeContractRevoked, why, map[string]any{"contract_version": e.Version})
	}
	if !c.pinned(e) {
		return nil, c.unsupported(name, version)
	}
	s, why := c.emitted(e)
	if s == nil {
		if version == "" {
			return nil, newError(CodeOperationNotFound, "")
		}
		return nil, errWith(CodeOperationUnverified, why, nil)
	}
	return s, nil
}

func (c *Catalog) unsupported(name, version string) *Error {
	var served []any
	for _, v := range c.Versions(name) {
		if v.Status == "served" {
			served = append(served, v.Version)
		}
	}
	return errWith(CodeContractVersionUnsupported, "", map[string]any{"requested": version, "supported": served})
}

// ByDigest describes a stored contract whether or not it is still served, so an old invocation stays readable.
func (c *Catalog) ByDigest(digest string) (*Spec, bool) {
	e, ok := c.byDig[digest]
	if !ok {
		return nil, false
	}
	s, _ := c.emitted(e)
	return s, s != nil
}

// Contract returns a stored document by digest, for reconciliation.
func (c *Catalog) Contract(digest string) ([]byte, bool) { return c.src.Document(digest) }

// ListQuery selects a page of the catalog.
type ListQuery struct {
	Namespace string
	Cursor    string
	Limit     int
	// Scopes limits the page to operations whose required scopes the caller holds.
	Scopes []string
}

type Page struct {
	Items      []*Spec `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
	// Scanned says how many entries were looked at to fill the page; it never exceeds the scan bound.
	Scanned int `json:"scanned"`
}

const (
	DefaultPageSize = 50
	MaxPageSize     = 200
	cursorPrefix    = "c1."
)

func encodeCursor(name string) string {
	return cursorPrefix + base64.RawURLEncoding.EncodeToString([]byte(name))
}

func decodeCursor(cur string) (string, error) {
	rest, ok := strings.CutPrefix(cur, cursorPrefix)
	if !ok || len(cur) > 400 {
		return "", newError(CodeInvalidRequest, "bad_cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return "", newError(CodeInvalidRequest, "bad_cursor")
	}
	return string(b), nil
}

func holds(have []string, need []string) bool {
	for _, n := range need {
		found := false
		for _, h := range have {
			if h == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// List returns one deterministic page, evaluating only as many entries as the page needs.
func (c *Catalog) List(q ListQuery) (Page, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	start := 0
	if q.Cursor != "" {
		after, err := decodeCursor(q.Cursor)
		if err != nil {
			return Page{}, err
		}
		start = sort.SearchStrings(c.names, after)
		if start < len(c.names) && c.names[start] == after {
			start++
		}
	}
	prefix := ""
	if q.Namespace != "" {
		prefix = opPrefix + q.Namespace + "."
		if i := sort.SearchStrings(c.names, prefix); i > start {
			start = i
		}
	}
	scanBound := limit * 4
	var page Page
	last := ""
	for i := start; i < len(c.names); i++ {
		name := c.names[i]
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			break
		}
		if page.Scanned >= scanBound || len(page.Items) >= limit {
			page.NextCursor = encodeCursor(last)
			return page, nil
		}
		page.Scanned++
		last = name
		e := c.latest(name)
		if e == nil {
			continue
		}
		s, _ := c.emitted(e)
		if s == nil || !holds(q.Scopes, s.Effect.RequiredScopes) {
			continue
		}
		page.Items = append(page.Items, s)
	}
	return page, nil
}

// Namespace is one application's share of the catalog.
type Namespace struct {
	Name       string `json:"name"`
	App        string `json:"app"`
	Operations int    `json:"operations"`
}

// Namespaces is answered from the index alone; no contract is decoded.
func (c *Catalog) Namespaces() []Namespace {
	var out []Namespace
	for app, n := range c.apps {
		out = append(out, Namespace{Name: encodeApp(app), App: app, Operations: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ErrNotEmitted marks a lookup by digest that names something not served.
var ErrNotEmitted = errors.New("not emitted")
