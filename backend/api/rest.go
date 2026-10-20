package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// RESTConfig carries what the REST adapter needs besides the backend.
type RESTConfig struct {
	Backend Backend
	Auth    Authenticator
	// AllowedOrigins lists browser origins that may call the API; a request with any other Origin is refused.
	AllowedOrigins []string
	// AllowedHosts, when set, must contain the request Host (guards against DNS rebinding on localhost).
	AllowedHosts []string
	OpenAPI      func(p Principal, namespace string) (map[string]any, *Error)
}

type rest struct{ cfg RESTConfig }

// NewREST returns the REST adapter; it contains no operation logic, only translation to Backend.
func NewREST(cfg RESTConfig) http.Handler {
	r := &rest{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/discover", r.guard(false, r.discover))
	mux.HandleFunc("GET /v1/namespaces", r.guard(true, r.namespaces))
	mux.HandleFunc("GET /v1/operations", r.guard(true, r.list))
	mux.HandleFunc("GET /v1/operations/{name}", r.guard(true, r.describe))
	mux.HandleFunc("GET /v1/operations/{name}/versions", r.guard(true, r.versions))
	mux.HandleFunc("POST /v1/operations/{name}/versions/{version}/invocations", r.guard(true, r.invoke))
	mux.HandleFunc("POST /v1/operations/{name}/versions/{version}/confirmations", r.guard(true, r.confirm))
	mux.HandleFunc("GET /v1/invocations/{key}", r.guard(true, r.status))
	mux.HandleFunc("POST /v1/invocations/{key}/cancel", r.guard(true, r.cancel))
	mux.HandleFunc("POST /v1/invocations/{key}/reconcile", r.guard(true, r.reconcile))
	mux.HandleFunc("GET /v1/openapi.json", r.guard(true, r.openapi))
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		writeError(w, http.StatusNotFound, errWith(CodeInvalidRequest, "no_such_route", nil))
	})
	return mux
}

type handler func(w http.ResponseWriter, req *http.Request, p Principal)

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (r *rest) guard(auth bool, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		hd := w.Header()
		hd.Set("Content-Type", "application/json")
		hd.Set("Cache-Control", "no-store")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-VSF-API-Version", APIVersion)
		if len(r.cfg.AllowedHosts) > 0 && !contains(r.cfg.AllowedHosts, req.Host) {
			writeError(w, http.StatusForbidden, errWith(CodePermissionDenied, "host_not_allowed", nil))
			return
		}
		if o := req.Header.Get("Origin"); o != "" && !contains(r.cfg.AllowedOrigins, o) {
			writeError(w, http.StatusForbidden, errWith(CodePermissionDenied, "origin_not_allowed", nil))
			return
		}
		var p Principal
		if auth {
			tok, ok := bearer(req)
			if !ok {
				hd.Set("WWW-Authenticate", `Bearer realm="vsf"`)
				writeError(w, 0, newError(CodeUnauthenticated, ""))
				return
			}
			surface, serr := surfaceOf(req)
			if serr != nil {
				writeError(w, 0, serr)
				return
			}
			var err *Error
			if p, err = r.cfg.Auth.Authenticate(surface, tok); err != nil {
				if err.Code == CodeUnauthenticated {
					hd.Set("WWW-Authenticate", `Bearer realm="vsf"`)
				}
				writeError(w, 0, err)
				return
			}
		}
		h(w, req, p)
	}
}

// ClientHeader lets the CLI present its own credential over the same transport; the credential decides what is accepted.
const ClientHeader = "X-VSF-Client"

func surfaceOf(req *http.Request) (Surface, *Error) {
	switch v := req.Header.Get(ClientHeader); v {
	case "", string(SurfaceREST):
		return SurfaceREST, nil
	case string(SurfaceCLI):
		return SurfaceCLI, nil
	case string(SurfaceUI):
		return SurfaceUI, nil
	default:
		return "", errWith(CodeInvalidRequest, "unknown_client", nil)
	}
}

func bearer(req *http.Request) (string, bool) {
	v := req.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(v) <= len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(v[len(prefix):])
	return tok, tok != "" && len(tok) <= 512
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		status, b = http.StatusInternalServerError, []byte(`{"error":{"code":"internal"}}`)
	}
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

// writeError renders an Error; status 0 takes the status the taxonomy fixes for its code.
func writeError(w http.ResponseWriter, status int, e *Error) {
	if status == 0 {
		status = InfoOf(e.Code).HTTP
	}
	if e.RetryAfterMs > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt((e.RetryAfterMs+999)/1000, 10))
	}
	writeJSON(w, status, map[string]any{"error": e})
}

// StatusOf is the HTTP status of a Result; the taxonomy, not the handler, decides it.
func StatusOf(res Result) int {
	if res.Error != nil {
		return InfoOf(res.Error.Code).HTTP
	}
	if res.Category == CategoryInProgress {
		return http.StatusAccepted
	}
	return http.StatusOK
}

func writeResult(w http.ResponseWriter, res Result) {
	if res.Error != nil && res.Error.RetryAfterMs > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt((res.Error.RetryAfterMs+999)/1000, 10))
	}
	if res.Invocation != "" {
		w.Header().Set("Location", "/v1/invocations/"+url.PathEscape(res.Invocation))
	}
	writeJSON(w, StatusOf(res), res)
}

// discover is unauthenticated metadata about the interface, so it never says how much the catalog holds.
func (r *rest) discover(w http.ResponseWriter, _ *http.Request, _ Principal) {
	d := r.cfg.Backend.Discovery()
	d.Operations = 0
	writeJSON(w, http.StatusOK, d)
}

func (r *rest) namespaces(w http.ResponseWriter, _ *http.Request, p Principal) {
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": r.cfg.Backend.Namespaces(p)})
}

func (r *rest) list(w http.ResponseWriter, req *http.Request, p Principal) {
	q := req.URL.Query()
	lq := ListQuery{Namespace: q.Get("namespace"), Cursor: q.Get("cursor")}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, 0, errWith(CodeInvalidRequest, "bad_limit", nil))
			return
		}
		lq.Limit = n
	}
	page, err := r.cfg.Backend.List(p, lq)
	if err != nil {
		writeError(w, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (r *rest) describe(w http.ResponseWriter, req *http.Request, p Principal) {
	spec, err := r.cfg.Backend.Describe(p, req.PathValue("name"), req.URL.Query().Get("version"))
	if err != nil {
		writeError(w, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, spec)
}

func (r *rest) versions(w http.ResponseWriter, req *http.Request, p Principal) {
	v, err := r.cfg.Backend.Versions(p, req.PathValue("name"))
	if err != nil {
		writeError(w, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": v})
}

func readBody(w http.ResponseWriter, req *http.Request) ([]byte, *Error) {
	mt, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return nil, errWith(CodeInvalidRequest, "content_type_must_be_json", nil)
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, req.Body, MaxRequestBytes))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, newError(CodeTooLarge, "")
		}
		return nil, errWith(CodeInvalidRequest, "unreadable_body", nil)
	}
	return b, nil
}

func (r *rest) invoke(w http.ResponseWriter, req *http.Request, p Principal) {
	body, err := readBody(w, req)
	if err != nil {
		writeError(w, 0, err)
		return
	}
	writeResult(w, r.cfg.Backend.Invoke(req.Context(), p, body, Route{Operation: req.PathValue("name"), Version: req.PathValue("version")}))
}

func (r *rest) status(w http.ResponseWriter, req *http.Request, p Principal) {
	writeResult(w, r.cfg.Backend.Status(p, req.PathValue("key"), req.URL.Query().Get("progress") == "1"))
}

func (r *rest) cancel(w http.ResponseWriter, req *http.Request, p Principal) {
	writeResult(w, r.cfg.Backend.Cancel(p, req.PathValue("key")))
}

func (r *rest) reconcile(w http.ResponseWriter, req *http.Request, p Principal) {
	body, err := readBody(w, req)
	if err != nil {
		writeError(w, 0, err)
		return
	}
	writeResult(w, r.cfg.Backend.Reconcile(req.Context(), p, req.PathValue("key"), body))
}

func (r *rest) openapi(w http.ResponseWriter, req *http.Request, p Principal) {
	if r.cfg.OpenAPI == nil {
		writeError(w, http.StatusNotFound, errWith(CodeInvalidRequest, "no_such_route", nil))
		return
	}
	doc, err := r.cfg.OpenAPI(p, req.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (r *rest) confirm(w http.ResponseWriter, req *http.Request, p Principal) {
	body, err := readBody(w, req)
	if err != nil {
		writeError(w, 0, err)
		return
	}
	c, cerr := r.cfg.Backend.Confirm(req.Context(), p, body, Route{Operation: req.PathValue("name"), Version: req.PathValue("version"), Delegate: req.URL.Query().Get("for")})
	if cerr != nil {
		writeError(w, 0, cerr)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
