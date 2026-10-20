// Package secret holds the keys that keep one tenant's private data unreadable and unguessable from another's, and seals data with them.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"

	"github.com/insightlabs38-pixel/vsf/backend/storage/meta"
)

var (
	ErrOpen    = errors.New("the sealed data cannot be opened")
	ErrKeyFile = errors.New("the key file is unusable")
)

const (
	keyLen    = 32
	tagLen    = 16
	nonceLen  = 12
	fileMode  = 0o600
	fileForm  = 1
	idInfo    = "vsf/id/v1|"
	dekInfo   = "vsf/dek/v1|"
	nonceInfo = "vsf/nonce/v1|"
)

// Keyring roots every derived key; the identity key never rotates since stored names derive from it, data keys are versioned.
type Keyring struct {
	mu     sync.RWMutex
	idKey  []byte
	data   map[byte][]byte
	active byte
}

func random() []byte {
	b := make([]byte, keyLen)
	if _, err := rand.Read(b); err != nil {
		panic("no entropy: " + err.Error())
	}
	return b
}

// Generate creates a keyring with fresh random keys.
func Generate() *Keyring {
	return &Keyring{idKey: random(), data: map[byte][]byte{1: random()}, active: 1}
}

// New builds a keyring from existing key material.
func New(idKey []byte, data map[byte][]byte, active byte) (*Keyring, error) {
	if len(idKey) != keyLen || data[active] == nil {
		return nil, fmt.Errorf("%w: missing key", ErrKeyFile)
	}
	cp := make(map[byte][]byte, len(data))
	for v, k := range data {
		if v == 0 || len(k) != keyLen {
			return nil, fmt.Errorf("%w: bad data key", ErrKeyFile)
		}
		cp[v] = append([]byte(nil), k...)
	}
	return &Keyring{idKey: append([]byte(nil), idKey...), data: cp, active: active}, nil
}

// Rotate adds a new data key and makes it the one new writes use; it returns the new version.
func (k *Keyring) Rotate() (byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.active == 255 {
		return 0, errors.New("no more key versions")
	}
	k.active++
	k.data[k.active] = random()
	return k.active, nil
}

func (k *Keyring) Active() byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.active
}

type fileJSON struct {
	Format int               `json:"format"`
	IDKey  string            `json:"id_key"`
	Data   map[string]string `json:"data_keys"`
	Active int               `json:"active"`
}

// Save writes the keyring atomically with owner-only permissions.
func (k *Keyring) Save(path string) error {
	k.mu.RLock()
	f := fileJSON{Format: fileForm, IDKey: hex.EncodeToString(k.idKey), Data: map[string]string{}, Active: int(k.active)}
	for v, key := range k.data {
		f.Data[strconv.Itoa(int(v))] = hex.EncodeToString(key)
	}
	k.mu.RUnlock()
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".keys-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Load reads a keyring and refuses a file other users can read.
func Load(path string) (*Keyring, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w: permissions %o allow other users", ErrKeyFile, st.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f fileJSON
	if err := json.Unmarshal(raw, &f); err != nil || f.Format != fileForm || f.Active < 1 || f.Active > 255 {
		return nil, fmt.Errorf("%w: format", ErrKeyFile)
	}
	idKey, err := hex.DecodeString(f.IDKey)
	if err != nil {
		return nil, fmt.Errorf("%w: id key", ErrKeyFile)
	}
	data := map[byte][]byte{}
	for s, h := range f.Data {
		v, err := strconv.Atoi(s)
		key, herr := hex.DecodeString(h)
		if err != nil || herr != nil || v < 1 || v > 255 {
			return nil, fmt.Errorf("%w: data key", ErrKeyFile)
		}
		data[byte(v)] = key
	}
	return New(idKey, data, byte(f.Active))
}

// Versions lists the data key versions held, oldest first.
func (k *Keyring) Versions() []byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]byte, 0, len(k.data))
	for v := range k.data {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func derive(root []byte, info string, t meta.Tenant) []byte {
	out, err := hkdf.Key(sha256.New, root, nil, info+string(t), keyLen)
	if err != nil {
		panic(err)
	}
	return out
}

func (k *Keyring) dataKey(v byte) []byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.data[v]
}

func mac(key, a, b []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(a)
	h.Write([]byte{0})
	h.Write(b)
	return h.Sum(nil)
}

func newGCM(key []byte) cipher.AEAD {
	blk, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		panic(err)
	}
	return g
}

// Sealer keys identifiers and encrypts artifacts: stable within a tenant, unrelated across tenants.
type Sealer struct{ k *Keyring }

func (k *Keyring) Sealer() *Sealer { return &Sealer{k} }

// Overhead is what sealing adds to the plaintext length.
func (*Sealer) Overhead() int { return 1 + tagLen }

// Identify is the tenant-keyed identifier of some bytes, shaped like a digest so it fits wherever one does.
func (s *Sealer) Identify(t meta.Tenant, data []byte) string {
	sum := mac(derive(s.k.idKey, idInfo, t), nil, data)
	return "sha256:" + hex.EncodeToString(sum)
}

func (s *Sealer) aad(t meta.Tenant, id string) []byte {
	return []byte("vsf/artifact/v1|" + string(t) + "|" + id)
}

// Seal encrypts data under the newest data key; the nonce follows from the identifier, so sealing is repeatable.
func (s *Sealer) Seal(t meta.Tenant, id string, data []byte) ([]byte, error) {
	v := s.k.Active()
	root := s.k.dataKey(v)
	if root == nil {
		return nil, ErrOpen
	}
	nonce := mac(derive(root, nonceInfo, t), nil, []byte(id))[:nonceLen]
	out := make([]byte, 1, 1+len(data)+tagLen)
	out[0] = v
	return newGCM(derive(root, dekInfo, t)).Seal(out, nonce, data, s.aad(t, id)), nil
}

// Open decrypts sealed bytes and proves they are the content the identifier names.
func (s *Sealer) Open(t meta.Tenant, id string, sealed []byte) ([]byte, error) {
	if len(sealed) < 1+tagLen {
		return nil, ErrOpen
	}
	root := s.k.dataKey(sealed[0])
	if root == nil {
		return nil, ErrOpen
	}
	nonce := mac(derive(root, nonceInfo, t), nil, []byte(id))[:nonceLen]
	plain, err := newGCM(derive(root, dekInfo, t)).Open(nil, nonce, sealed[1:], s.aad(t, id))
	if err != nil || !hmac.Equal([]byte(s.Identify(t, plain)), []byte(id)) {
		return nil, ErrOpen
	}
	return plain, nil
}

// Box seals values for one purpose of one tenant with a random nonce, for data that changes under a stable name.
type Box struct {
	k       *Keyring
	t       meta.Tenant
	purpose string
}

func (k *Keyring) Box(t meta.Tenant, purpose string) *Box { return &Box{k, t, purpose} }

// Name derives an opaque, tenant-and-purpose-specific name for a label, so a name reveals nothing and cannot be probed from another tenant.
func (b *Box) Name(label string) string {
	return hex.EncodeToString(mac(derive(b.k.idKey, idInfo+b.purpose+"|", b.t), nil, []byte(label)))
}

// Seal encrypts plaintext bound to aad.
func (b *Box) Seal(aad, plaintext []byte) ([]byte, error) {
	v := b.k.Active()
	root := b.k.dataKey(v)
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(make([]byte, 0, 1+nonceLen+len(plaintext)+tagLen), v)
	out = append(out, nonce...)
	return newGCM(derive(root, dekInfo+b.purpose+"|", b.t)).Seal(out, nonce, plaintext, aad), nil
}

// Open decrypts what Seal produced for the same tenant, purpose and aad.
func (b *Box) Open(aad, sealed []byte) ([]byte, error) {
	if len(sealed) < 1+nonceLen+tagLen {
		return nil, ErrOpen
	}
	root := b.k.dataKey(sealed[0])
	if root == nil {
		return nil, ErrOpen
	}
	plain, err := newGCM(derive(root, dekInfo+b.purpose+"|", b.t)).Open(nil, sealed[1:1+nonceLen], sealed[1+nonceLen:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}
