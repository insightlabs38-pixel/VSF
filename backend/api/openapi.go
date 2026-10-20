package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// MaxOpenAPIOperations bounds one generated document; larger catalogs are described per namespace.
const MaxOpenAPIOperations = 200

func errorTable() []any {
	var rows []any
	for _, c := range Codes() {
		i := InfoOf(c)
		rows = append(rows, map[string]any{
			"code": string(c), "http_status": i.HTTP, "cli_exit": i.Exit, "category": string(i.Category),
			"effect": string(i.Effect), "retry": string(i.Retry), "summary": i.Summary,
		})
	}
	return rows
}

func errorStatuses() []int {
	seen := map[int]bool{}
	for _, c := range Codes() {
		seen[InfoOf(c).HTTP] = true
	}
	var out []int
	for s := range seen {
		out = append(out, s)
	}
	sort.Ints(out)
	return out
}

func ref(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }

func jsonBody(schema map[string]any) map[string]any {
	return map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
}

func jsonResp(desc string, schema map[string]any) map[string]any {
	return map[string]any{"description": desc, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
}

// ComponentKey names the schema component generated for one operation version.
func ComponentKey(s *Spec, kind string) string {
	return s.Name + "." + kind + "." + strings.ReplaceAll(s.ContractVersion, ".", "-")
}

func responsesFor(resultRef map[string]any) map[string]any {
	out := map[string]any{
		"200": jsonResp("The operation finished; see category and effect.", resultRef),
		"202": jsonResp("Still running; poll the invocation. The source may or may not have changed.", resultRef),
	}
	for _, st := range errorStatuses() {
		if st == 200 || st == 202 {
			continue
		}
		out[strconv.Itoa(st)] = jsonResp(http.StatusText(st)+"; the typed error says whether the source changed.", resultRef)
	}
	return out
}

func staticPaths() map[string]any {
	errResp := map[string]any{}
	for _, st := range errorStatuses() {
		errResp[strconv.Itoa(st)] = jsonResp(http.StatusText(st), ref("ErrorEnvelope"))
	}
	get := func(id, summary string, params []any, ok map[string]any) map[string]any {
		resp := map[string]any{"200": ok}
		for k, v := range errResp {
			resp[k] = v
		}
		op := map[string]any{"operationId": id, "summary": summary, "responses": resp, "security": []any{map[string]any{"bearer": []any{}}}}
		if len(params) > 0 {
			op["parameters"] = params
		}
		return op
	}
	q := func(name, desc string) map[string]any {
		return map[string]any{"name": name, "in": "query", "required": false, "description": desc, "schema": map[string]any{"type": "string"}}
	}
	p := func(name string) map[string]any {
		return map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}}
	}
	handle := []any{p("key")}
	resultBase := jsonResp("The invocation's current outcome.", ref("Result"))
	post := func(id, summary string, body map[string]any) map[string]any {
		resp := map[string]any{"200": resultBase}
		for _, st := range errorStatuses() {
			resp[strconv.Itoa(st)] = jsonResp(http.StatusText(st), ref("Result"))
		}
		op := map[string]any{"operationId": id, "summary": summary, "parameters": handle, "responses": resp, "security": []any{map[string]any{"bearer": []any{}}}}
		if body != nil {
			op["requestBody"] = body
		}
		return op
	}
	return map[string]any{
		"/v1/discover":   map[string]any{"get": get("discover", "Interface versions and limits.", nil, jsonResp("Discovery.", ref("Discovery")))},
		"/v1/namespaces": map[string]any{"get": get("listNamespaces", "Applications with operations, from the index alone.", nil, jsonResp("Namespaces.", map[string]any{"type": "object"}))},
		"/v1/operations": map[string]any{"get": get("listOperations", "A bounded, deterministic page of operations.",
			[]any{q("namespace", "Limit to one namespace."), q("cursor", "Opaque cursor from a previous page."), q("limit", "Page size, at most 200.")},
			jsonResp("A page.", map[string]any{"type": "object"}))},
		"/v1/operations/{name}": map[string]any{"get": get("describeOperation", "One operation's schema and effect disclosure.",
			[]any{p("name"), q("version", "Exact contract version; default is the newest served.")}, jsonResp("The operation.", map[string]any{"type": "object"}))},
		"/v1/operations/{name}/versions": map[string]any{"get": get("operationVersions", "Every known contract version and why it is or is not served.",
			[]any{p("name")}, jsonResp("Versions.", map[string]any{"type": "object"}))},
		"/v1/invocations/{key}":           map[string]any{"get": get("getInvocation", "The outcome of an earlier invocation.", handle, resultBase)},
		"/v1/invocations/{key}/cancel":    map[string]any{"post": post("cancelInvocation", "Ask a run to stop; the result says what that did to the source.", nil)},
		"/v1/invocations/{key}/reconcile": map[string]any{"post": post("reconcileInvocation", "Settle an unknown outcome from evidence of how it ended.", jsonBody(ref("ReconcileRequest")))},
	}
}

func staticComponents() map[string]any {
	return map[string]any{
		"ErrorEnvelope": map[string]any{"type": "object", "required": []any{"error"}, "properties": map[string]any{"error": ref("Error")}},
		"Error": map[string]any{"type": "object", "required": []any{"code", "message", "effect", "retry"}, "properties": map[string]any{
			"code": map[string]any{"enum": codeEnum()}, "message": str(), "reason": str(),
			"effect":         map[string]any{"enum": enumOf(string(EffectNone), string(EffectApplied), string(EffectUnknown))},
			"retry":          map[string]any{"type": "string"},
			"retry_after_ms": map[string]any{"type": "integer"}, "details": map[string]any{"type": "object"},
		}},
		"Result": resultSchema("", "", "", nil),
		"Confirmation": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"token", "by", "digest", "expires_at"},
			"properties": map[string]any{"token": str(), "by": str(), "digest": str(), "expires_at": map[string]any{"type": "integer"}}},
		"Discovery": map[string]any{"type": "object"},
		"ReconcileRequest": map[string]any{"type": "object", "additionalProperties": false, "required": []any{"session_id", "expect"},
			"properties": map[string]any{"session_id": str(), "expect": map[string]any{"type": "object", "additionalProperties": str()}}},
	}
}

func disclosureText(s *Spec) string {
	var b strings.Builder
	b.WriteString(s.Description)
	return b.String()
}

// OpenAPI generates the document from the operations the backend serves this principal; nothing is hand-written per operation.
func OpenAPI(b Backend, p Principal, namespace string) (map[string]any, *Error) {
	if namespace == "" && b.Discovery().Operations > MaxOpenAPIOperations {
		return nil, errWith(CodeInvalidRequest, "namespace_required", map[string]any{"max_operations": MaxOpenAPIOperations})
	}
	paths, comps := staticPaths(), staticComponents()
	cursor, count := "", 0
	for {
		page, err := b.List(p, ListQuery{Namespace: namespace, Cursor: cursor, Limit: MaxPageSize})
		if err != nil {
			return nil, err
		}
		for _, s := range page.Items {
			count++
			if count > MaxOpenAPIOperations {
				return nil, errWith(CodeInvalidRequest, "namespace_too_large", map[string]any{"max_operations": MaxOpenAPIOperations})
			}
			inKey, outKey := ComponentKey(s, "Invocation"), ComponentKey(s, "Result")
			comps[inKey] = s.InputSchema
			comps[outKey] = s.OutputSchema
			path := fmt.Sprintf("/v1/operations/%s/versions/%s/invocations", s.Name, s.ContractVersion)
			if s.Effect.Write {
				paths[strings.TrimSuffix(path, "/invocations")+"/confirmations"] = map[string]any{"post": map[string]any{
					"operationId": s.Name + "@" + s.ContractVersion + ".confirm", "summary": "A person confirms exactly this request (user interface credentials only).",
					"requestBody": jsonBody(ref(inKey)), "security": []any{map[string]any{"bearer": []any{}}},
					"parameters": []any{map[string]any{"name": "for", "in": "query", "required": false, "schema": map[string]any{"type": "string"},
						"description": "The caller who will run the request, when the person confirms for a caller they have delegated to."}},
					"responses": map[string]any{"200": jsonResp("The confirmation to send with the invocation.", ref("Confirmation")),
						"403": jsonResp("Only a person's interface credential may confirm.", ref("ErrorEnvelope"))},
				}}
			}
			paths[path] = map[string]any{"post": map[string]any{
				"operationId": s.Name + "@" + s.ContractVersion, "summary": s.Title, "description": disclosureText(s),
				"requestBody": jsonBody(ref(inKey)), "responses": responsesFor(ref(outKey)),
				"security":     []any{map[string]any{"bearer": []any{}}},
				"x-vsf-effect": s.Effect, "x-vsf-contract": map[string]any{"version": s.ContractVersion, "digest": s.ContractDigest, "lifecycle": s.Lifecycle},
				"x-mcp-annotations": s.Annotations,
			}}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "Varifold Systems Foundry", "version": APIVersion,
			"description": "Every operation route names an exact contract version; a version that is not served is refused, never substituted."},
		"jsonSchemaDialect": "https://json-schema.org/draft/2020-12/schema",
		"paths":             paths,
		"components": map[string]any{
			"schemas":         comps,
			"securitySchemes": map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer"}},
		},
		"x-vsf-errors":  errorTable(),
		"x-vsf-routing": map[string]any{"invoke": "/v1/operations/{name}/versions/{version}/invocations", "discovery": "/v1/operations/{name}/versions"},
	}, nil
}
