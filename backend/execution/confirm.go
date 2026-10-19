package execution

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// Confirmation is a person's yes, bound to one request digest and usable once.
type Confirmation struct {
	Token     string
	By        string
	Digest    string
	ExpiresAt time.Time
}

type Confirmer struct {
	mu     sync.Mutex
	secret []byte
	used   map[string]bool
}

func NewConfirmer(secret []byte) *Confirmer {
	return &Confirmer{secret: append([]byte(nil), secret...), used: map[string]bool{}}
}

func (c *Confirmer) mac(digest, by string, exp time.Time) string {
	m := hmac.New(sha256.New, c.secret)
	m.Write([]byte(digest + "\x00" + by + "\x00" + strconv.FormatInt(exp.UnixMilli(), 10)))
	return hex.EncodeToString(m.Sum(nil))
}

func (c *Confirmer) Issue(digest, by string, exp time.Time) Confirmation {
	return Confirmation{Token: c.mac(digest, by, exp), By: by, Digest: digest, ExpiresAt: exp}
}

// Valid checks the token without spending it.
func (c *Confirmer) Valid(conf Confirmation, digest string, now time.Time) bool {
	if conf.Token == "" || conf.By == "" || conf.Digest != digest || !conf.ExpiresAt.After(now) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return hmac.Equal([]byte(conf.Token), []byte(c.mac(digest, conf.By, conf.ExpiresAt))) && !c.used[conf.Token]
}

// Use checks the token and spends it in one step; a token works once.
func (c *Confirmer) Use(conf Confirmation, digest string, now time.Time) bool {
	if conf.Token == "" || conf.By == "" || conf.Digest != digest || !conf.ExpiresAt.After(now) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used[conf.Token] || !hmac.Equal([]byte(conf.Token), []byte(c.mac(digest, conf.By, conf.ExpiresAt))) {
		return false
	}
	c.used[conf.Token] = true
	return true
}

func requestDigest(contract, record string, params map[string]any) string {
	b, _ := json.Marshal(struct {
		C string         `json:"c"`
		R string         `json:"r"`
		P map[string]any `json:"p"`
	}{contract, record, params})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
