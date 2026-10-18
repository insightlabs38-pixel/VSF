package rt

import (
	"errors"
	"testing"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
)

func obj(t *testing.T, text string) *Obj {
	t.Helper()
	v, err := canon.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewObj(v, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Req("a"); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestSinceGatesFieldsByDeclaredVersion(t *testing.T) {
	older, newer := Ctx{Version{1, 0, 0}}, Ctx{Version{1, 1, 0}}
	code := func(err error) string {
		var e *Error
		if errors.As(err, &e) {
			return e.Code
		}
		return ""
	}
	_, _, err := obj(t, `{"a":1,"b":2}`).Since("b", Version{1, 1, 0}, true, older)
	if code(err) != "ir.field_from_future" {
		t.Fatalf("got %v", err)
	}
	if _, found, err := obj(t, `{"a":1}`).Since("b", Version{1, 1, 0}, true, older); err != nil || found {
		t.Fatalf("absent before introduction: %v %v", found, err)
	}
	if _, found, err := obj(t, `{"a":1,"b":2}`).Since("b", Version{1, 1, 0}, true, newer); err != nil || !found {
		t.Fatalf("present after introduction: %v %v", found, err)
	}
	_, _, err = obj(t, `{"a":1}`).Since("b", Version{1, 1, 0}, true, newer)
	if code(err) != "ir.missing_field" {
		t.Fatalf("got %v", err)
	}
	if _, found, err := obj(t, `{"a":1}`).Since("b", Version{1, 1, 0}, false, newer); err != nil || found {
		t.Fatalf("optional absent: %v %v", found, err)
	}
}
