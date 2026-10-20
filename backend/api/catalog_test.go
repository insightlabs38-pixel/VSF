package api

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

type stubValidator struct {
	reject map[string]bool
	err    error
	calls  int
}

func (v *stubValidator) Valid(doc []byte) (bool, error) {
	v.calls++
	if v.err != nil {
		return false, v.err
	}
	for k := range v.reject {
		if strings.Contains(string(doc), k) {
			return false, nil
		}
	}
	return true, nil
}

func opDocs(t testing.TB, apps []string, ops int, scopes ...string) [][]byte {
	var docs [][]byte
	for _, app := range apps {
		for i := 0; i < ops; i++ {
			id := fmt.Sprintf("app.records.op_%04d", i)
			docs = append(docs, contractDoc(t, func(b map[string]any) {
				b["operation_id"] = id
				b["source"].(map[string]any)["app_id"] = app
				if len(scopes) > 0 {
					a := b["authorization"].(map[string]any)
					sc := []any{}
					for _, s := range scopes {
						sc = append(sc, s)
					}
					a["required_scopes"] = sc
				}
			}))
		}
	}
	return docs
}

func newCat(t testing.TB, docs [][]byte, v Validator, mut func(*CatalogConfig)) *Catalog {
	src, err := NewMemorySource(docs...)
	if err != nil {
		t.Fatal(err)
	}
	cfg := CatalogConfig{Source: src, Validator: v}
	if mut != nil {
		mut(&cfg)
	}
	c, err := NewCatalog(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func names(p Page) []string {
	var out []string
	for _, s := range p.Items {
		out = append(out, s.Name)
	}
	return out
}

func walk(t testing.TB, c *Catalog, q ListQuery) ([]string, int) {
	var all []string
	pages := 0
	for {
		p, err := c.List(q)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		all = append(all, names(p)...)
		if p.NextCursor == "" {
			return all, pages
		}
		q.Cursor = p.NextCursor
		if pages > 10000 {
			t.Fatal("pagination does not end")
		}
	}
}

func TestListingIsDeterministicWhateverTheInsertionOrder(t *testing.T) {
	docs := opDocs(t, []string{"vendor-console:app.a", "vendor-console:app.b"}, 30)
	base, _ := walk(t, newCat(t, docs, &stubValidator{}, nil), ListQuery{Scopes: []string{"records.write"}, Limit: 7})
	for seed := int64(1); seed <= 5; seed++ {
		sh := append([][]byte(nil), docs...)
		rand.New(rand.NewSource(seed)).Shuffle(len(sh), func(i, j int) { sh[i], sh[j] = sh[j], sh[i] })
		got, _ := walk(t, newCat(t, sh, &stubValidator{}, nil), ListQuery{Scopes: []string{"records.write"}, Limit: 7})
		if !reflect.DeepEqual(base, got) {
			t.Fatalf("seed %d changed the order", seed)
		}
	}
	if len(base) != 60 {
		t.Fatalf("%d", len(base))
	}
	for i := 1; i < len(base); i++ {
		if base[i-1] >= base[i] {
			t.Fatalf("not sorted at %d", i)
		}
	}
}

func TestPageSizeDoesNotChangeWhatIsListed(t *testing.T) {
	c := newCat(t, opDocs(t, []string{"vendor-console:app.a"}, 45), &stubValidator{}, nil)
	var ref []string
	for _, lim := range []int{1, 2, 7, 45, 500} {
		got, _ := walk(t, c, ListQuery{Scopes: []string{"records.write"}, Limit: lim})
		if ref == nil {
			ref = got
		} else if !reflect.DeepEqual(ref, got) {
			t.Fatalf("limit %d changed the listing", lim)
		}
	}
	if len(ref) != 45 {
		t.Fatal(len(ref))
	}
}

func TestNamespaceListingTouchesOnlyThatNamespace(t *testing.T) {
	apps := []string{"vendor-console:app.a", "vendor-console:app.b", "vendor-console:app.c"}
	v := &stubValidator{}
	c := newCat(t, opDocs(t, apps, 20), v, nil)
	ns := OperationName(apps[1], "x")
	prefix, _, _ := strings.Cut(strings.TrimPrefix(ns, opPrefix), ".")
	got, _ := walk(t, c, ListQuery{Namespace: prefix, Scopes: []string{"records.write"}, Limit: 5})
	if len(got) != 20 {
		t.Fatalf("%d", len(got))
	}
	for _, n := range got {
		if app, _, _ := SplitName(n); app != apps[1] {
			t.Fatalf("%s is not in the namespace", n)
		}
	}
	if c.Evaluations() != 20 {
		t.Fatalf("a namespace listing decoded %d contracts, want only its own 20", c.Evaluations())
	}
	if len(c.Namespaces()) != 3 {
		t.Fatal("namespaces come from the index")
	}
	miss, _ := walk(t, c, ListQuery{Namespace: "no-such-app", Scopes: []string{"records.write"}})
	if len(miss) != 0 {
		t.Fatal("an unknown namespace lists nothing")
	}
}

func TestOnePageEvaluatesAtMostItsScanBoundNotTheCatalog(t *testing.T) {
	v := &stubValidator{}
	c := newCat(t, opDocs(t, []string{"vendor-console:app.a"}, 500), v, nil)
	if c.Evaluations() != 0 || v.calls != 0 || c.Size() != 500 {
		t.Fatalf("construction must not decode anything: %d %d %d", c.Evaluations(), v.calls, c.Size())
	}
	_ = c.Namespaces()
	p, _ := c.List(ListQuery{Scopes: []string{"records.write"}, Limit: 10})
	if len(p.Items) != 10 || p.NextCursor == "" || p.Scanned != 10 {
		t.Fatalf("%d items, scanned %d", len(p.Items), p.Scanned)
	}
	if c.Evaluations() != 10 || v.calls != 10 {
		t.Fatalf("a page of 10 evaluated %d contracts", c.Evaluations())
	}
	_, _ = c.List(ListQuery{Scopes: []string{"records.write"}, Limit: 10})
	_, _ = c.List(ListQuery{Scopes: []string{"records.write"}, Limit: 10})
	if c.Evaluations() != 10 {
		t.Fatal("a repeated first page must be served from memory")
	}
}

func TestAnAdversarialCatalogCannotMakeOnePageScanEverything(t *testing.T) {
	v := &stubValidator{reject: map[string]bool{"op_0": true}}
	docs := opDocs(t, []string{"vendor-console:app.a"}, 400)
	c := newCat(t, docs, v, nil)
	p, err := c.List(ListQuery{Scopes: []string{"records.write"}, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if p.Scanned > 5*4 || c.Evaluations() > 5*4 {
		t.Fatalf("scanned %d, evaluated %d", p.Scanned, c.Evaluations())
	}
	if len(p.Items) != 0 || p.NextCursor == "" {
		t.Fatalf("an all-withheld stretch must return an empty page with a cursor to continue: %d %q", len(p.Items), p.NextCursor)
	}
	all, pages := walk(t, c, ListQuery{Scopes: []string{"records.write"}, Limit: 5})
	if len(all) != 0 || pages < 400/20 {
		t.Fatalf("%d items over %d pages", len(all), pages)
	}
	lacking := newCat(t, opDocs(t, []string{"vendor-console:app.a"}, 400, "records.admin"), &stubValidator{}, nil)
	p, _ = lacking.List(ListQuery{Scopes: []string{"records.read"}, Limit: 5})
	if p.Scanned > 20 || len(p.Items) != 0 {
		t.Fatalf("scope filtering scanned %d", p.Scanned)
	}
}

func TestOperationsTheCallerLacksScopesForAreNotListed(t *testing.T) {
	c := newCat(t, opDocs(t, []string{"vendor-console:app.a"}, 3, "records.admin"), &stubValidator{}, nil)
	if got, _ := walk(t, c, ListQuery{Scopes: []string{"records.write"}}); len(got) != 0 {
		t.Fatalf("%v", got)
	}
	if got, _ := walk(t, c, ListQuery{Scopes: []string{"records.write", "records.admin"}}); len(got) != 3 {
		t.Fatalf("%v", got)
	}
}

func TestCursorsAreOpaqueBoundedAndSurviveCatalogChurn(t *testing.T) {
	c := newCat(t, opDocs(t, []string{"vendor-console:app.a"}, 10), &stubValidator{}, nil)
	for _, bad := range []string{"garbage", "c1.", "c1.***", "c2." + encodeCursor("x")[3:], strings.Repeat("c1.AAAA", 200)} {
		if _, err := c.List(ListQuery{Cursor: bad}); err == nil || AsError(err).Code != CodeInvalidRequest {
			t.Fatalf("cursor %q accepted", bad[:min(len(bad), 8)])
		}
	}
	p, _ := c.List(ListQuery{Scopes: []string{"records.write"}, Limit: 3})
	smaller := newCat(t, opDocs(t, []string{"vendor-console:app.a"}, 4), &stubValidator{}, nil)
	rest, err := smaller.List(ListQuery{Scopes: []string{"records.write"}, Cursor: p.NextCursor, Limit: 50})
	if err != nil || len(rest.Items) != 1 {
		t.Fatalf("a cursor from a larger catalog must still resume: %v %d", err, len(rest.Items))
	}
	if after, _ := decodeCursor(encodeCursor("op.zzz")); after != "op.zzz" {
		t.Fatal("cursor round trip")
	}
	end, _ := c.List(ListQuery{Scopes: []string{"records.write"}, Cursor: encodeCursor("op.zzzz"), Limit: 5})
	if len(end.Items) != 0 || end.NextCursor != "" {
		t.Fatal("a cursor past the end lists nothing")
	}
}

func TestOnlyWhatMayBeEmittedIsServed(t *testing.T) {
	docs := [][]byte{
		contractDoc(t, func(b map[string]any) { b["operation_id"] = "app.records.good" }),
		contractDoc(t, func(b map[string]any) { b["operation_id"] = "app.records.bad_effect" }),
		contractDoc(t, func(b map[string]any) { b["operation_id"] = "app.records.deprecated"; b["lifecycle"] = "deprecated" }),
		contractDoc(t, func(b map[string]any) { b["operation_id"] = "app.records.proposed"; b["lifecycle"] = "proposed" }),
	}
	v := &stubValidator{reject: map[string]bool{"bad_effect": true}}
	c := newCat(t, docs, v, nil)
	got, _ := walk(t, c, ListQuery{Scopes: []string{"records.write"}})
	if len(got) != 1 || !strings.HasSuffix(got[0], ".good") {
		t.Fatalf("%v", got)
	}
	why := map[string]string{}
	for _, n := range []string{"bad_effect", "deprecated", "proposed"} {
		name := OperationName(testApp, "app.records."+n)
		vs := c.Versions(name)
		if len(vs) != 1 || vs[0].Status != "withheld" {
			t.Fatalf("%s: %+v", n, vs)
		}
		why[n] = vs[0].Reason
		if _, err := c.Resolve(name, ""); err == nil {
			t.Fatalf("%s resolved", n)
		}
		if _, err := c.Resolve(name, "1.3.0"); AsError(err).Code != CodeOperationUnverified {
			t.Fatalf("%s: %v", n, err)
		}
	}
	if why["bad_effect"] != "invalid_contract" || why["deprecated"] != "lifecycle_deprecated" || why["proposed"] != "lifecycle_proposed" {
		t.Fatalf("%v", why)
	}
	down := newCat(t, docs[:1], &stubValidator{err: errors.New("worker down")}, nil)
	if got, _ := walk(t, down, ListQuery{Scopes: []string{"records.write"}}); len(got) != 0 {
		t.Fatal("an unreachable validator must fail closed")
	}
}

func TestPinsRevocationsAndVersionsDecideWhatRuns(t *testing.T) {
	old := contractDoc(t, func(b map[string]any) { b["contract_version"] = "1.2.0" })
	cur := contractDoc(t, nil)
	rev := &revs{set: map[string]string{}}
	c := newCat(t, [][]byte{old, cur}, &stubValidator{}, func(cfg *CatalogConfig) { cfg.Revocations = rev })
	name := OperationName(testApp, testOperation)
	s, err := c.Resolve(name, "")
	if err != nil || s.ContractVersion != "1.3.0" {
		t.Fatalf("%v %+v", err, s)
	}
	if s, err = c.Resolve(name, "1.2.0"); err != nil || s.ContractVersion != "1.2.0" {
		t.Fatalf("an older served version must stay addressable by exact pin: %v", err)
	}
	if _, err = c.Resolve(name, "9.9.9"); AsError(err).Code != CodeContractVersionUnsupported {
		t.Fatal(err)
	}
	if _, err = c.Resolve("op.nope.x", ""); AsError(err).Code != CodeOperationNotFound {
		t.Fatal(err)
	}
	rev.set[specOfDoc(t, cur).ContractDigest] = "security fix"
	if s, err = c.Resolve(name, ""); err != nil || s.ContractVersion != "1.2.0" {
		t.Fatalf("a revoked newest version must fall back to the next served one: %v", err)
	}
	if _, err = c.Resolve(name, "1.3.0"); AsError(err).Code != CodeContractRevoked {
		t.Fatalf("a pin must never revive a revoked version: %v", err)
	}
	vs := c.Versions(name)
	if vs[0].Status != "revoked" || vs[1].Status != "served" {
		t.Fatalf("%+v", vs)
	}
	pinned := newCat(t, [][]byte{old, cur}, &stubValidator{}, func(cfg *CatalogConfig) { cfg.Pins = Pins{name: "1.2.0"} })
	if s, _ := pinned.Resolve(name, ""); s.ContractVersion != "1.2.0" {
		t.Fatal("a pin set restricts what is served")
	}
	if _, err := pinned.Resolve(name, "1.3.0"); AsError(err).Code != CodeContractVersionUnsupported {
		t.Fatal(err)
	}
}

type revs struct{ set map[string]string }

func (r *revs) RevokedContract(d, _, _ string) (string, bool) { w, ok := r.set[d]; return w, ok }

func TestTheSpecCacheIsBounded(t *testing.T) {
	c := newCat(t, opDocs(t, []string{"vendor-console:app.a"}, 60), &stubValidator{}, func(cfg *CatalogConfig) { cfg.CacheEntries = 8 })
	walk(t, c, ListQuery{Scopes: []string{"records.write"}, Limit: 5})
	c.mu.Lock()
	n := len(c.cache)
	c.mu.Unlock()
	if n > 8 {
		t.Fatalf("cache holds %d specs", n)
	}
}

func TestListQueryIsClampedToTheMaximumPage(t *testing.T) {
	c := newCat(t, opDocs(t, []string{"vendor-console:app.a"}, 250), &stubValidator{}, nil)
	p, _ := c.List(ListQuery{Scopes: []string{"records.write"}, Limit: 100000})
	if len(p.Items) != MaxPageSize {
		t.Fatalf("%d", len(p.Items))
	}
	if p, _ = c.List(ListQuery{Scopes: []string{"records.write"}, Limit: -3}); len(p.Items) != DefaultPageSize {
		t.Fatalf("%d", len(p.Items))
	}
}

func hugeSource(b *testing.B) *MemorySource {
	docs := opDocs(b, []string{"vendor-console:app.a", "vendor-console:app.b"}, 5000)
	src, err := NewMemorySource(docs...)
	if err != nil {
		b.Fatal(err)
	}
	return src
}

// BenchmarkCatalogIndex is the cost of making 10000 contracts discoverable: indexing only, nothing decoded.
func BenchmarkCatalogIndex(b *testing.B) {
	src := hugeSource(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewCatalog(CatalogConfig{Source: src, Validator: &stubValidator{}}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFirstPageOfAHugeCatalog is the cost of one cold page of 50 from 10000 contracts.
func BenchmarkFirstPageOfAHugeCatalog(b *testing.B) {
	src := hugeSource(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		c, _ := NewCatalog(CatalogConfig{Source: src, Validator: &stubValidator{}})
		b.StartTimer()
		p, _ := c.List(ListQuery{Scopes: []string{"records.write"}, Limit: DefaultPageSize})
		if len(p.Items) != DefaultPageSize {
			b.Fatal(len(p.Items))
		}
	}
}
