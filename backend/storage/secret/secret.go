// Package secret holds the keys that keep one tenant's private data unreadable and unguessable from another's, and seals data with them.
package secret

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	keyLen         = 32
	tagLen         = 16
	nonceLen       = 12
	fileMode       = 0o600
	fileForm       = 1
	tenantIDInfo   = "vsf/tenant-id/v1|"
	tenantDataInfo = "vsf/tenant-data/v1|"
	idInfo         = "vsf/id/v2"
	dekInfo        = "vsf/dek/v2"
	nonceInfo      = "vsf/nonce/v2"
	nameInfo       = "vsf/name/v2|"
)

// Keyring roots every derived key; the identity key never rotates since stored names derive from it, data keys are versioned.
type Keyring struct {
	mu     sync.RWMutex
	idKey  []byte
	data   map[byte][]byte
	active byte
	// tenants holds material that does not follow from the roots: a tenant adopted from another installation, or rotated on its own.
	tenants map[meta.Tenant]*TenantKeys
	guards  []RetireGuard
}

// ErrInUse means a key version cannot be retired because sealed data still depends on it, or because nothing vouches that none does.
var ErrInUse = errors.New("sealed data still depends on this key version")

// RetireGuard counts the sealed objects a holder still has under a key version; an error makes retirement fail closed.
type RetireGuard func(ctx context.Context, version byte) (int, error)

// GuardRetire registers a holder of sealed data (an artifact store, a vault) whose objects must be gone from a version before it is retired.
func (k *Keyring) GuardRetire(g RetireGuard) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.guards = append(k.guards, g)
}

// TenantKeys is everything needed to open and write one tenant's sealed data, and nothing about any other tenant.
type TenantKeys struct {
	ID     []byte
	Data   map[byte][]byte
	Active byte
}

func (t TenantKeys) clone() *TenantKeys {
	c := &TenantKeys{ID: append([]byte(nil), t.ID...), Data: make(map[byte][]byte, len(t.Data)), Active: t.Active}
	for v, k := range t.Data {
		c.Data[v] = append([]byte(nil), k...)
	}
	return c
}

func (t TenantKeys) valid() bool {
	if len(t.ID) != keyLen || t.Data[t.Active] == nil {
		return false
	}
	for v, k := range t.Data {
		if v == 0 || len(k) != keyLen {
			return false
		}
	}
	return true
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
	return &Keyring{idKey: random(), data: map[byte][]byte{1: random()}, active: 1, tenants: map[meta.Tenant]*TenantKeys{}}
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
	return &Keyring{idKey: append([]byte(nil), idKey...), data: cp, active: active, tenants: map[meta.Tenant]*TenantKeys{}}, nil
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
	Format  int                   `json:"format"`
	IDKey   string                `json:"id_key"`
	Data    map[string]string     `json:"data_keys"`
	Active  int                   `json:"active"`
	Tenants map[string]tenantJSON `json:"tenants,omitempty"`
}

type tenantJSON struct {
	ID     string            `json:"id_key"`
	Data   map[string]string `json:"data_keys"`
	Active int               `json:"active"`
}

func (t TenantKeys) toJSON() tenantJSON {
	j := tenantJSON{ID: hex.EncodeToString(t.ID), Data: map[string]string{}, Active: int(t.Active)}
	for v, k := range t.Data {
		j.Data[strconv.Itoa(int(v))] = hex.EncodeToString(k)
	}
	return j
}

func (j tenantJSON) keys() (TenantKeys, error) {
	id, err := hex.DecodeString(j.ID)
	if err != nil || j.Active < 1 || j.Active > 255 {
		return TenantKeys{}, ErrKeyFile
	}
	t := TenantKeys{ID: id, Data: map[byte][]byte{}, Active: byte(j.Active)}
	for s, h := range j.Data {
		v, err := strconv.Atoi(s)
		k, herr := hex.DecodeString(h)
		if err != nil || herr != nil || v < 1 || v > 255 {
			return TenantKeys{}, ErrKeyFile
		}
		t.Data[byte(v)] = k
	}
	if !t.valid() {
		return TenantKeys{}, ErrKeyFile
	}
	return t, nil
}

// Save writes the keyring atomically with owner-only permissions.
func (k *Keyring) Save(path string) error {
	k.mu.RLock()
	f := fileJSON{Format: fileForm, IDKey: hex.EncodeToString(k.idKey), Data: map[string]string{}, Active: int(k.active)}
	for v, key := range k.data {
		f.Data[strconv.Itoa(int(v))] = hex.EncodeToString(key)
	}
	if len(k.tenants) > 0 {
		f.Tenants = map[string]tenantJSON{}
		for t, m := range k.tenants {
			f.Tenants[string(t)] = m.toJSON()
		}
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
	k, err := New(idKey, data, byte(f.Active))
	if err != nil {
		return nil, err
	}
	for t, j := range f.Tenants {
		tk, err := j.keys()
		if err != nil || !meta.ValidTenant(meta.Tenant(t)) {
			return nil, fmt.Errorf("%w: tenant keys", ErrKeyFile)
		}
		k.tenants[meta.Tenant(t)] = tk.clone()
	}
	return k, nil
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

func kdf(root []byte, info string) []byte {
	out, err := hkdf.Key(sha256.New, root, nil, info, keyLen)
	if err != nil {
		panic(err)
	}
	return out
}

// idRoot is the tenant's root for identifiers and names; it never rotates.
func (k *Keyring) idRoot(t meta.Tenant) []byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if m, ok := k.tenants[t]; ok {
		return m.ID
	}
	return kdf(k.idKey, tenantIDInfo+string(t))
}

// dataRoot is the tenant's root for one data key version, or nil when that version is unknown.
func (k *Keyring) dataRoot(t meta.Tenant, v byte) []byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if m, ok := k.tenants[t]; ok {
		return m.Data[v]
	}
	root, ok := k.data[v]
	if !ok {
		return nil
	}
	return kdf(root, tenantDataInfo+string(t))
}

// ActiveFor is the data key version new writes for a tenant use.
func (k *Keyring) ActiveFor(t meta.Tenant) byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if m, ok := k.tenants[t]; ok {
		return m.Active
	}
	return k.active
}

// materialize copies what the roots imply for a tenant into an override, so the tenant can then change on its own.
func (k *Keyring) materialize(t meta.Tenant) *TenantKeys {
	if m, ok := k.tenants[t]; ok {
		return m
	}
	m := &TenantKeys{ID: kdf(k.idKey, tenantIDInfo+string(t)), Data: map[byte][]byte{}, Active: k.active}
	for v, root := range k.data {
		m.Data[v] = kdf(root, tenantDataInfo+string(t))
	}
	k.tenants[t] = m
	return m
}

// RotateTenant adds a data key for one tenant alone and makes it that tenant's active one; the tenant's existing data still opens.
func (k *Keyring) RotateTenant(t meta.Tenant) (byte, error) {
	if !meta.ValidTenant(t) {
		return 0, ErrOpen
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	m := k.materialize(t)
	if m.Active == 255 {
		return 0, errors.New("no more key versions")
	}
	m.Active++
	for m.Data[m.Active] != nil {
		if m.Active == 255 {
			return 0, errors.New("no more key versions")
		}
		m.Active++
	}
	m.Data[m.Active] = random()
	return m.Active, nil
}

// Retire forgets a data key version for every tenant once no registered holder of sealed data has an object under it. It fails closed: with no holder registered, with a holder that cannot be read, or while any tenant still writes under the version. Holders live in this process; a second process sharing the same keyring file must be stopped, or must itself have been scanned, before retiring.
func (k *Keyring) Retire(ctx context.Context, v byte) error {
	k.mu.RLock()
	guards := slices.Clone(k.guards)
	k.mu.RUnlock()
	if len(guards) == 0 {
		return fmt.Errorf("%w: no holder of sealed data is registered", ErrInUse)
	}
	for _, g := range guards {
		n, err := g(ctx, v)
		if err != nil {
			return fmt.Errorf("%w: a holder could not be scanned", ErrInUse)
		}
		if n > 0 {
			return ErrInUse
		}
	}
	return k.RetireUnchecked(v)
}

// RetireUnchecked forgets the version without asking anyone. Whatever is still sealed under it becomes unreadable for good; Retire is the safe form.
func (k *Keyring) RetireUnchecked(v byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if v == k.active || k.data[v] == nil {
		return ErrOpen
	}
	for _, m := range k.tenants {
		if m.Active == v {
			return ErrInUse
		}
	}
	delete(k.data, v)
	for _, m := range k.tenants {
		delete(m.Data, v)
	}
	return nil
}

// GuardRetire lets a holder of sealed data register with the sealer's keyring.
func (s *Sealer) GuardRetire(g func(ctx context.Context, version byte) (int, error)) {
	s.k.GuardRetire(g)
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

// Version is the data key version a sealed object was written under.
func (*Sealer) Version(sealed []byte) byte {
	if len(sealed) == 0 {
		return 0
	}
	return sealed[0]
}

// Active is the version new writes for the tenant use.
func (s *Sealer) Active(t meta.Tenant) byte { return s.k.ActiveFor(t) }

// Identify is the tenant-keyed identifier of some bytes, shaped like a digest so it fits wherever one does.
func (s *Sealer) Identify(t meta.Tenant, data []byte) string {
	sum := mac(kdf(s.k.idRoot(t), idInfo), nil, data)
	return "sha256:" + hex.EncodeToString(sum)
}

func (s *Sealer) aad(t meta.Tenant, id string) []byte {
	return []byte("vsf/artifact/v1|" + string(t) + "|" + id)
}

// Seal encrypts data under the tenant's newest data key; the nonce follows from the identifier, so sealing is repeatable.
func (s *Sealer) Seal(t meta.Tenant, id string, data []byte) ([]byte, error) {
	v := s.k.ActiveFor(t)
	root := s.k.dataRoot(t, v)
	if root == nil {
		return nil, ErrOpen
	}
	nonce := mac(kdf(root, nonceInfo), nil, []byte(id))[:nonceLen]
	out := make([]byte, 1, 1+len(data)+tagLen)
	out[0] = v
	return newGCM(kdf(root, dekInfo)).Seal(out, nonce, data, s.aad(t, id)), nil
}

// Open decrypts sealed bytes and proves they are the content the identifier names.
func (s *Sealer) Open(t meta.Tenant, id string, sealed []byte) ([]byte, error) {
	if len(sealed) < 1+tagLen {
		return nil, ErrOpen
	}
	root := s.k.dataRoot(t, sealed[0])
	if root == nil {
		return nil, ErrOpen
	}
	nonce := mac(kdf(root, nonceInfo), nil, []byte(id))[:nonceLen]
	plain, err := newGCM(kdf(root, dekInfo)).Open(nil, nonce, sealed[1:], s.aad(t, id))
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
	return hex.EncodeToString(mac(kdf(b.k.idRoot(b.t), nameInfo+b.purpose), nil, []byte(label)))
}

// Version is the data key version a sealed value was written under; Active is the one new values use.
func (b *Box) Version(sealed []byte) byte {
	if len(sealed) == 0 {
		return 0
	}
	return sealed[0]
}

func (b *Box) Active() byte { return b.k.ActiveFor(b.t) }

// Seal encrypts plaintext bound to aad.
func (b *Box) Seal(aad, plaintext []byte) ([]byte, error) {
	v := b.k.ActiveFor(b.t)
	root := b.k.dataRoot(b.t, v)
	if root == nil {
		return nil, ErrOpen
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(make([]byte, 0, 1+nonceLen+len(plaintext)+tagLen), v)
	out = append(out, nonce...)
	return newGCM(kdf(root, dekInfo+"|"+b.purpose)).Seal(out, nonce, plaintext, aad), nil
}

// Open decrypts what Seal produced for the same tenant, purpose and aad.
func (b *Box) Open(aad, sealed []byte) ([]byte, error) {
	if len(sealed) < 1+nonceLen+tagLen {
		return nil, ErrOpen
	}
	root := b.k.dataRoot(b.t, sealed[0])
	if root == nil {
		return nil, ErrOpen
	}
	plain, err := newGCM(kdf(root, dekInfo+"|"+b.purpose)).Open(nil, sealed[1:1+nonceLen], sealed[1+nonceLen:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}

// --- moving a tenant between installations ---

const (
	wrapIterations = 600_000
	maxIterations  = 10_000_000
	minPassphrase  = 12
	saltLen        = 16
)

type wrapped struct {
	Format     int    `json:"format"`
	Tenant     string `json:"tenant"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Sealed     string `json:"sealed"`
}

func wrapAAD(t string, iter int) []byte {
	return []byte("vsf/tenant-keys/v1|" + t + "|" + strconv.Itoa(iter))
}

// ExportTenant returns one tenant's keys encrypted under a passphrase, for opening that tenant's sealed data in another installation. It holds nothing about other tenants or the roots.
func (k *Keyring) ExportTenant(t meta.Tenant, passphrase string) ([]byte, error) {
	if !meta.ValidTenant(t) || len(passphrase) < minPassphrase {
		return nil, ErrOpen
	}
	k.mu.RLock()
	var tk TenantKeys
	if m, ok := k.tenants[t]; ok {
		tk = *m.clone()
	} else {
		tk = TenantKeys{ID: kdf(k.idKey, tenantIDInfo+string(t)), Data: map[byte][]byte{}, Active: k.active}
		for v, root := range k.data {
			tk.Data[v] = kdf(root, tenantDataInfo+string(t))
		}
	}
	k.mu.RUnlock()
	plain, err := json.Marshal(tk.toJSON())
	if err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	wk, err := pbkdf2.Key(sha256.New, passphrase, salt, wrapIterations, keyLen)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := newGCM(wk).Seal(append([]byte(nil), nonce...), nonce, plain, wrapAAD(string(t), wrapIterations))
	zero(wk)
	return json.Marshal(wrapped{Format: 1, Tenant: string(t), KDF: "pbkdf2-hmac-sha256", Iterations: wrapIterations,
		Salt: hex.EncodeToString(salt), Sealed: hex.EncodeToString(sealed)})
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ImportTenant opens an export. It refuses a file asking for fewer iterations than this build writes, so a forged file cannot make a guessed passphrase cheap.
func ImportTenant(raw []byte, passphrase string) (meta.Tenant, TenantKeys, error) {
	var w wrapped
	if len(raw) > 64<<10 || json.Unmarshal(raw, &w) != nil || w.Format != 1 || w.KDF != "pbkdf2-hmac-sha256" ||
		w.Iterations < wrapIterations || w.Iterations > maxIterations || !meta.ValidTenant(meta.Tenant(w.Tenant)) {
		return "", TenantKeys{}, ErrKeyFile
	}
	salt, err1 := hex.DecodeString(w.Salt)
	sealed, err2 := hex.DecodeString(w.Sealed)
	if err1 != nil || err2 != nil || len(salt) != saltLen || len(sealed) < nonceLen+tagLen {
		return "", TenantKeys{}, ErrKeyFile
	}
	wk, err := pbkdf2.Key(sha256.New, passphrase, salt, w.Iterations, keyLen)
	if err != nil {
		return "", TenantKeys{}, err
	}
	defer zero(wk)
	plain, err := newGCM(wk).Open(nil, sealed[:nonceLen], sealed[nonceLen:], wrapAAD(w.Tenant, w.Iterations))
	if err != nil {
		return "", TenantKeys{}, ErrOpen
	}
	var j tenantJSON
	if json.Unmarshal(plain, &j) != nil {
		return "", TenantKeys{}, ErrKeyFile
	}
	tk, err := j.keys()
	if err != nil {
		return "", TenantKeys{}, err
	}
	return meta.Tenant(w.Tenant), tk, nil
}

// Adopt makes this keyring open and write a tenant with imported keys. It refuses to replace any key it already holds with a different one, and it never drops an imported version: either every version is merged or none is.
func (k *Keyring) Adopt(t meta.Tenant, keys TenantKeys) error {
	if !meta.ValidTenant(t) || !keys.valid() {
		return ErrKeyFile
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	have, held := k.tenants[t]
	rootID := kdf(k.idKey, tenantIDInfo+string(t))
	if held && !hmac.Equal(have.ID, keys.ID) {
		return ErrKeyFile
	}
	if !held && hmac.Equal(rootID, keys.ID) {
		have = nil
	} else if !held {
		k.tenants[t] = keys.clone()
		return nil
	}
	known := func(v byte) []byte {
		if have != nil {
			if cur, ok := have.Data[v]; ok {
				return cur
			}
		}
		if root, ok := k.data[v]; ok && !held {
			return kdf(root, tenantDataInfo+string(t))
		}
		return nil
	}
	for v, key := range keys.Data {
		if cur := known(v); cur != nil && !hmac.Equal(cur, key) {
			return ErrKeyFile
		}
	}
	if have == nil {
		have = k.materialize(t)
	}
	for v, key := range keys.Data {
		if have.Data[v] == nil {
			have.Data[v] = append([]byte(nil), key...)
		}
	}
	if keys.Active > have.Active {
		have.Active = keys.Active
	}
	return nil
}
