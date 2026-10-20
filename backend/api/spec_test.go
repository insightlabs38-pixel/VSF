package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func envelope(mut func(m map[string]any)) []byte {
	m := map[string]any{
		"request_id": "req-1", "session_id": "sess-1", "record": "o1",
		"params": map[string]any{"record_id": "o1", "stage": "won"},
		"expect": map[string]any{"stage": "won"}, "seen": map[string]any{"version": "v1", "observed_at": 1},
	}
	if mut != nil {
		mut(m)
	}
	b, _ := json.Marshal(m)
	return b
}

func TestSpecDescribesTheContractAndDisclosesItsEffect(t *testing.T) {
	s := specOfDoc(t, contractDoc(t, nil))
	if s.Name != OperationName(testApp, testOperation) {
		t.Fatal(s.Name)
	}
	e := s.Effect
	if !e.Write || e.EffectClass != "conditional_write" || !e.VerifyRequired || e.OnAmbiguity != "report_unknown_no_auto_retry" ||
		!e.RequiresCaller || !e.RequiresSession || len(e.RequiredScopes) != 1 || e.Freshness != "source_read_required" {
		t.Fatalf("disclosure %+v", e)
	}
	if s.Annotations.ReadOnly || s.Annotations.Destructive || !s.Annotations.OpenWorld {
		t.Fatalf("annotations %+v", s.Annotations)
	}
	if !strings.Contains(s.Description, "unknown_commit") || !strings.Contains(s.Description, "read-back") {
		t.Fatal("a write's description must state the unknown_commit rule")
	}
	req := s.InputSchema["required"].([]any)
	if req[0] != "request_id" {
		t.Fatalf("a write must require a request id: %v", req)
	}
	b1, _ := json.Marshal(s)
	b2, _ := json.Marshal(specOfDoc(t, contractDoc(t, nil)))
	if string(b1) != string(b2) {
		t.Fatal("a spec must be deterministic")
	}
}

func TestEnvelopeIsCheckedAgainstTheOperationsOwnSchema(t *testing.T) {
	s := specOfDoc(t, contractDoc(t, nil))
	if _, err := DecodeInvocation(s, envelope(nil)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(m map[string]any){
		"missing request id":   func(m map[string]any) { delete(m, "request_id") },
		"missing session":      func(m map[string]any) { delete(m, "session_id") },
		"unknown envelope key": func(m map[string]any) { m["admin"] = true },
		"unknown param":        func(m map[string]any) { m["params"].(map[string]any)["extra"] = "x" },
		"missing param":        func(m map[string]any) { delete(m["params"].(map[string]any), "stage") },
		"wrong param type":     func(m map[string]any) { m["params"].(map[string]any)["stage"] = 7 },
		"choice outside set":   func(m map[string]any) { m["params"].(map[string]any)["stage"] = "lost" },
		"oversize text": func(m map[string]any) {
			m["params"].(map[string]any)["record_id"] = strings.Repeat("a", MaxTextChars+1)
		},
		"request id has space": func(m map[string]any) { m["request_id"] = "a b" },
		"wait too long":        func(m map[string]any) { m["wait_ms"] = MaxWaitMs + 1 },
		"negative wait":        func(m map[string]any) { m["wait_ms"] = -1 },
		"strategy not offered": func(m map[string]any) { m["strategy"] = "magic" },
		"seen incomplete":      func(m map[string]any) { m["seen"] = map[string]any{"version": "v1"} },
		"expect non string":    func(m map[string]any) { m["expect"] = map[string]any{"stage": 1} },
		"params is a list":     func(m map[string]any) { m["params"] = []any{} },
	}
	for name, mut := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeInvocation(s, envelope(mut))
			if err == nil || err.Code != CodeInvalidArgument {
				t.Fatalf("got %v", err)
			}
			if len(err.Details["violations"].([]any)) == 0 {
				t.Fatal("violations must name the failing paths")
			}
		})
	}
}

func TestViolationsNeverEchoCallerValues(t *testing.T) {
	s := specOfDoc(t, contractDoc(t, nil))
	_, err := DecodeInvocation(s, envelope(func(m map[string]any) { m["params"].(map[string]any)["stage"] = "TOPSECRET-VALUE" }))
	b, _ := json.Marshal(err)
	if strings.Contains(string(b), "TOPSECRET") {
		t.Fatalf("a rejected value was reflected: %s", b)
	}
}

func TestStrictJSONRefusesWhatSchemasCannotSee(t *testing.T) {
	s := specOfDoc(t, contractDoc(t, nil))
	for name, raw := range map[string]string{
		"duplicate key":    `{"request_id":"a","request_id":"b","session_id":"s","params":{}}`,
		"duplicate nested": `{"request_id":"a","session_id":"s","params":{"stage":"won","stage":"lost"}}`,
		"trailing data":    string(envelope(nil)) + `{}`,
		"not json":         `request_id=1`,
		"array":            `[]`,
		"deep":             strings.Repeat(`{"a":`, 40) + `1` + strings.Repeat(`}`, 40),
		"empty":            ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeInvocation(s, []byte(raw)); err == nil || err.Code != CodeInvalidRequest {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := DecodeInvocation(s, make([]byte, MaxRequestBytes+1)); err == nil || err.Code != CodeTooLarge {
		t.Fatalf("got %v", err)
	}
}

func TestLargeIntegersSurviveDecoding(t *testing.T) {
	s := specOfDoc(t, contractDoc(t, nil))
	inv, err := DecodeInvocation(s, []byte(strings.Replace(string(envelope(nil)), `"observed_at":1`, `"observed_at":9007199254740993`, 1)))
	if err != nil || inv.Seen.ObservedAt != 9007199254740993 {
		t.Fatalf("%v %+v", err, inv.Seen)
	}
}

func TestNamesRoundTripAndCannotCollide(t *testing.T) {
	apps := []string{"vendor-console:app.vendor.test", "a_b", "a.b", "a:b", "a__b", "a_db", "ünï", "a b", "x_cy"}
	seen := map[string]string{}
	for _, a := range apps {
		n := OperationName(a, "app.records.update_stage")
		if prev, dup := seen[n]; dup {
			t.Fatalf("%q and %q collide as %s", a, prev, n)
		}
		seen[n] = a
		got, op, ok := SplitName(n)
		if !ok || got != a || op != "app.records.update_stage" {
			t.Fatalf("%q -> %s -> %q %q %v", a, n, got, op, ok)
		}
		if strings.ContainsAny(n, " /:") {
			t.Fatalf("%s is not safe as a tool name", n)
		}
	}
	for _, bad := range []string{"", "op.", "op.x", "x.y.z", "op._z.a", "op.a_.b", "op.a_x1.b"} {
		if _, _, ok := SplitName(bad); ok {
			t.Fatalf("%q must not split", bad)
		}
	}
}

func jsonTags(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if tag != "" && tag != "-" {
			out = append(out, tag)
		}
	}
	return out
}

// The published result schema must name every field Result can carry; additionalProperties is false.
func TestResultSchemaDeclaresEveryResultField(t *testing.T) {
	props := ResultSchemaFor("op.x", "1.0.0", "d", nil)["properties"].(map[string]any)
	for _, tag := range jsonTags(reflect.TypeOf(Result{})) {
		if _, ok := props[tag]; !ok {
			t.Errorf("result field %q is missing from the schema", tag)
		}
	}
	for tag, sub := range map[string]reflect.Type{"observed": reflect.TypeOf(Observed{}), "reconciliation": reflect.TypeOf(Reconciliation{}), "error": reflect.TypeOf(Error{})} {
		inner := props[tag].(map[string]any)["properties"].(map[string]any)
		for _, f := range jsonTags(sub) {
			if _, ok := inner[f]; !ok {
				t.Errorf("%s field %q is missing from the schema", tag, f)
			}
		}
	}
	item := props["progress"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	for _, f := range jsonTags(reflect.TypeOf(Progress{})) {
		if _, ok := item[f]; !ok {
			t.Errorf("progress field %q is missing from the schema", f)
		}
	}
}
