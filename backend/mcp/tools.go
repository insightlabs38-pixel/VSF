package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/insightlabs38-pixel/vsf/backend/api"
)

const (
	toolStatus    = "vsf.invocation.status"
	toolCancel    = "vsf.invocation.cancel"
	toolReconcile = "vsf.invocation.reconcile"
	toolVersions  = "vsf.operation.versions"
	toolNamespace = "vsf.operation.namespaces"
	toolList      = "vsf.operation.list"
	toolDescribe  = "vsf.operation.describe"
)

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		r := make([]any, len(required))
		for i, v := range required {
			r[i] = v
		}
		s["required"] = r
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 256, "description": desc}
}

var handleProp = strProp("The invocation handle returned by an earlier call.")

func resultOut() map[string]any {
	return api.ResultSchemaFor("", "", "", nil)
}

// metaTools are the fixed, namespaced tools every caller sees; the vsf. prefix cannot collide with the op. prefix.
func metaTools() []map[string]any {
	ann := func(readOnly, destructive, idem bool) map[string]any {
		return map[string]any{"readOnlyHint": readOnly, "destructiveHint": destructive, "idempotentHint": idem, "openWorldHint": true}
	}
	return []map[string]any{
		{"name": toolCancel, "title": "Cancel an invocation",
			"description": "Ask a running invocation to stop. The result says what that did to the source: cancelled_before_effect, effect_unknown, too_late or already_finished.",
			"inputSchema": obj(map[string]any{"invocation": handleProp}, "invocation"), "outputSchema": resultOut(), "annotations": ann(false, false, true)},
		{"name": toolReconcile, "title": "Reconcile an unknown outcome",
			"description": "Ask the source what happened to an invocation whose outcome is unknown. It settles only on evidence of how that invocation ended, never because the expected values happen to be visible.",
			"inputSchema": obj(map[string]any{"invocation": handleProp, "session_id": strProp("The source session to read through."),
				"expect": map[string]any{"type": "object", "minProperties": 1, "additionalProperties": map[string]any{"type": "string", "maxLength": api.MaxTextChars},
					"description": "The field values the original call expected."}}, "invocation", "session_id", "expect"),
			"outputSchema": resultOut(), "annotations": ann(false, false, false)},
		{"name": toolStatus, "title": "Invocation status",
			"description": "The current outcome of an invocation, including progress. Use it to follow an in_progress result.",
			"inputSchema": obj(map[string]any{"invocation": handleProp}, "invocation"), "outputSchema": resultOut(), "annotations": ann(true, false, true)},
		{"name": toolNamespace, "title": "Applications with operations",
			"description": "The applications that have operations, answered from the index alone. Use it first, then vsf.operation.list for one namespace.",
			"inputSchema": obj(map[string]any{}), "outputSchema": obj(map[string]any{"namespaces": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}, "namespaces"),
			"annotations": ann(true, false, true)},
		{"name": toolList, "title": "List operations",
			"description": "A bounded, deterministic page of operations, optionally for one namespace. Every listed operation can be called by name; describe it first to get its argument schema.",
			"inputSchema": obj(map[string]any{"namespace": strProp("Limit to one namespace."), "cursor": strProp("Opaque cursor from the previous page."),
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": api.MaxPageSize}}),
			"outputSchema": obj(map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "next_cursor": map[string]any{"type": "string"}}, "items"),
			"annotations":  ann(true, false, true)},
		{"name": toolDescribe, "title": "Describe an operation",
			"description":  "The argument schema, effect disclosure and contract identity of one operation, at the newest served contract version or an exact one.",
			"inputSchema":  obj(map[string]any{"name": strProp("The operation tool name."), "version": strProp("An exact contract version.")}, "name"),
			"outputSchema": obj(map[string]any{}), "annotations": ann(true, false, true)},
		{"name": toolVersions, "title": "Contract versions of an operation",
			"description":  "Every known contract version of an operation and whether it is served, revoked or withheld.",
			"inputSchema":  obj(map[string]any{"name": strProp("The operation tool name.")}, "name"),
			"outputSchema": obj(map[string]any{"versions": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}, "versions"),
			"annotations":  ann(true, false, true)},
	}
}

func init() {
	sort.SliceStable(metaToolList, func(i, j int) bool { return metaToolList[i]["name"].(string) < metaToolList[j]["name"].(string) })
}

var metaToolList = metaTools()

func opTool(s *api.Spec) map[string]any {
	return map[string]any{
		"name": s.Name, "title": s.Title, "description": s.Description,
		"inputSchema": s.InputSchema, "outputSchema": s.OutputSchema,
		"annotations": map[string]any{"readOnlyHint": s.Annotations.ReadOnly, "destructiveHint": s.Annotations.Destructive,
			"idempotentHint": s.Annotations.Idempotent, "openWorldHint": s.Annotations.OpenWorld},
	}
}

// listCursor wraps the catalog cursor with how many operations were already sent, so a bounded listing stays bounded across pages.
const listCursorPrefix = "t1."

func splitListCursor(c string) (sent int, inner string, ok bool) {
	if c == "" {
		return 0, "", true
	}
	rest, found := strings.CutPrefix(c, listCursorPrefix)
	if !found {
		return 0, "", false
	}
	n, in, found := strings.Cut(rest, ".")
	if !found {
		return 0, "", false
	}
	v, err := strconv.Atoi(n)
	if err != nil || v < 0 || v > 1<<20 {
		return 0, "", false
	}
	return v, in, true
}

func (s *Server) toolsList(p api.Principal, pv paramsView) (map[string]any, *rpcError) {
	if e := unusedParams(pv, "cursor"); e != nil {
		return nil, e
	}
	cursor, e := cursorOf(pv)
	if e != nil {
		return nil, e
	}
	sent, inner, ok := splitListCursor(cursor)
	if !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid cursor"}
	}
	limit := api.DefaultPageSize
	if max := s.cfg.MaxListedOperations; max > 0 && max-sent < limit {
		limit = max - sent
	}
	tools := []map[string]any{}
	if cursor == "" {
		tools = append(tools, metaToolList...)
	}
	var next string
	if limit > 0 {
		page, aerr := s.cfg.Backend.List(p, api.ListQuery{Cursor: inner, Limit: limit})
		if aerr != nil {
			return nil, apiProtocolError(aerr)
		}
		for _, sp := range page.Items {
			tools = append(tools, opTool(sp))
		}
		sent += len(page.Items)
		if page.NextCursor != "" && (s.cfg.MaxListedOperations == 0 || sent < s.cfg.MaxListedOperations) {
			next = listCursorPrefix + strconv.Itoa(sent) + "." + page.NextCursor
		}
	}
	res := complete(map[string]any{"tools": tools, "ttlMs": s.cfg.ListTTLMs, "cacheScope": "private"})
	if next != "" {
		res["nextCursor"] = next
	}
	return res, nil
}

// apiProtocolError reports a failure that is about the request itself, not about running an operation.
func apiProtocolError(e *api.Error) *rpcError {
	switch e.Code {
	case api.CodeInvalidRequest:
		return &rpcError{Code: codeInvalidParams, Message: e.Message, Data: map[string]any{"code": string(e.Code), "reason": e.Reason}}
	case api.CodeUnauthenticated, api.CodePermissionDenied, api.CodeWrongSurface:
		return &rpcError{Code: codeForbidden, Message: e.Message, Data: map[string]any{"code": string(e.Code)}}
	case api.CodeRateLimited:
		return &rpcError{Code: codeRateLimited, Message: e.Message, Data: map[string]any{"code": string(e.Code), "retry_after_ms": e.RetryAfterMs}}
	}
	return &rpcError{Code: codeInternal, Message: api.InfoOf(api.CodeInternal).Summary, Data: map[string]any{"code": string(api.CodeInternal)}}
}

func (s *Server) toolsCall(ctx context.Context, p api.Principal, pv paramsView) (map[string]any, *rpcError) {
	if e := unusedParams(pv, "name", "arguments"); e != nil {
		return nil, e
	}
	var name string
	if json.Unmarshal(pv.fields["name"], &name) != nil || name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "a tool name is required"}
	}
	args := pv.fields["arguments"]
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	switch name {
	case toolStatus, toolCancel, toolReconcile:
		return s.invocationTool(ctx, p, name, args)
	case toolVersions:
		return s.versionsTool(p, args)
	case toolNamespace, toolList, toolDescribe:
		return s.discoveryTool(p, name, args)
	}
	if !strings.HasPrefix(name, "op.") {
		return nil, unknownTool(name)
	}
	res := s.cfg.Backend.Invoke(ctx, p, args, api.Route{Operation: name})
	if res.Error != nil && res.Error.Code == api.CodeOperationNotFound {
		return nil, unknownTool(name)
	}
	if res.Error != nil && res.Category == api.CategoryRefused && (res.Error.Code == api.CodeUnauthenticated || res.Error.Code == api.CodeRateLimited) {
		return nil, apiProtocolError(res.Error)
	}
	return toolResult(res), nil
}

func unknownTool(name string) *rpcError {
	if len(name) > 128 {
		name = name[:128]
	}
	return &rpcError{Code: codeInvalidParams, Message: "Unknown tool", Data: map[string]any{"name": name}}
}

func decodeArgs(args json.RawMessage, allowed ...string) (map[string]any, *rpcError) {
	v, err := api.DecodeStrict(args)
	m, ok := v.(map[string]any)
	if err != nil || !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "arguments must be one json object"}
	}
	for k := range m {
		found := false
		for _, a := range allowed {
			found = found || a == k
		}
		if !found {
			return nil, &rpcError{Code: codeInvalidParams, Message: "unexpected argument " + k}
		}
	}
	return m, nil
}

func toolError(code api.Code, reason string) map[string]any {
	e := &api.Error{Code: code, Message: api.InfoOf(code).Summary, Reason: reason, Effect: api.InfoOf(code).Effect, Retry: api.InfoOf(code).Retry}
	return toolResult(api.RefusedResult(nil, e))
}

func (s *Server) invocationTool(ctx context.Context, p api.Principal, tool string, args json.RawMessage) (map[string]any, *rpcError) {
	allowed := []string{"invocation"}
	if tool == toolReconcile {
		allowed = append(allowed, "session_id", "expect")
	}
	m, rerr := decodeArgs(args, allowed...)
	if rerr != nil {
		return nil, rerr
	}
	key, _ := m["invocation"].(string)
	if key == "" {
		return toolError(api.CodeInvalidArgument, "invocation_required"), nil
	}
	var res api.Result
	switch tool {
	case toolStatus:
		res = s.cfg.Backend.Status(p, key, true)
	case toolCancel:
		res = s.cfg.Backend.Cancel(p, key)
	default:
		body, _ := json.Marshal(map[string]any{"session_id": m["session_id"], "expect": m["expect"]})
		res = s.cfg.Backend.Reconcile(ctx, p, key, body)
	}
	if res.Error != nil && (res.Error.Code == api.CodeUnauthenticated || res.Error.Code == api.CodeRateLimited) {
		return nil, apiProtocolError(res.Error)
	}
	return toolResult(res), nil
}

func (s *Server) versionsTool(p api.Principal, args json.RawMessage) (map[string]any, *rpcError) {
	m, rerr := decodeArgs(args, "name")
	if rerr != nil {
		return nil, rerr
	}
	name, _ := m["name"].(string)
	v, aerr := s.cfg.Backend.Versions(p, name)
	if aerr != nil {
		if aerr.Code == api.CodeUnauthenticated || aerr.Code == api.CodeRateLimited {
			return nil, apiProtocolError(aerr)
		}
		return toolFailure(aerr), nil
	}
	return structured(map[string]any{"versions": v}, false), nil
}

func toolFailure(e *api.Error) map[string]any { return toolResult(api.RefusedResult(nil, e)) }

func structured(v any, isError bool) map[string]any {
	b, _ := json.Marshal(v)
	return complete(map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(b)}},
		"structuredContent": json.RawMessage(b), "isError": isError,
	})
}

// toolResult reports an outcome; anything but success, a dry run or a run still going is an execution error the model can see.
func toolResult(r api.Result) map[string]any {
	isErr := r.Error != nil
	switch r.Category {
	case api.CategoryRefused, api.CategoryFailed, api.CategoryUnknownCommit, api.CategoryUnverifiedEffect:
		isErr = true
	}
	return structured(r, isErr)
}

// compact is what a browsing client needs to choose an operation; the schema comes from describe.
func compact(sp *api.Spec) map[string]any {
	return map[string]any{
		"name": sp.Name, "namespace": sp.Namespace, "operation_id": sp.OperationID, "contract_version": sp.ContractVersion,
		"effect_class": sp.Effect.EffectClass, "write": sp.Effect.Write, "title": sp.Title,
	}
}

func (s *Server) discoveryFailure(e *api.Error) (map[string]any, *rpcError) {
	if e.Code == api.CodeUnauthenticated || e.Code == api.CodeRateLimited {
		return nil, apiProtocolError(e)
	}
	return toolFailure(e), nil
}

func (s *Server) discoveryTool(p api.Principal, tool string, args json.RawMessage) (map[string]any, *rpcError) {
	switch tool {
	case toolNamespace:
		if _, rerr := decodeArgs(args); rerr != nil {
			return nil, rerr
		}
		return structured(map[string]any{"namespaces": s.cfg.Backend.Namespaces(p)}, false), nil
	case toolList:
		m, rerr := decodeArgs(args, "namespace", "cursor", "limit")
		if rerr != nil {
			return nil, rerr
		}
		q := api.ListQuery{}
		q.Namespace, _ = m["namespace"].(string)
		q.Cursor, _ = m["cursor"].(string)
		if n, ok := m["limit"].(json.Number); ok {
			v, err := n.Int64()
			if err != nil || v < 1 || v > api.MaxPageSize {
				return toolError(api.CodeInvalidArgument, "limit"), nil
			}
			q.Limit = int(v)
		} else if _, present := m["limit"]; present {
			return toolError(api.CodeInvalidArgument, "limit"), nil
		}
		page, aerr := s.cfg.Backend.List(p, q)
		if aerr != nil {
			return s.discoveryFailure(aerr)
		}
		items := make([]map[string]any, 0, len(page.Items))
		for _, sp := range page.Items {
			items = append(items, compact(sp))
		}
		out := map[string]any{"items": items}
		if page.NextCursor != "" {
			out["next_cursor"] = page.NextCursor
		}
		return structured(out, false), nil
	}
	m, rerr := decodeArgs(args, "name", "version")
	if rerr != nil {
		return nil, rerr
	}
	name, _ := m["name"].(string)
	version, _ := m["version"].(string)
	sp, aerr := s.cfg.Backend.Describe(p, name, version)
	if aerr != nil {
		return s.discoveryFailure(aerr)
	}
	return structured(sp, false), nil
}
