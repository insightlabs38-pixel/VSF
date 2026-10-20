package execution

import (
	"crypto/sha256"
	"sync"

	"github.com/insightlabs38-pixel/vsf/backend/ir"
)

const docCacheEntries = 256

type decoded struct {
	un     ir.Unvalidated
	digest string
}

// docCache remembers decoded contract documents by content; a document never changes, so a hit is exact.
var docCache = struct {
	mu    sync.Mutex
	items map[[32]byte]decoded
	order [][32]byte
}{items: map[[32]byte]decoded{}}

// decodeDocument decodes a contract document and its digest once per distinct content.
func decodeDocument(doc []byte) (ir.Unvalidated, string, error) {
	key := sha256.Sum256(doc)
	docCache.mu.Lock()
	d, ok := docCache.items[key]
	docCache.mu.Unlock()
	if ok {
		return d.un, d.digest, nil
	}
	un, err := ir.DecodeDocument(doc)
	if err != nil {
		return ir.Unvalidated{}, "", err
	}
	d = decoded{un: un, digest: un.Digest()}
	docCache.mu.Lock()
	defer docCache.mu.Unlock()
	if _, dup := docCache.items[key]; !dup {
		if len(docCache.order) >= docCacheEntries {
			delete(docCache.items, docCache.order[0])
			docCache.order = docCache.order[1:]
		}
		docCache.items[key] = d
		docCache.order = append(docCache.order, key)
	}
	return d.un, d.digest, nil
}
