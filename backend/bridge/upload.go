package bridge

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
)

type upload struct {
	total    int64
	chunks   int
	sha256   string
	received map[int][]byte
	size     int64
	touched  time.Time
}

type uploads struct {
	mu  sync.Mutex
	cfg Config
	m   map[string]*upload
}

func newUploads(cfg Config) *uploads { return &uploads{cfg: cfg, m: map[string]*upload{}} }

func (u *uploads) sweep() {
	now := u.cfg.Now()
	for id, up := range u.m {
		if now.Sub(up.touched) > u.cfg.UploadExpiry {
			delete(u.m, id)
		}
	}
}

func (s *Server) uploadBegin(w http.ResponseWriter, r *http.Request) {
	body, status, code := s.readBody(w, r, 4<<10)
	if code != "" {
		deny(w, status, code)
		return
	}
	var req struct {
		TotalBytes int64  `json:"total_bytes"`
		Chunks     int    `json:"chunks"`
		SHA256     string `json:"sha256"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		deny(w, http.StatusBadRequest, "bridge.bad_json")
		return
	}
	if req.TotalBytes <= 0 || req.Chunks <= 0 || len(req.SHA256) != 64 || !isHex(req.SHA256) {
		deny(w, http.StatusBadRequest, "bridge.bad_request")
		return
	}
	if req.TotalBytes > s.cfg.MaxUpload || req.Chunks > s.cfg.MaxChunks || req.TotalBytes > int64(req.Chunks)*s.cfg.MaxChunk {
		deny(w, http.StatusRequestEntityTooLarge, "bridge.payload_too_large")
		return
	}
	u := s.uploads
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sweep()
	if len(u.m) >= u.cfg.MaxUploads {
		deny(w, http.StatusTooManyRequests, "bridge.too_many_uploads")
		return
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		deny(w, http.StatusInternalServerError, "bridge.internal")
		return
	}
	id := hex.EncodeToString(raw)
	u.m[id] = &upload{total: req.TotalBytes, chunks: req.Chunks, sha256: strings.ToLower(req.SHA256), received: map[int][]byte{}, touched: u.cfg.Now()}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func (s *Server) uploadRoute(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || len(parts[0]) != 32 || !isHex(parts[0]) {
		deny(w, http.StatusNotFound, "bridge.not_found")
		return
	}
	if r.Method != http.MethodPost {
		deny(w, http.StatusMethodNotAllowed, "bridge.method_not_allowed")
		return
	}
	switch parts[1] {
	case "chunk":
		s.uploadChunk(w, r, parts[0])
	case "finish":
		s.uploadFinish(w, r, parts[0])
	default:
		deny(w, http.StatusNotFound, "bridge.not_found")
	}
}

func (s *Server) uploadChunk(w http.ResponseWriter, r *http.Request, id string) {
	seq, err := strconv.Atoi(r.URL.Query().Get("seq"))
	if err != nil || seq < 0 {
		deny(w, http.StatusBadRequest, "bridge.bad_request")
		return
	}
	body, status, code := s.readBody(w, r, s.cfg.MaxChunk)
	if code != "" {
		deny(w, status, code)
		return
	}
	u := s.uploads
	u.mu.Lock()
	defer u.mu.Unlock()
	u.sweep()
	up, ok := u.m[id]
	if !ok {
		deny(w, http.StatusNotFound, "bridge.unknown_upload")
		return
	}
	if seq >= up.chunks {
		deny(w, http.StatusBadRequest, "bridge.bad_request")
		return
	}
	if _, dup := up.received[seq]; dup {
		deny(w, http.StatusConflict, "bridge.duplicate_chunk")
		return
	}
	if up.size+int64(len(body)) > up.total {
		delete(u.m, id)
		deny(w, http.StatusRequestEntityTooLarge, "bridge.payload_too_large")
		return
	}
	up.received[seq] = body
	up.size += int64(len(body))
	up.touched = u.cfg.Now()
	writeJSON(w, http.StatusOK, map[string]any{"received": len(up.received)})
}

func (s *Server) uploadFinish(w http.ResponseWriter, r *http.Request, id string) {
	u := s.uploads
	u.mu.Lock()
	u.sweep()
	up, ok := u.m[id]
	if !ok {
		u.mu.Unlock()
		deny(w, http.StatusNotFound, "bridge.unknown_upload")
		return
	}
	if len(up.received) != up.chunks || up.size != up.total {
		u.mu.Unlock()
		deny(w, http.StatusConflict, "bridge.upload_incomplete")
		return
	}
	delete(u.m, id)
	u.mu.Unlock()
	assembled := make([]byte, 0, up.total)
	for i := 0; i < up.chunks; i++ {
		assembled = append(assembled, up.received[i]...)
	}
	if canon.SHA256Hex(assembled) != up.sha256 {
		deny(w, http.StatusUnprocessableEntity, "bridge.digest_mismatch")
		return
	}
	s.verify(w, assembled)
}
