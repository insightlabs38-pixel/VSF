package execution_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/execution"
	"github.com/insightlabs38-pixel/vsf/backend/ir"
	"github.com/insightlabs38-pixel/vsf/backend/ir/rt"
)

func fixtureDoc(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "ir", "fixtures", "valid", "operation-witnessed-1-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := ir.DigestAt("operation", rt.Version{1, 1, 0})
	var doc any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "$DIGEST", d)), &doc); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(doc)
	v, perr := canon.Parse(out)
	if perr != nil {
		t.Fatal(perr)
	}
	return canon.ToCanonical(v)
}

type forger struct{}

func (forger) Authorize([]byte, execution.Env) (execution.Authorized, error) {
	return execution.Authorized{}, nil
}

type people struct{}

func (people) Lookup(id string) (execution.Caller, bool) {
	return execution.Caller{ID: id, Scopes: []string{"records.write"}}, true
}

type sessions struct{}

func (sessions) Lookup(id string) (execution.Session, bool) {
	return execution.Session{ID: id, Owner: "ada", Authenticated: true, AppID: "vendor-console:app.vendor.test", VariantClass: "sales-rep", AppVersion: "2.0.0", SourceScopes: []string{"records.write"}}, true
}

type counting struct{ calls int }

func (c *counting) Kind() string                         { return "provider_api_authorized" }
func (c *counting) Capabilities() execution.Capabilities { return execution.Capabilities{} }
func (c *counting) Read(context.Context, execution.Target) (execution.Snapshot, error) {
	c.calls++
	return execution.Snapshot{Version: "v1"}, nil
}
func (c *counting) Apply(context.Context, execution.Call) (execution.Result, error) {
	c.calls++
	return execution.Result{Outcome: execution.OutcomeCommitted}, nil
}

func TestCodeOutsideThePackageCannotForgeAuthority(t *testing.T) {
	typ := reflect.TypeOf(execution.Authorized{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			t.Errorf("Authorized exposes %s", typ.Field(i).Name)
		}
	}
	doc := fixtureDoc(t)
	un, err := ir.DecodeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	ex := &counting{}
	rtm, err := execution.NewRuntime(execution.Config{
		Journal: mustJournal(t), Authorizer: forger{}, Callers: people{}, Sessions: sessions{},
		Executors: map[string]execution.Executor{"provider_api_authorized": ex},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := rtm.Submit(context.Background(), execution.Request{
		RequestID: "r1", Caller: "ada", SessionID: "s1", Contract: doc,
		Pin: execution.Pin{Version: "1.3.0", Digest: un.Digest()}, Params: map[string]any{"record_id": "o1", "stage": "won"},
		Record: "o1", Expect: map[string]string{"stage": "won"}, Seen: &execution.Seen{Version: "v1"},
	})
	var ref *execution.Refusal
	if err == nil || !asRefusal(err, &ref) || ref.Reason != "authorization_invalid" {
		t.Fatalf("err %v", err)
	}
	if h.State() != execution.Failed || ex.calls != 0 {
		t.Fatalf("state %s, source calls %d", h.State(), ex.calls)
	}
}

func TestDecodingOnlyEverYieldsUnvalidatedDocuments(t *testing.T) {
	un, err := ir.DecodeDocument(fixtureDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	var _ ir.Unvalidated = un
	if un.Unsafe().Operation == nil || un.Digest() == "" {
		t.Fatal("decoded document lost its body or digest")
	}
	typ := reflect.TypeOf(ir.Unvalidated{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).IsExported() {
			t.Errorf("Unvalidated exposes %s", typ.Field(i).Name)
		}
	}
}

func mustJournal(t *testing.T) *execution.Journal {
	t.Helper()
	j, err := execution.OpenJournal(&execution.MemStore{})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func asRefusal(err error, out **execution.Refusal) bool {
	r, ok := err.(*execution.Refusal)
	*out = r
	return ok
}
