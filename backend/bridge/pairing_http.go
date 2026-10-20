package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
)

func decodeStrict(body []byte, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	return dec.Decode(v) == nil && !dec.More()
}

func pairError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		deny(w, http.StatusTooManyRequests, "pair.rate_limited")
	case errors.Is(err, ErrRefreshReused):
		deny(w, http.StatusUnauthorized, "pair.refresh_reused")
	case errors.Is(err, ErrExpired):
		deny(w, http.StatusUnauthorized, "pair.expired")
	case errors.Is(err, ErrDenied):
		deny(w, http.StatusForbidden, "pair.denied")
	default:
		deny(w, http.StatusInternalServerError, "pair.internal")
	}
}

// pairRoute serves the three unauthenticated pairing calls; it reports whether the path was one of them.
func (s *Server) pairRoute(w http.ResponseWriter, r *http.Request, origin string) bool {
	switch r.URL.Path {
	case "/v1/pair/begin", "/v1/pair/finish", "/v1/token/refresh":
	default:
		return false
	}
	if r.Method != http.MethodPost {
		deny(w, http.StatusMethodNotAllowed, "bridge.method_not_allowed")
		return true
	}
	if origin == "" || !s.cfg.Pairing.origins[origin] {
		deny(w, http.StatusForbidden, "pair.denied")
		return true
	}
	body, status, code := s.readBody(w, r, 4<<10)
	if code != "" {
		deny(w, status, code)
		return true
	}
	switch r.URL.Path {
	case "/v1/pair/begin":
		var req struct {
			Nonce string `json:"nonce"`
		}
		if !decodeStrict(body, &req) {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return true
		}
		nonce, err := b64.DecodeString(req.Nonce)
		if err != nil {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return true
		}
		reply, err := s.cfg.Pairing.Begin(origin, nonce)
		if err != nil {
			pairError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, reply)
	case "/v1/pair/finish":
		var req struct {
			OfferID string `json:"offer_id"`
			Proof   string `json:"proof"`
			Name    string `json:"name"`
		}
		if !decodeStrict(body, &req) {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return true
		}
		proof, err := b64.DecodeString(req.Proof)
		if err != nil {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return true
		}
		creds, err := s.cfg.Pairing.Finish(origin, req.OfferID, req.Name, proof)
		if err != nil {
			pairError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, creds)
	case "/v1/token/refresh":
		var req struct {
			ClientID string `json:"client_id"`
			Refresh  string `json:"refresh"`
		}
		if !decodeStrict(body, &req) {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return true
		}
		refresh, err := b64.DecodeString(req.Refresh)
		if err != nil {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return true
		}
		creds, err := s.cfg.Pairing.Refresh(origin, req.ClientID, refresh)
		if err != nil {
			pairError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, creds)
	}
	return true
}
