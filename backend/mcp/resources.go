package mcp

import (
	"encoding/json"
	"regexp"

	"github.com/insightlabs38-pixel/vsf/backend/api"
)

const (
	opURIPrefix  = "vsf://operation/"
	invURIPrefix = "vsf://invocation/"
)

var (
	opURI  = regexp.MustCompile(`^vsf://operation/([a-z0-9_.\-]{1,128})/([0-9]{1,9}\.[0-9]{1,9}\.[0-9]{1,9})$`)
	invURI = regexp.MustCompile(`^vsf://invocation/(op_[0-9a-f]{32}|[A-Za-z0-9_\-]{1,64})$`)
)

func operationURI(name, version string) string { return opURIPrefix + name + "/" + version }

func (s *Server) resourcesList(p api.Principal, pv paramsView) (map[string]any, *rpcError) {
	if e := unusedParams(pv, "cursor"); e != nil {
		return nil, e
	}
	cursor, e := cursorOf(pv)
	if e != nil {
		return nil, e
	}
	page, aerr := s.cfg.Backend.List(p, api.ListQuery{Cursor: cursor})
	if aerr != nil {
		return nil, apiProtocolError(aerr)
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, sp := range page.Items {
		items = append(items, map[string]any{
			"uri": operationURI(sp.Name, sp.ContractVersion), "name": sp.Name, "title": sp.Title, "description": sp.Description,
			"mimeType": "application/json",
		})
	}
	res := complete(map[string]any{"resources": items, "ttlMs": s.cfg.ListTTLMs, "cacheScope": "private"})
	if page.NextCursor != "" {
		res["nextCursor"] = page.NextCursor
	}
	return res, nil
}

func (s *Server) resourceTemplates(pv paramsView) (map[string]any, *rpcError) {
	if e := unusedParams(pv, "cursor"); e != nil {
		return nil, e
	}
	if _, e := cursorOf(pv); e != nil {
		return nil, e
	}
	return complete(map[string]any{"resourceTemplates": []map[string]any{
		{"uriTemplate": opURIPrefix + "{name}/{version}", "name": "operation", "title": "Operation contract",
			"description": "The schema, effect disclosure and contract identity of one operation at an exact contract version.", "mimeType": "application/json"},
		{"uriTemplate": invURIPrefix + "{key}", "name": "invocation", "title": "Invocation outcome",
			"description": "The current outcome of one of your invocations.", "mimeType": "application/json"},
	}, "ttlMs": 3600000, "cacheScope": "public"}), nil
}

func notFound(uri string) *rpcError {
	if len(uri) > 256 {
		uri = uri[:256]
	}
	return &rpcError{Code: codeInvalidParams, Message: "Resource not found", Data: map[string]any{"uri": uri}}
}

func (s *Server) resourcesRead(p api.Principal, pv paramsView) (map[string]any, *rpcError) {
	if e := unusedParams(pv, "uri"); e != nil {
		return nil, e
	}
	var uri string
	if json.Unmarshal(pv.fields["uri"], &uri) != nil || uri == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "a resource uri is required"}
	}
	if m := opURI.FindStringSubmatch(uri); m != nil {
		spec, aerr := s.cfg.Backend.Describe(p, m[1], m[2])
		if aerr != nil {
			if aerr.Code == api.CodeUnauthenticated || aerr.Code == api.CodeRateLimited {
				return nil, apiProtocolError(aerr)
			}
			return nil, notFound(uri)
		}
		return s.readContents(uri, spec, s.cfg.ListTTLMs), nil
	}
	if m := invURI.FindStringSubmatch(uri); m != nil {
		res := s.cfg.Backend.Status(p, m[1], true)
		if res.Error != nil {
			if res.Error.Code == api.CodeUnauthenticated || res.Error.Code == api.CodeRateLimited {
				return nil, apiProtocolError(res.Error)
			}
			return nil, notFound(uri)
		}
		return s.readContents(uri, res, 0), nil
	}
	return nil, notFound(uri)
}

func (s *Server) readContents(uri string, v any, ttl int64) map[string]any {
	b, _ := json.Marshal(v)
	return complete(map[string]any{
		"contents": []map[string]any{{"uri": uri, "mimeType": "application/json", "text": string(b)}},
		"ttlMs":    ttl, "cacheScope": "private",
	})
}
