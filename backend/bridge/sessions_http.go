package bridge

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func bindingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBindingMissing):
		deny(w, http.StatusNotFound, "session.unknown")
	case errors.Is(err, ErrBindingStale):
		deny(w, http.StatusConflict, "session.stale")
	case errors.Is(err, ErrBindingDenied):
		deny(w, http.StatusForbidden, "session.denied")
	default:
		deny(w, http.StatusBadGateway, "session.validator_unavailable")
	}
}

func (s *Server) sessionRoute(w http.ResponseWriter, r *http.Request, client string) {
	if s.cfg.Bindings == nil {
		deny(w, http.StatusNotFound, "bridge.not_found")
		return
	}
	if client == "" {
		deny(w, http.StatusForbidden, "session.client_required")
		return
	}
	path := r.URL.Path
	switch {
	case path == "/v1/session" && r.Method == http.MethodPost:
		body, status, code := s.readBody(w, r, 256<<10)
		if code != "" {
			deny(w, status, code)
			return
		}
		var req struct {
			Origin   string          `json:"origin"`
			Document string          `json:"document"`
			Manifest json.RawMessage `json:"manifest"`
		}
		if !decodeStrict(body, &req) || len(req.Manifest) == 0 {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return
		}
		bd, err := s.cfg.Bindings.Register(client, req.Origin, req.Document, req.Manifest)
		if err != nil {
			bindingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, bd)
	case path == "/v1/session/renew" && r.Method == http.MethodPost:
		body, status, code := s.readBody(w, r, 4<<10)
		if code != "" {
			deny(w, status, code)
			return
		}
		var req struct {
			ID string `json:"session_id"`
		}
		if !decodeStrict(body, &req) {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return
		}
		bd, err := s.cfg.Bindings.Renew(req.ID, client)
		if err != nil {
			bindingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, bd)
	case path == "/v1/session/end" && r.Method == http.MethodPost:
		body, status, code := s.readBody(w, r, 4<<10)
		if code != "" {
			deny(w, status, code)
			return
		}
		var req struct {
			ID string `json:"session_id"`
		}
		if !decodeStrict(body, &req) {
			deny(w, http.StatusBadRequest, "bridge.bad_json")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ended": s.cfg.Bindings.End(req.ID, client)})
	case strings.HasPrefix(path, "/v1/session/") && r.Method == http.MethodGet:
		bd, err := s.cfg.Bindings.Get(strings.TrimPrefix(path, "/v1/session/"), client)
		if err != nil {
			bindingError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, bd)
	default:
		deny(w, http.StatusMethodNotAllowed, "bridge.method_not_allowed")
	}
}
