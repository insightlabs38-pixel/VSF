package bridge

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/pairing"
)

// unauthenticated reports whether the request is one that cannot carry a credential yet.
func (s *Server) unauthenticated(r *http.Request) bool {
	if s.cfg.Pairing == nil || r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/v1/pair/begin", "/v1/pair/finish", "/v1/pair/renew":
		return true
	}
	return false
}

func (s *Server) authenticate(r *http.Request, origin string) (*pairing.Principal, int, string) {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil, http.StatusUnauthorized, "bridge.unauthorized"
	}
	if s.cfg.Pairing == nil {
		if !constantEq(got, s.cfg.Token) {
			return nil, http.StatusUnauthorized, "bridge.unauthorized"
		}
		return nil, 0, ""
	}
	if origin == "" {
		return nil, http.StatusUnauthorized, "bridge.unauthorized"
	}
	p, err := s.cfg.Pairing.Authenticate(origin, got)
	switch pairing.Code(err) {
	case "":
		return &p, 0, ""
	case "auth.expired":
		return nil, http.StatusUnauthorized, "bridge.token_expired"
	case "auth.revoked":
		return nil, http.StatusUnauthorized, "bridge.revoked"
	default:
		return nil, http.StatusUnauthorized, "bridge.unauthorized"
	}
}

var pairStatus = map[string]int{
	"pairing.peer_not_allowed":  http.StatusForbidden,
	"pairing.peer_mismatch":     http.StatusForbidden,
	"pairing.bad_proof":         http.StatusForbidden,
	"pairing.attempts_exceeded": http.StatusForbidden,
	"pairing.bad_request":       http.StatusBadRequest,
	"pairing.rate_limited":      http.StatusTooManyRequests,
	"pairing.too_many_pending":  http.StatusTooManyRequests,
	"pairing.unknown":           http.StatusNotFound,
	"pairing.not_approved":      http.StatusConflict,
	"auth.invalid":              http.StatusUnauthorized,
	"auth.expired":              http.StatusUnauthorized,
	"auth.revoked":              http.StatusUnauthorized,
	"auth.peer_mismatch":        http.StatusForbidden,
	"auth.refresh_reused":       http.StatusUnauthorized,
	"pairing.already_approved":  http.StatusConflict,
}

func (s *Server) pairFail(w http.ResponseWriter, err error) {
	code := pairing.Code(err)
	status, ok := pairStatus[code]
	if !ok {
		deny(w, http.StatusInternalServerError, "bridge.internal")
		return
	}
	deny(w, status, "bridge."+code)
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request, name string, who *pairing.Principal) {
	if s.cfg.Pairing == nil {
		deny(w, http.StatusNotFound, "bridge.not_found")
		return
	}
	origin := r.Header.Get("Origin")
	if name == "whoami" {
		if who == nil || (r.Method != http.MethodGet && r.Method != http.MethodPost) {
			deny(w, http.StatusMethodNotAllowed, "bridge.method_not_allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"pairing_id": who.PairingID, "peer": who.Peer})
		return
	}
	if r.Method != http.MethodPost {
		deny(w, http.StatusMethodNotAllowed, "bridge.method_not_allowed")
		return
	}
	if origin == "" {
		deny(w, http.StatusForbidden, "bridge.origin_not_allowed")
		return
	}
	body, status, code := s.readBody(w, r, 4096)
	if code != "" {
		deny(w, status, code)
		return
	}
	var in struct {
		NonceC  string `json:"nonce_c"`
		PairID  string `json:"pair_id"`
		Proof   string `json:"proof"`
		Refresh string `json:"refresh"`
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil && name != "revoke" {
		deny(w, http.StatusBadRequest, "bridge.bad_request")
		return
	}
	switch name {
	case "begin":
		b, err := s.cfg.Pairing.Begin(origin, in.NonceC)
		if err != nil {
			s.pairFail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, b)
	case "finish":
		c, err := s.cfg.Pairing.Finish(in.PairID, origin, in.Proof)
		if err != nil {
			s.pairFail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	case "renew":
		c, err := s.cfg.Pairing.Renew(origin, in.Refresh)
		if err != nil {
			s.pairFail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	case "revoke":
		if who == nil {
			deny(w, http.StatusUnauthorized, "bridge.unauthorized")
			return
		}
		if err := s.cfg.Pairing.Revoke(who.PairingID); err != nil {
			s.pairFail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		deny(w, http.StatusNotFound, "bridge.not_found")
	}
}

func constantEq(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
