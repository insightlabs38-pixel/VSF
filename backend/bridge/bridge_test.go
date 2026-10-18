package bridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const extOrigin = "chrome-extension://lgnekpogndcjggibkbaekekanfonjhie"

type fixture struct {
	srv   *Server
	addr  string
	token string
	clock *fakeClock
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func start(t *testing.T, mutate func(*Config)) *fixture {
	t.Helper()
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	cfg := Config{Token: token, AllowedOrigins: []string{extOrigin}, Now: clock.now}
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })
	return &fixture{srv: srv, addr: addr, token: token, clock: clock}
}

type opts struct {
	host, origin, auth, ctype string
	headers                   map[string]string
	body                      []byte
}

func (f *fixture) do(t *testing.T, method, path string, o opts) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+f.addr+path, bytes.NewReader(o.body))
	if err != nil {
		t.Fatal(err)
	}
	if o.host != "" {
		req.Host = o.host
	}
	if o.origin != "" {
		req.Header.Set("Origin", o.origin)
	}
	if o.auth != "" {
		req.Header.Set("Authorization", o.auth)
	}
	if o.ctype != "" {
		req.Header.Set("Content-Type", o.ctype)
	}
	for k, v := range o.headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func (f *fixture) good(t *testing.T, method, path string, body []byte, ctype string) (int, string) {
	t.Helper()
	status, b, _ := f.do(t, method, path, opts{origin: extOrigin, auth: "Bearer " + f.token, ctype: ctype, headers: map[string]string{csrfHeader: "1"}, body: body})
	return status, b
}

func envelope() []byte {
	return []byte(`{"schema":"vsf.smoke.envelope/1.0.0","schema_digest":"` + knownDigest() + `","seq":1,"payload":{"a":1}}`)
}

func TestAuthorizedExtensionCallSucceeds(t *testing.T) {
	f := start(t, nil)
	if status, body := f.good(t, "GET", "/v1/ping", nil, ""); status != 200 || !strings.Contains(body, "vsf.smoke.envelope") {
		t.Fatalf("ping: %d %s", status, body)
	}
	status, body := f.good(t, "POST", "/v1/envelope/verify", envelope(), "application/json")
	if status != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("verify: %d %s", status, body)
	}
}

func TestUnauthenticatedAndWrongCredentialsAreDenied(t *testing.T) {
	f := start(t, nil)
	cases := []struct {
		name string
		o    opts
	}{
		{"no credential", opts{}},
		{"wrong token", opts{auth: "Bearer " + strings.Repeat("0", 64)}},
		{"basic scheme", opts{auth: "Basic " + f.token}},
		{"token as raw header", opts{auth: f.token}},
		{"empty bearer", opts{auth: "Bearer "}},
	}
	for _, c := range cases {
		if status, body, _ := f.do(t, "GET", "/v1/ping", c.o); status != 401 || !strings.Contains(body, "bridge.unauthorized") {
			t.Errorf("%s: %d %s", c.name, status, body)
		}
	}
	if status, _, _ := f.do(t, "GET", "/v1/ping?token="+f.token, opts{}); status != 401 {
		t.Errorf("token in query must not authenticate: %d", status)
	}
}

func TestDNSRebindingHostMismatchIsDenied(t *testing.T) {
	f := start(t, nil)
	_, port, _ := net.SplitHostPort(f.addr)
	for _, host := range []string{"evil.example:" + port, "127.0.0.1.evil.example:" + port, "127.0.0.1", "localhost", "127.0.0.1:1", "[::1]:" + port, ""} {
		status, body, _ := f.do(t, "GET", "/v1/ping", opts{host: host, auth: "Bearer " + f.token, origin: extOrigin})
		if host == "" {
			continue
		}
		if status != 403 || !strings.Contains(body, "bridge.host_not_allowed") {
			t.Errorf("host %q: %d %s", host, status, body)
		}
	}
	if status, _, _ := f.do(t, "GET", "/v1/ping", opts{host: "localhost:" + port, auth: "Bearer " + f.token}); status != 200 {
		t.Errorf("localhost alias must work: %d", status)
	}
}

func TestForeignAndNullOriginsAreDeniedWithoutCORSGrants(t *testing.T) {
	f := start(t, nil)
	for _, origin := range []string{"https://evil.example", "http://127.0.0.1:1", "null", "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", extOrigin + ".evil"} {
		status, body, h := f.do(t, "GET", "/v1/ping", opts{origin: origin, auth: "Bearer " + f.token})
		if status != 403 || !strings.Contains(body, "bridge.origin_not_allowed") {
			t.Errorf("origin %q: %d %s", origin, status, body)
		}
		if h.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("origin %q received CORS grant", origin)
		}
	}
	status, _, h := f.do(t, "OPTIONS", "/v1/envelope/verify", opts{origin: "https://evil.example", headers: map[string]string{"Access-Control-Request-Method": "POST"}})
	if status != 403 || h.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("foreign preflight: %d", status)
	}
	status, _, h = f.do(t, "OPTIONS", "/v1/envelope/verify", opts{origin: extOrigin})
	if status != 204 || h.Get("Access-Control-Allow-Origin") != extOrigin || !strings.Contains(h.Get("Access-Control-Allow-Headers"), csrfHeader) {
		t.Errorf("allowed preflight: %d %v", status, h)
	}
	if status, _, _ := f.do(t, "OPTIONS", "/v1/ping", opts{}); status != 403 {
		t.Errorf("OPTIONS without origin must be denied: %d", status)
	}
	if status, _, _ := f.do(t, "GET", "/v1/ping", opts{auth: "Bearer " + f.token, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}}); status != 403 {
		t.Errorf("cross-site fetch metadata without origin: %d", status)
	}
}

func TestCSRFFormPostsAndSimpleRequestsAreRefused(t *testing.T) {
	f := start(t, nil)
	auth := "Bearer " + f.token
	for _, ct := range []string{"application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "text/plain", ""} {
		status, body, _ := f.do(t, "POST", "/v1/envelope/verify", opts{origin: extOrigin, auth: auth, ctype: ct, headers: map[string]string{csrfHeader: "1"}, body: envelope()})
		if status != 415 || !strings.Contains(body, "unsupported_media_type") {
			t.Errorf("content type %q: %d %s", ct, status, body)
		}
	}
	status, body, _ := f.do(t, "POST", "/v1/envelope/verify", opts{origin: extOrigin, auth: auth, ctype: "application/json", body: envelope()})
	if status != 403 || !strings.Contains(body, "csrf_header_missing") {
		t.Errorf("missing csrf header: %d %s", status, body)
	}
	if status, _, _ := f.do(t, "POST", "/v1/envelope/verify", opts{origin: extOrigin, auth: auth, ctype: "application/json", headers: map[string]string{csrfHeader: "0"}, body: envelope()}); status != 403 {
		t.Errorf("wrong csrf value: %d", status)
	}
}

func TestMalformedRequestsFailClosedWithStableCodes(t *testing.T) {
	f := start(t, nil)
	cases := []struct {
		method, path, ctype string
		body                []byte
		status              int
		code                string
	}{
		{"GET", "/v1/nope", "", nil, 404, "bridge.not_found"},
		{"POST", "/v1/ping", "application/json", []byte("{}"), 405, "bridge.method_not_allowed"},
		{"GET", "/v1/envelope/verify", "", nil, 405, "bridge.method_not_allowed"},
		{"POST", "/v1/envelope/verify", "application/json", []byte("{not json"), 422, "canon.syntax"},
		{"POST", "/v1/envelope/verify", "application/json", []byte(""), 422, "canon.syntax"},
		{"POST", "/v1/envelope/verify", "application/json", []byte(`{"schema":"vsf.smoke.envelope/2.0.0","schema_digest":"x","seq":1,"payload":null}`), 422, "schema.incompatible_major"},
		{"POST", "/v1/envelope/verify", "application/json", append(envelope()[:20:20], 0xff, 0xfe), 422, "canon.invalid_utf8"},
		{"POST", "/v1/envelope/verify", "application/json", bytes.Repeat([]byte(" "), DefaultMaxBody+1), 413, "bridge.payload_too_large"},
		{"POST", "/v1/upload/begin", "application/json", []byte(`{"total_bytes":1,"chunks":1,"sha256":"zz"}`), 400, "bridge.bad_request"},
		{"POST", "/v1/upload/begin", "application/json", []byte(`{"unknown":1}`), 400, "bridge.bad_json"},
		{"POST", "/v1/upload/begin", "application/json", []byte(`[]`), 400, "bridge.bad_json"},
		{"POST", "/v1/upload/not-an-id/chunk?seq=0", "application/octet-stream", []byte("x"), 404, "bridge.not_found"},
		{"GET", "/v1/%00", "", nil, 404, "bridge.not_found"},
		{"GET", "/v1/../v1/ping/x", "", nil, 404, "bridge.not_found"},
	}
	for _, c := range cases {
		status, body := f.good(t, c.method, c.path, c.body, c.ctype)
		if status != c.status || !strings.Contains(body, c.code) {
			t.Errorf("%s %s: got %d %s want %d %s", c.method, c.path, status, body, c.status, c.code)
		}
	}
}

func rawRequest(t *testing.T, addr, payload string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	conn.Write([]byte(payload))
	b, _ := io.ReadAll(conn)
	return string(b)
}

func TestOversizedHeadersAndSlowHeadersAreCut(t *testing.T) {
	f := start(t, nil)
	huge := "GET /v1/ping HTTP/1.1\r\nHost: " + f.addr + "\r\nX-Pad: " + strings.Repeat("a", 64<<10) + "\r\n\r\n"
	if resp := rawRequest(t, f.addr, huge); !strings.Contains(resp, "431") {
		t.Errorf("expected 431, got %q", resp[:min(60, len(resp))])
	}
	conn, err := net.Dial("tcp", f.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("GET /v1/ping HTTP/1.1\r\nHost: " + f.addr + "\r\n"))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	_, _ = io.ReadAll(conn)
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("slow header connection survived %v", elapsed)
	}
}

func TestBindIsLoopbackOnlyAndConfigIsValidated(t *testing.T) {
	f := start(t, nil)
	host, _, _ := net.SplitHostPort(f.addr)
	if host != "127.0.0.1" {
		t.Fatalf("bound to %s", host)
	}
	for _, bad := range []Config{
		{Token: "short"},
		{Token: strings.Repeat("a", 32), AllowedOrigins: []string{"*"}},
		{Token: strings.Repeat("a", 32), AllowedOrigins: []string{"null"}},
		{Token: strings.Repeat("a", 32), AllowedOrigins: []string{"https://*.example"}},
		{Token: strings.Repeat("a", 32), AllowedOrigins: []string{""}},
	} {
		if _, err := New(bad); err == nil {
			t.Errorf("config %+v should be rejected", bad)
		}
	}
}

func chunked(t *testing.T, f *fixture, data []byte, size int, mutate func(id string, chunks [][]byte)) (int, string) {
	t.Helper()
	sum := sha256.Sum256(data)
	var chunks [][]byte
	for i := 0; i < len(data); i += size {
		chunks = append(chunks, data[i:min(len(data), i+size)])
	}
	begin := fmt.Sprintf(`{"total_bytes":%d,"chunks":%d,"sha256":"%s"}`, len(data), len(chunks), hex.EncodeToString(sum[:]))
	status, body := f.good(t, "POST", "/v1/upload/begin", []byte(begin), "application/json")
	if status != 200 {
		return status, body
	}
	id := body[strings.Index(body, `"id":"`)+6:][:32]
	if mutate != nil {
		mutate(id, chunks)
	}
	for i, c := range chunks {
		if status, body := f.good(t, "POST", fmt.Sprintf("/v1/upload/%s/chunk?seq=%d", id, i), c, "application/octet-stream"); status != 200 {
			return status, body
		}
	}
	return f.good(t, "POST", "/v1/upload/"+id+"/finish", nil, "application/json")
}

func bigEnvelope(n int) []byte {
	return []byte(`{"schema":"vsf.smoke.envelope/1.0.0","schema_digest":"` + knownDigest() + `","seq":1,"payload":"` + strings.Repeat("x", n) + `"}`)
}

func TestLargePayloadChunkingRoundTrips(t *testing.T) {
	f := start(t, nil)
	data := bigEnvelope(5 << 20)
	if status, _ := f.good(t, "POST", "/v1/envelope/verify", data, "application/json"); status != 413 {
		t.Fatalf("single large body must be refused, got %d", status)
	}
	status, body := chunked(t, f, data, 1<<20, nil)
	if status != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("chunked: %d %s", status, body)
	}
}

func TestChunkingAdversarialCases(t *testing.T) {
	f := start(t, nil)
	data := bigEnvelope(3 << 20)
	if status, body := chunked(t, f, data, 1<<20, func(id string, chunks [][]byte) {
		f.good(t, "POST", fmt.Sprintf("/v1/upload/%s/chunk?seq=0", id), chunks[0], "application/octet-stream")
	}); status != 409 || !strings.Contains(body, "duplicate_chunk") {
		t.Errorf("duplicate chunk: %d %s", status, body)
	}
	sum := sha256.Sum256(data)
	begin := fmt.Sprintf(`{"total_bytes":%d,"chunks":4,"sha256":"%s"}`, len(data), hex.EncodeToString(sum[:]))
	_, body := f.good(t, "POST", "/v1/upload/begin", []byte(begin), "application/json")
	id := body[strings.Index(body, `"id":"`)+6:][:32]
	f.good(t, "POST", fmt.Sprintf("/v1/upload/%s/chunk?seq=0", id), data[:1<<20], "application/octet-stream")
	if status, b := f.good(t, "POST", "/v1/upload/"+id+"/finish", nil, "application/json"); status != 409 || !strings.Contains(b, "upload_incomplete") {
		t.Errorf("incomplete finish: %d %s", status, b)
	}
	if status, b := f.good(t, "POST", fmt.Sprintf("/v1/upload/%s/chunk?seq=9", id), []byte("x"), "application/octet-stream"); status != 400 {
		t.Errorf("seq out of range: %d %s", status, b)
	}
	tampered := append([]byte(nil), data...)
	tampered[100] ^= 1
	bad := fmt.Sprintf(`{"total_bytes":%d,"chunks":4,"sha256":"%s"}`, len(tampered), hex.EncodeToString(sum[:]))
	_, body = f.good(t, "POST", "/v1/upload/begin", []byte(bad), "application/json")
	id = body[strings.Index(body, `"id":"`)+6:][:32]
	for i := 0; i < 4; i++ {
		f.good(t, "POST", fmt.Sprintf("/v1/upload/%s/chunk?seq=%d", id, i), tampered[min(len(tampered), i<<20):min(len(tampered), (i+1)<<20)], "application/octet-stream")
	}
	if status, b := f.good(t, "POST", "/v1/upload/"+id+"/finish", nil, "application/json"); status != 422 || !strings.Contains(b, "digest_mismatch") {
		t.Errorf("tampered content: %d %s", status, b)
	}
	if status, b := f.good(t, "POST", "/v1/upload/begin", []byte(`{"total_bytes":99999999999,"chunks":10,"sha256":"`+strings.Repeat("a", 64)+`"}`), "application/json"); status != 413 {
		t.Errorf("declared size over cap: %d %s", status, b)
	}
	if status, b := f.good(t, "POST", fmt.Sprintf("/v1/upload/%s/chunk?seq=0", strings.Repeat("0", 32)), []byte("x"), "application/octet-stream"); status != 404 || !strings.Contains(b, "unknown_upload") {
		t.Errorf("unknown upload: %d %s", status, b)
	}
}

func TestUploadLimitsAndExpiry(t *testing.T) {
	f := start(t, func(c *Config) { c.MaxUploads = 2 })
	begin := []byte(`{"total_bytes":10,"chunks":1,"sha256":"` + strings.Repeat("a", 64) + `"}`)
	var ids []string
	for i := 0; i < 2; i++ {
		status, body := f.good(t, "POST", "/v1/upload/begin", begin, "application/json")
		if status != 200 {
			t.Fatalf("begin %d: %d %s", i, status, body)
		}
		ids = append(ids, body[strings.Index(body, `"id":"`)+6:][:32])
	}
	if status, b := f.good(t, "POST", "/v1/upload/begin", begin, "application/json"); status != 429 {
		t.Errorf("third upload: %d %s", status, b)
	}
	f.clock.advance(DefaultUploadExpiry + time.Second)
	if status, b := f.good(t, "POST", fmt.Sprintf("/v1/upload/%s/chunk?seq=0", ids[0]), []byte("0123456789"), "application/octet-stream"); status != 404 {
		t.Errorf("expired upload must be gone: %d %s", status, b)
	}
	if status, _ := f.good(t, "POST", "/v1/upload/begin", begin, "application/json"); status != 200 {
		t.Errorf("slots must free after expiry: %d", status)
	}
}

func TestConcurrentUploadsAreRaceFree(t *testing.T) {
	f := start(t, func(c *Config) { c.MaxUploads = 16 })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if status, body := chunked(t, f, bigEnvelope(1<<20), 256<<10, nil); status != 200 {
				t.Errorf("concurrent upload: %d %s", status, body)
			}
		}()
	}
	wg.Wait()
}
