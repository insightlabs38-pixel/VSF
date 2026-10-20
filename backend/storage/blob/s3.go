package blob

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// S3 speaks the path-style S3 REST dialect every commodity object store accepts, signed with SigV4.
type S3 struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Client    *http.Client
	Now       func() time.Time
}

func (s *S3) Name() string { return "s3" }

func (s *S3) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}

func (s *S3) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hm(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func uriEscape(s string, keepSlash bool) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~', c == '/' && keepSlash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEscape(k, false)+"="+uriEscape(v, false))
		}
	}
	return strings.Join(parts, "&")
}

func (s *S3) do(ctx context.Context, method, key string, q url.Values, body []byte, hdr map[string]string) (*http.Response, error) {
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		return nil, err
	}
	path := "/" + s.Bucket
	if key != "" {
		path += "/" + key
	}
	now := s.now()
	amzDate, day := now.Format("20060102T150405Z"), now.Format("20060102")
	payload := sha(body)
	headers := map[string]string{"host": u.Host, "x-amz-content-sha256": payload, "x-amz-date": amzDate}
	for k, v := range hdr {
		headers[strings.ToLower(k)] = v
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + strings.TrimSpace(headers[k]) + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := strings.Join([]string{method, uriEscape(path, true), canonicalQuery(q), canonHeaders.String(), signed, payload}, "\n")
	scope := day + "/" + s.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha([]byte(canonical))
	k := hm([]byte("AWS4"+s.SecretKey), day)
	k = hm(k, s.Region)
	k = hm(k, "s3")
	k = hm(k, "aws4_request")
	sig := hex.EncodeToString(hm(k, toSign))
	target := strings.TrimRight(s.Endpoint, "/") + uriEscape(path, true)
	if len(q) > 0 {
		target += "?" + canonicalQuery(q)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		if k != "host" {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", s.AccessKey, scope, signed, sig))
	return s.client().Do(req)
}

func drain(r *http.Response) []byte {
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, MaxObject+1))
	return b
}

func (s *S3) fail(r *http.Response) error {
	b := drain(r)
	if len(b) > 300 {
		b = b[:300]
	}
	return fmt.Errorf("s3 %s: %s", r.Status, strings.TrimSpace(string(b)))
}

func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	if !ValidKey(key) {
		return ErrBadKey
	}
	if len(data) > MaxObject {
		return ErrTooLarge
	}
	r, err := s.do(ctx, "PUT", key, nil, data, map[string]string{"if-none-match": "*"})
	if err != nil {
		return err
	}
	switch r.StatusCode {
	case http.StatusOK:
		drain(r)
		return nil
	case http.StatusPreconditionFailed, http.StatusConflict:
		drain(r)
		old, gerr := s.Get(ctx, key)
		if gerr != nil {
			return gerr
		}
		if bytes.Equal(old, data) {
			return nil
		}
		return ErrOverwrite
	}
	return s.fail(r)
}

func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	if !ValidKey(key) {
		return nil, ErrBadKey
	}
	r, err := s.do(ctx, "GET", key, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	switch r.StatusCode {
	case http.StatusOK:
		return drain(r), nil
	case http.StatusNotFound:
		drain(r)
		return nil, ErrNotFound
	}
	return nil, s.fail(r)
}

func (s *S3) Stat(ctx context.Context, key string) (Info, error) {
	if !ValidKey(key) {
		return Info{}, ErrBadKey
	}
	r, err := s.do(ctx, "HEAD", key, nil, nil, nil)
	if err != nil {
		return Info{}, err
	}
	defer r.Body.Close()
	switch r.StatusCode {
	case http.StatusOK:
		return Info{Key: key, Size: r.ContentLength}, nil
	case http.StatusNotFound:
		return Info{}, ErrNotFound
	}
	return Info{}, fmt.Errorf("s3 %s", r.Status)
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if !ValidKey(key) {
		return ErrBadKey
	}
	if _, err := s.Stat(ctx, key); err != nil {
		return err
	}
	r, err := s.do(ctx, "DELETE", key, nil, nil, nil)
	if err != nil {
		return err
	}
	if r.StatusCode == http.StatusNoContent || r.StatusCode == http.StatusOK {
		drain(r)
		return nil
	}
	return s.fail(r)
}

type listResult struct {
	IsTruncated bool `xml:"IsTruncated"`
	Contents    []struct {
		Key  string `xml:"Key"`
		Size int64  `xml:"Size"`
	} `xml:"Contents"`
}

func (s *S3) List(ctx context.Context, prefix, after string, limit int) ([]Info, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	q := url.Values{"list-type": {"2"}, "prefix": {prefix}, "max-keys": {strconv.Itoa(limit)}}
	if after != "" {
		q.Set("start-after", after)
	}
	r, err := s.do(ctx, "GET", "", q, nil, nil)
	if err != nil {
		return nil, err
	}
	if r.StatusCode != http.StatusOK {
		return nil, s.fail(r)
	}
	var res listResult
	if err := xml.NewDecoder(bytes.NewReader(drain(r))).Decode(&res); err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(res.Contents))
	for _, c := range res.Contents {
		out = append(out, Info{Key: c.Key, Size: c.Size})
	}
	return out, nil
}

// CreateBucket makes the bucket if it is missing; tests and the benchmark use it, the product expects one to exist.
func (s *S3) CreateBucket(ctx context.Context) error {
	r, err := s.do(ctx, "PUT", "", nil, nil, nil)
	if err != nil {
		return err
	}
	if r.StatusCode == http.StatusOK || r.StatusCode == http.StatusConflict {
		drain(r)
		return nil
	}
	return s.fail(r)
}

var _ Backend = (*S3)(nil)
