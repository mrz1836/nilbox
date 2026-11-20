package mcpserver

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The MCP methods the tool mode answers. Any other request, server/discover among them,
// is a method it doesn't have.
const (
	methodInitialize = "initialize"
	methodPing       = "ping"
	methodToolsList  = "tools/list"
	methodToolsCall  = "tools/call"
)

// serverVersion is the version serverInfo gives: the tool mode's protocol's, which its flag
// names, never the app's, so what a session's client remembers of the server changes only
// with the protocol.
const serverVersion = "1"

// protocolVersions are the MCP versions the tool mode speaks, the newest first: an
// initialize that asks for one of them gets it, and any other the newest. Each answers
// the same: what the tool mode uses of MCP is the same in all of them.
func protocolVersions() []string {
	return []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}
}

// dispatch answers the request req.
func (s *server) dispatch(ctx context.Context, req request) *reply {
	switch req.method {
	case methodInitialize:
		return resultOf(req.id, s.initialize(req.params))
	case methodPing:
		return resultOf(req.id, struct{}{})
	case methodToolsList:
		return resultOf(req.id, toolList{Tools: toolInfos(s.where)})
	case methodToolsCall:
		return s.toolsCall(ctx, req)
	default:
		return errorOf(req.id, codeNoMethod, "Method not found: "+textutil.Truncate(req.method, 80))
	}
}

// initializeResult is what initialize answers: the protocol version the session speaks,
// what the server offers (its tools alone), who it is, and the instructions the agent
// reads beside the tools.
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    capabilities   `json:"capabilities"`
	ServerInfo      implementation `json:"serverInfo"`
	Instructions    string         `json:"instructions"`
}

// capabilities are what the server offers: tools, whose list never changes in a session.
type capabilities struct {
	Tools struct{} `json:"tools"`
}

// implementation names the server.
type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// initialize answers initialize: the client's protocol version when the tool mode speaks
// it, else the newest it speaks, whatever else params hold.
func (s *server) initialize(params json.RawMessage) initializeResult {
	var asked struct {
		ProtocolVersion string `json:"protocolVersion"`
	}

	_ = json.Unmarshal(params, &asked) // any params get an answer; unreadable ones the newest version

	version := protocolVersions()[0]
	if slices.Contains(protocolVersions(), asked.ProtocolVersion) {
		version = asked.ProtocolVersion
	}

	return initializeResult{
		ProtocolVersion: version,
		ServerInfo:      implementation{Name: brand.Slug, Version: serverVersion},
		Instructions:    kit.ServerInstructions(s.where),
	}
}

// callParams are a tools/call's params: the tool's name and its arguments, as sent. Its
// _meta (the client's tool use ID and progress token) asks nothing of the tool mode.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolsCall answers a tools/call: a call that names no tool, or one not served here, is
// refused as invalid params; any other is answered by its tool, a refusal included.
func (s *server) toolsCall(ctx context.Context, req request) *reply {
	var p callParams

	if err := json.Unmarshal(req.params, &p); err != nil || p.Name == "" {
		return errorOf(req.id, codeInvalidParams, "Invalid params: a tool call names its tool.")
	}

	if !slices.Contains(kit.Tools(s.where), p.Name) {
		return errorOf(req.id, codeInvalidParams, "Unknown tool: "+textutil.Truncate(p.Name, 80))
	}

	return resultOf(req.id, s.callTool(ctx, p.Name, p.Arguments).result())
}
