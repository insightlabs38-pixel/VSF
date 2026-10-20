package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/api"
)

// Config wires the adapter; it holds no operation logic of its own.
type Config struct {
	Backend        api.Backend
	Auth           api.Authenticator
	AllowedOrigins []string
	ServerName     string
	ServerVersion  string
	// ListTTLMs is how long clients may cache catalog lists; the lists vary by caller, so they are private.
	ListTTLMs int64
	// MaxListedOperations bounds how many operations tools/list advertises; every served operation stays callable by name
	// and discoverable through vsf.operation.list. Zero lists them all.
	MaxListedOperations int
}

// Server is the Streamable HTTP endpoint; every request stands alone and nothing is remembered between them.
type Server struct{ cfg Config }

func New(cfg Config) *Server {
	if cfg.ServerName == "" {
		cfg.ServerName = "vsf"
	}
	if cfg.ServerVersion == "" {
		cfg.ServerVersion = "0.1.0"
	}
	if cfg.ListTTLMs == 0 {
		cfg.ListTTLMs = 60000
	}
	return &Server{cfg: cfg}
}

const instructions = "Each tool is one verified operation of a source application. A write is reported succeeded only after the source's own " +
	"read-back confirms it. If the outcome is unknown_commit or unverified_effect the change may or may not have been applied: do not repeat it; " +
	"call vsf.invocation.reconcile. If a call returns in_progress, poll vsf.invocation.status. Reuse a request_id only to repeat the very same request."

func (s *Server) serverInfo() map[string]any {
	return map[string]any{"name": s.cfg.ServerName, "version": s.cfg.ServerVersion}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func writeRPC(w http.ResponseWriter, status int, resp rpcResponse) {
	resp.JSONRPC = "2.0"
	b, err := json.Marshal(resp)
	if err != nil {
		status, b = http.StatusInternalServerError, []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"internal"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func fail(w http.ResponseWriter, status int, id json.RawMessage, code int, msg string, data any) {
	writeRPC(w, status, rpcResponse{ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}})
}

func supported() []string { return []string{Version} }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); o != "" && !contains(s.cfg.AllowedOrigins, o) {
		fail(w, http.StatusForbidden, nil, codeForbidden, "origin not allowed", nil)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(w, http.StatusMethodNotAllowed, nil, codeInvalidRequest, "this endpoint accepts POST only", nil)
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, nil, codeInvalidRequest, "content type must be application/json", nil)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, api.MaxRequestBytes+(64<<10)))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			fail(w, http.StatusRequestEntityTooLarge, nil, codeInvalidRequest, "request too large", nil)
			return
		}
		fail(w, http.StatusBadRequest, nil, codeParse, "unreadable body", nil)
		return
	}
	req, perr := parseRequest(raw)
	if perr != nil {
		fail(w, http.StatusBadRequest, nil, perr.Code, perr.Message, nil)
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	params, herr := s.validateEnvelope(r, req)
	if herr != nil {
		fail(w, http.StatusBadRequest, req.ID, herr.Code, herr.Message, herr.Data)
		return
	}
	s.dispatch(w, r, req, params)
}

func parseRequest(raw []byte) (rpcRequest, *rpcError) {
	v, err := api.DecodeStrict(raw)
	if err != nil {
		return rpcRequest{}, &rpcError{Code: codeParse, Message: "parse error"}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return rpcRequest{}, &rpcError{Code: codeInvalidRequest, Message: "a request must be one json object"}
	}
	var req rpcRequest
	if json.Unmarshal(raw, &req) != nil || req.JSONRPC != "2.0" || req.Method == "" {
		return rpcRequest{}, &rpcError{Code: codeInvalidRequest, Message: "not a json-rpc 2.0 request"}
	}
	if _, has := obj["id"]; has && !validID(req.ID) {
		return rpcRequest{}, &rpcError{Code: codeInvalidRequest, Message: "the request id must be a string or an integer"}
	}
	if _, isNotification := obj["id"]; !isNotification {
		req.ID = nil
	}
	return req, nil
}

type paramsView struct {
	meta   map[string]any
	fields map[string]json.RawMessage
}

// validateEnvelope enforces the per-request metadata and the headers that mirror it, before anything is dispatched.
func (s *Server) validateEnvelope(r *http.Request, req rpcRequest) (paramsView, *rpcError) {
	var pv paramsView
	hv := r.Header.Get(headerVersion)
	if req.Method == "initialize" {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		return pv, &rpcError{Code: codeUnsupportedVer, Message: "This server is stateless and has no initialize handshake; send " + Version + " requests with per-request _meta.",
			Data: map[string]any{"supported": supported(), "requested": p.ProtocolVersion}}
	}
	if hv == "" {
		return pv, &rpcError{Code: codeHeaderMismatch, Message: "missing " + headerVersion + " header"}
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &pv.fields); err != nil {
			return pv, &rpcError{Code: codeInvalidParams, Message: "params must be an object"}
		}
	}
	if m, ok := pv.fields["_meta"]; ok {
		if json.Unmarshal(m, &pv.meta) != nil {
			return pv, &rpcError{Code: codeInvalidParams, Message: "_meta must be an object"}
		}
	}
	ver, _ := pv.meta[metaVersion].(string)
	if ver == "" {
		return pv, &rpcError{Code: codeInvalidParams, Message: "missing required _meta field " + metaVersion}
	}
	if _, ok := pv.meta[metaCaps].(map[string]any); !ok {
		return pv, &rpcError{Code: codeInvalidParams, Message: "missing required _meta field " + metaCaps}
	}
	if hv != ver {
		return pv, &rpcError{Code: codeHeaderMismatch, Message: headerVersion + " does not match the _meta protocol version"}
	}
	if ver != Version {
		return pv, &rpcError{Code: codeUnsupportedVer, Message: "Unsupported protocol version", Data: map[string]any{"supported": supported(), "requested": ver}}
	}
	if m := r.Header.Get(headerMethod); m != req.Method {
		return pv, &rpcError{Code: codeHeaderMismatch, Message: headerMethod + " does not match the request method"}
	}
	want := ""
	switch req.Method {
	case "tools/call", "prompts/get":
		_ = json.Unmarshal(pv.fields["name"], &want)
	case "resources/read":
		_ = json.Unmarshal(pv.fields["uri"], &want)
	default:
		return pv, nil
	}
	got, ok := decodeHeaderValue(r.Header.Get(headerName))
	if !ok || got != want || want == "" {
		return pv, &rpcError{Code: codeHeaderMismatch, Message: headerName + " does not match the request body"}
	}
	return pv, nil
}

func bearer(r *http.Request) (string, bool) {
	v := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(v) <= len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(v[len(prefix):])
	return tok, tok != "" && len(tok) <= 512
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, id json.RawMessage) (api.Principal, bool) {
	tok, ok := bearer(r)
	var err *api.Error
	var p api.Principal
	if ok {
		p, err = s.cfg.Auth.Authenticate(api.SurfaceMCP, tok)
	} else {
		err = &api.Error{Code: api.CodeUnauthenticated}
	}
	if err == nil {
		return p, true
	}
	code, status := codeUnauthenticated, http.StatusUnauthorized
	if err.Code == api.CodeWrongSurface || err.Code == api.CodePermissionDenied {
		code, status = codeForbidden, http.StatusForbidden
	}
	if err.Code == api.CodeRateLimited {
		code, status = codeRateLimited, http.StatusTooManyRequests
	}
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="vsf"`)
	}
	fail(w, status, id, code, api.InfoOf(err.Code).Summary, map[string]any{"code": string(err.Code)})
	return api.Principal{}, false
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, req rpcRequest, pv paramsView) {
	if req.Method == "server/discover" {
		s.reply(w, req.ID, s.discover(), nil)
		return
	}
	switch req.Method {
	case "tools/list", "tools/call", "resources/list", "resources/templates/list", "resources/read":
	default:
		fail(w, http.StatusNotFound, req.ID, codeMethodNotFound, "method not found", nil)
		return
	}
	p, ok := s.authenticate(w, r, req.ID)
	if !ok {
		return
	}
	ctx := r.Context()
	var res map[string]any
	var rerr *rpcError
	switch req.Method {
	case "tools/list":
		res, rerr = s.toolsList(p, pv)
	case "tools/call":
		res, rerr = s.toolsCall(ctx, p, pv)
	case "resources/list":
		res, rerr = s.resourcesList(p, pv)
	case "resources/templates/list":
		res, rerr = s.resourceTemplates(pv)
	case "resources/read":
		res, rerr = s.resourcesRead(p, pv)
	}
	s.reply(w, req.ID, res, rerr)
}

func (s *Server) reply(w http.ResponseWriter, id json.RawMessage, res map[string]any, rerr *rpcError) {
	if rerr != nil {
		writeRPC(w, http.StatusOK, rpcResponse{ID: id, Error: rerr})
		return
	}
	res["_meta"] = map[string]any{metaServer: s.serverInfo()}
	writeRPC(w, http.StatusOK, rpcResponse{ID: id, Result: res})
}

func (s *Server) discover() map[string]any {
	return complete(map[string]any{
		"supportedVersions": supported(),
		"capabilities":      map[string]any{"tools": map[string]any{}, "resources": map[string]any{}},
		"instructions":      instructions,
		"ttlMs":             3600000, "cacheScope": "public",
	})
}

func cursorOf(pv paramsView) (string, *rpcError) {
	raw, ok := pv.fields["cursor"]
	if !ok {
		return "", nil
	}
	var c string
	if json.Unmarshal(raw, &c) != nil || len(c) > 400 {
		return "", &rpcError{Code: codeInvalidParams, Message: "invalid cursor"}
	}
	return c, nil
}

func unusedParams(pv paramsView, allowed ...string) *rpcError {
	for k := range pv.fields {
		if k != "_meta" && !contains(allowed, k) {
			return &rpcError{Code: codeInvalidParams, Message: "unexpected parameter " + k}
		}
	}
	return nil
}
