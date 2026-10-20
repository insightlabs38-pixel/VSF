package execution

import (
	"fmt"
	"testing"
)

func TestDecodedDocumentsAreMemoizedByContentAndNeverConfused(t *testing.T) {
	a := contractDoc(t, nil)
	b := contractDoc(t, func(m map[string]any) { m["operation_id"] = "app.records.other" })
	ua, da, err := decodeDocument(a)
	if err != nil {
		t.Fatal(err)
	}
	ub, db, err := decodeDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	if da == db || ua.Unsafe().Operation.OperationID == ub.Unsafe().Operation.OperationID {
		t.Fatal("different documents must stay different")
	}
	ua2, da2, _ := decodeDocument(append([]byte(nil), a...))
	if da2 != da || ua2.Unsafe().Operation != ua.Unsafe().Operation {
		t.Fatal("the same bytes must hit the cache")
	}
	if _, _, err := decodeDocument([]byte(`{"nope":1}`)); err == nil {
		t.Fatal("an undecodable document must stay an error and must not be cached as valid")
	}
	if _, _, err := decodeDocument([]byte(`{"nope":1}`)); err == nil {
		t.Fatal("still an error")
	}
	for i := 0; i < docCacheEntries+20; i++ {
		d := contractDoc(t, func(m map[string]any) { m["operation_id"] = fmt.Sprintf("app.records.op_%04d", i) })
		if _, _, err := decodeDocument(d); err != nil {
			t.Fatal(err)
		}
	}
	docCache.mu.Lock()
	n := len(docCache.items)
	docCache.mu.Unlock()
	if n > docCacheEntries {
		t.Fatalf("the cache holds %d documents", n)
	}
}
