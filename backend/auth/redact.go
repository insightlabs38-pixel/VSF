package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"sort"
	"sync"
)

const (
	redacted     = "[redacted]"
	minRedactLen = 4
)

// Redactor removes known secrets, in the forms they usually take in logs, from text, errors and streams.
type Redactor struct {
	mu   sync.RWMutex
	pats [][]byte
}

func NewRedactor() *Redactor { return &Redactor{} }

func variants(secret []byte) [][]byte {
	s := string(secret)
	js, _ := json.Marshal(s)
	var plain bytes.Buffer
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	pj := bytes.TrimSpace(plain.Bytes())
	vs := [][]byte{
		secret,
		[]byte(base64.StdEncoding.EncodeToString(secret)),
		[]byte(base64.RawStdEncoding.EncodeToString(secret)),
		[]byte(base64.URLEncoding.EncodeToString(secret)),
		[]byte(base64.RawURLEncoding.EncodeToString(secret)),
		[]byte(hex.EncodeToString(secret)),
		[]byte(url.QueryEscape(s)),
		[]byte(url.PathEscape(s)),
		js[1 : len(js)-1],
		pj[1 : len(pj)-1],
	}
	return vs
}

// Add registers a secret; one too short to tell apart from ordinary text is refused, since redacting it would mangle every message.
func (r *Redactor) Add(secret []byte) error {
	if len(secret) < minRedactLen {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range variants(secret) {
		if len(v) >= minRedactLen && !r.has(v) {
			r.pats = append(r.pats, append([]byte(nil), v...))
		}
	}
	sort.Slice(r.pats, func(i, j int) bool { return len(r.pats[i]) > len(r.pats[j]) })
	return nil
}

func (r *Redactor) has(v []byte) bool {
	for _, p := range r.pats {
		if bytes.Equal(p, v) {
			return true
		}
	}
	return false
}

// Bytes returns b with every registered secret replaced.
func (r *Redactor) Bytes(b []byte) []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.pats {
		b = bytes.ReplaceAll(b, p, []byte(redacted))
	}
	return b
}

func (r *Redactor) String(s string) string { return string(r.Bytes([]byte(s))) }

type redactedError struct{ msg string }

func (e redactedError) Error() string { return e.msg }

// Err returns an error whose text is clean; the original is kept for errors.Is and errors.As.
func (r *Redactor) Err(err error) error {
	if err == nil {
		return nil
	}
	return &wrapped{redactedError{r.String(err.Error())}, err}
}

type wrapped struct {
	redactedError
	inner error
}

func (w *wrapped) Unwrap() error { return w.inner }

// Writer returns a writer that redacts line by line, so a secret is caught even when a log call splits it across writes within one line.
func (r *Redactor) Writer(w io.Writer) io.WriteCloser { return &lineWriter{r: r, w: w} }

type lineWriter struct {
	r   *Redactor
	w   io.Writer
	buf []byte
}

const maxLine = 1 << 20

func (l *lineWriter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		if _, err := l.w.Write(l.r.Bytes(append([]byte(nil), l.buf[:i+1]...))); err != nil {
			return 0, err
		}
		l.buf = l.buf[i+1:]
	}
	if len(l.buf) > maxLine {
		return 0, errors.New("log line too long to redact")
	}
	return len(p), nil
}

// Close writes whatever is left of an unfinished line.
func (l *lineWriter) Close() error {
	if len(l.buf) == 0 {
		return nil
	}
	_, err := l.w.Write(l.r.Bytes(l.buf))
	l.buf = nil
	return err
}
