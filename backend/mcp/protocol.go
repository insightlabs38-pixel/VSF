// Package mcp serves the VSF operation catalog over the Model Context Protocol (revision 2026-07-28), statelessly.
package mcp

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Version is the one protocol revision this server implements; there is no session to negotiate it into.
const Version = "2026-07-28"

const (
	metaVersion   = "io.modelcontextprotocol/protocolVersion"
	metaClient    = "io.modelcontextprotocol/clientInfo"
	metaCaps      = "io.modelcontextprotocol/clientCapabilities"
	metaServer    = "io.modelcontextprotocol/serverInfo"
	headerVersion = "MCP-Protocol-Version"
	headerMethod  = "Mcp-Method"
	headerName    = "Mcp-Name"
)

// JSON-RPC and MCP error codes; the 40xxx codes carry this server's own authentication failures.
const (
	codeParse           = -32700
	codeInvalidRequest  = -32600
	codeMethodNotFound  = -32601
	codeInvalidParams   = -32602
	codeInternal        = -32603
	codeHeaderMismatch  = -32020
	codeUnsupportedVer  = -32022
	codeUnauthenticated = 40100
	codeForbidden       = 40300
	codeRateLimited     = 42900
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// validID accepts a string or an integer; null, booleans, fractions and containers are not request ids.
func validID(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return false
	}
	if s[0] == '"' {
		var v string
		return json.Unmarshal(raw, &v) == nil
	}
	n := json.Number(s)
	if _, err := n.Int64(); err != nil {
		return false
	}
	return !strings.ContainsAny(s, ".eE")
}

const b64Prefix, b64Suffix = "=?base64?", "?="

// decodeHeaderValue undoes the sentinel encoding clients use for values that are not plain header text.
func decodeHeaderValue(v string) (string, bool) {
	if strings.HasPrefix(v, b64Prefix) && strings.HasSuffix(v, b64Suffix) && len(v) >= len(b64Prefix)+len(b64Suffix) {
		b, err := base64.StdEncoding.DecodeString(v[len(b64Prefix) : len(v)-len(b64Suffix)])
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	return v, true
}

func complete(fields map[string]any) map[string]any {
	fields["resultType"] = "complete"
	return fields
}
