package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
)

// Remote is the service as a client sees it; Client implements it over REST.
type Remote interface {
	Discover(ctx context.Context) (Discovery, *Error)
	Namespaces(ctx context.Context) ([]Namespace, *Error)
	List(ctx context.Context, q ListQuery) (Page, *Error)
	Describe(ctx context.Context, name, version string) (*Spec, *Error)
	Versions(ctx context.Context, name string) ([]VersionInfo, *Error)
	Invoke(ctx context.Context, route Route, body []byte) Result
	Status(ctx context.Context, key string) Result
	Cancel(ctx context.Context, key string) Result
	Reconcile(ctx context.Context, key string, body []byte) Result
}

// Client speaks the REST interface with one credential for one surface.
type Client struct {
	base    *url.URL
	token   string
	surface Surface
	http    *http.Client
}

const maxResponseBytes = 8 << 20

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NewClient refuses to send a credential over plain http to anything but this machine.
func NewClient(base, token string, surface Surface, hc *http.Client) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil {
		return nil, errors.New("invalid service url")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, errors.New("the service url must be https, or http on loopback")
	}
	if token == "" {
		return nil, errors.New("a credential is required")
	}
	if surface != SurfaceREST && surface != SurfaceCLI {
		return nil, errors.New("the rest transport serves the rest and cli surfaces")
	}
	c := http.Client{}
	if hc != nil {
		c = *hc
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: u, token: token, surface: surface, http: &c}, nil
}

func pathSafe(parts ...string) bool {
	for _, p := range parts {
		if p == "" || len(p) > MaxNameChars+16 {
			return false
		}
		for _, r := range p {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

func errBadSegment() *Error { return errWith(CodeInvalidRequest, "bad_identifier", nil) }

type exchange struct {
	status int
	body   []byte
	header http.Header
}

func (c *Client) send(ctx context.Context, method, path string, query url.Values, body []byte) (exchange, *Error) {
	u := *c.base
	u.Path = path
	u.RawPath = ""
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return exchange{}, newError(CodeInvalidRequest, "bad_url")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if c.surface != SurfaceREST {
		req.Header.Set(ClientHeader, string(c.surface))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return exchange{}, newError(CodeServiceUnreachable, "")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(b) > maxResponseBytes {
		return exchange{}, newError(CodeServiceUnreachable, "unreadable_response")
	}
	return exchange{resp.StatusCode, b, resp.Header}, nil
}

func envelopeError(x exchange) *Error {
	var env struct {
		Error *Error `json:"error"`
	}
	if json.Unmarshal(x.body, &env) == nil && env.Error != nil && env.Error.Code != "" {
		return env.Error
	}
	var res Result
	if json.Unmarshal(x.body, &res) == nil && res.Error != nil {
		return res.Error
	}
	return errWith(CodeInternal, "unreadable_error_response", nil)
}

func decode[T any](x exchange, xerr *Error) (T, *Error) {
	var v T
	if xerr != nil {
		return v, xerr
	}
	if x.status < 200 || x.status > 299 {
		return v, envelopeError(x)
	}
	if err := json.Unmarshal(x.body, &v); err != nil {
		return v, errWith(CodeInternal, "unreadable_response", nil)
	}
	return v, nil
}

func resultOf(x exchange, xerr *Error, mayHaveReachedService bool) Result {
	if xerr != nil {
		r := RefusedResult(nil, xerr)
		if xerr.Code == CodeServiceUnreachable && mayHaveReachedService {
			r.Category, r.Effect, r.Retry, r.State = CategoryFailed, EffectUnknown, RetrySameRequest, "unreachable"
		}
		return r
	}
	var res Result
	if err := json.Unmarshal(x.body, &res); err != nil || (res.Category == "" && res.Error == nil) {
		if e := envelopeError(x); e.Code != CodeInternal || x.status >= 400 {
			return RefusedResult(nil, e)
		}
		return RefusedResult(nil, errWith(CodeInternal, "unreadable_response", nil))
	}
	return res
}

func (c *Client) Discover(ctx context.Context) (Discovery, *Error) {
	x, e := c.send(ctx, "GET", "/v1/discover", nil, nil)
	return decode[Discovery](x, e)
}

func (c *Client) Namespaces(ctx context.Context) ([]Namespace, *Error) {
	x, e := c.send(ctx, "GET", "/v1/namespaces", nil, nil)
	v, err := decode[struct {
		Namespaces []Namespace `json:"namespaces"`
	}](x, e)
	return v.Namespaces, err
}

func (c *Client) List(ctx context.Context, q ListQuery) (Page, *Error) {
	v := url.Values{}
	if q.Namespace != "" {
		v.Set("namespace", q.Namespace)
	}
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	x, e := c.send(ctx, "GET", "/v1/operations", v, nil)
	return decode[Page](x, e)
}

func (c *Client) Describe(ctx context.Context, name, version string) (*Spec, *Error) {
	if !pathSafe(name) {
		return nil, errBadSegment()
	}
	v := url.Values{}
	if version != "" {
		v.Set("version", version)
	}
	x, e := c.send(ctx, "GET", "/v1/operations/"+name, v, nil)
	s, err := decode[Spec](x, e)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) Versions(ctx context.Context, name string) ([]VersionInfo, *Error) {
	if !pathSafe(name) {
		return nil, errBadSegment()
	}
	x, e := c.send(ctx, "GET", "/v1/operations/"+name+"/versions", nil, nil)
	v, err := decode[struct {
		Versions []VersionInfo `json:"versions"`
	}](x, e)
	return v.Versions, err
}

func (c *Client) Invoke(ctx context.Context, route Route, body []byte) Result {
	if !pathSafe(route.Operation, route.Version) {
		return RefusedResult(nil, errBadSegment())
	}
	x, e := c.send(ctx, "POST", "/v1/operations/"+route.Operation+"/versions/"+route.Version+"/invocations", nil, body)
	return resultOf(x, e, true)
}

func (c *Client) Status(ctx context.Context, key string) Result {
	if !pathSafe(key) {
		return RefusedResult(nil, errBadSegment())
	}
	x, e := c.send(ctx, "GET", "/v1/invocations/"+key, url.Values{"progress": {"1"}}, nil)
	return resultOf(x, e, false)
}

func (c *Client) Cancel(ctx context.Context, key string) Result {
	if !pathSafe(key) {
		return RefusedResult(nil, errBadSegment())
	}
	x, e := c.send(ctx, "POST", "/v1/invocations/"+key+"/cancel", nil, []byte("{}"))
	return resultOf(x, e, true)
}

func (c *Client) Reconcile(ctx context.Context, key string, body []byte) Result {
	if !pathSafe(key) {
		return RefusedResult(nil, errBadSegment())
	}
	x, e := c.send(ctx, "POST", "/v1/invocations/"+key+"/reconcile", nil, body)
	return resultOf(x, e, true)
}

var _ Remote = (*Client)(nil)
