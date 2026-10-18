// Package bridge is the loopback HTTP bridge prototype between a paired client (the extension) and the local service.
package bridge

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/schema"
)

const (
	DefaultMaxBody      = 1 << 20
	DefaultMaxUpload    = 64 << 20
	DefaultMaxChunk     = 1 << 20
	DefaultMaxChunks    = 128
	DefaultMaxUploads   = 4
	DefaultUploadExpiry = 30 * time.Second
	csrfHeader          = "X-VSF-Bridge"
)

type Config struct {
	// Token is the pairing secret presented as a bearer credential; at least 32 hex characters.
	Token string
	// AllowedOrigins are exact browser origins (for example chrome-extension://<id>) allowed to call the bridge.
	AllowedOrigins []string
	MaxBody        int64
	MaxUpload      int64
	MaxChunk       int64
	MaxChunks      int
	MaxUploads     int
	UploadExpiry   time.Duration
	Now            func() time.Time
	// OnRequest receives one record per request after it completes; it never sees credentials or bodies.
	OnRequest func(RequestRecord)
}

type RequestRecord struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Origin string `json:"origin"`
	Host   string `json:"host"`
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

type Server struct {
	cfg     Config
	origins map[string]bool
	hosts   map[string]bool
	uploads *uploads
	httpSrv *http.Server
	ln      net.Listener
}

func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func New(cfg Config) (*Server, error) {
	if len(cfg.Token) < 32 {
		return nil, errors.New("bridge: token must be at least 32 characters")
	}
	if cfg.MaxBody == 0 {
		cfg.MaxBody = DefaultMaxBody
	}
	if cfg.MaxUpload == 0 {
		cfg.MaxUpload = DefaultMaxUpload
	}
	if cfg.MaxChunk == 0 {
		cfg.MaxChunk = DefaultMaxChunk
	}
	if cfg.MaxChunks == 0 {
		cfg.MaxChunks = DefaultMaxChunks
	}
	if cfg.MaxUploads == 0 {
		cfg.MaxUploads = DefaultMaxUploads
	}
	if cfg.UploadExpiry == 0 {
		cfg.UploadExpiry = DefaultUploadExpiry
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &Server{cfg: cfg, origins: map[string]bool{}, hosts: map[string]bool{}}
	for _, o := range cfg.AllowedOrigins {
		if o == "" || o == "null" || strings.Contains(o, "*") {
			return nil, fmt.Errorf("bridge: invalid allowed origin %q", o)
		}
		s.origins[o] = true
	}
	s.uploads = newUploads(cfg)
	return s, nil
}

// Listen binds an ephemeral loopback port; the Host allowlist is derived from the bound address.
func (s *Server) Listen() (string, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	s.ln = ln
	port := ln.Addr().(*net.TCPAddr).Port
	s.hosts[fmt.Sprintf("127.0.0.1:%d", port)] = true
	s.hosts[fmt.Sprintf("localhost:%d", port)] = true
	s.httpSrv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return ln.Addr().String(), nil
}

func (s *Server) Serve() error { return s.httpSrv.Serve(s.ln) }

func (s *Server) Close() error { return s.httpSrv.Close() }

type errBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func deny(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errBody{Error: code})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.OnRequest != nil {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			s.cfg.OnRequest(RequestRecord{Method: r.Method, Path: r.URL.Path, Status: sw.status, Origin: r.Header.Get("Origin"), Host: r.Host})
		}()
		w = sw
	}
	s.handle(w, r)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// 1. DNS rebinding: the Host header must name the bound loopback address.
	if !s.hosts[r.Host] {
		deny(w, http.StatusForbidden, "bridge.host_not_allowed")
		return
	}
	// 2. Origin: browsers always send it on cross-origin and on non-GET requests; unknown origins never get through.
	origin := r.Header.Get("Origin")
	if origin != "" && !s.origins[origin] {
		deny(w, http.StatusForbidden, "bridge.origin_not_allowed")
		return
	}
	if origin == "" && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		deny(w, http.StatusForbidden, "bridge.origin_not_allowed")
		return
	}
	if origin != "" {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Vary", "Origin")
	}
	if r.Method == http.MethodOptions {
		if origin == "" {
			deny(w, http.StatusForbidden, "bridge.origin_not_allowed")
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Methods", "GET, POST")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, "+csrfHeader)
		h.Set("Access-Control-Max-Age", "60")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// 3. Authentication: bearer token only; never from URL, cookie or form field.
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) != 1 {
		deny(w, http.StatusUnauthorized, "bridge.unauthorized")
		return
	}
	// 4. CSRF: state-changing calls need a non-simple content type and a custom header (forces a CORS preflight).
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
		if ct != "application/json" && ct != "application/octet-stream" {
			deny(w, http.StatusUnsupportedMediaType, "bridge.unsupported_media_type")
			return
		}
		if r.Header.Get(csrfHeader) != "1" {
			deny(w, http.StatusForbidden, "bridge.csrf_header_missing")
			return
		}
	}
	s.route(w, r)
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/v1/ping":
		if r.Method != http.MethodGet {
			deny(w, http.StatusMethodNotAllowed, "bridge.method_not_allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schema": schema.SchemaName + "/" + schema.CurrentVersion})
	case path == "/v1/envelope/verify":
		if r.Method != http.MethodPost {
			deny(w, http.StatusMethodNotAllowed, "bridge.method_not_allowed")
			return
		}
		body, status, code := s.readBody(w, r, s.cfg.MaxBody)
		if code != "" {
			deny(w, status, code)
			return
		}
		s.verify(w, body)
	case path == "/v1/upload/begin":
		if r.Method != http.MethodPost {
			deny(w, http.StatusMethodNotAllowed, "bridge.method_not_allowed")
			return
		}
		s.uploadBegin(w, r)
	case strings.HasPrefix(path, "/v1/upload/"):
		s.uploadRoute(w, r, strings.TrimPrefix(path, "/v1/upload/"))
	default:
		deny(w, http.StatusNotFound, "bridge.not_found")
	}
}

func (s *Server) readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, int, string) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, http.StatusRequestEntityTooLarge, "bridge.payload_too_large"
		}
		return nil, http.StatusBadRequest, "bridge.read_failed"
	}
	return body, 0, ""
}

func (s *Server) verify(w http.ResponseWriter, body []byte) {
	_, canonical, err := schema.VerifyBytes(body)
	if err != nil {
		deny(w, http.StatusUnprocessableEntity, schema.ErrorCode(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bytes": len(canonical), "sha256": canon.SHA256Hex(canonical)})
}
