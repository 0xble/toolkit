// Package mcp exposes the operations of a registry that set MCP: true as MCP
// tools, over stdio or streamable HTTP.
//
// A tool's name is the operation name with dots replaced by underscores, its
// input schema is the registry's wire schema (including apply and confirm),
// and its annotations follow the effect. Every call goes through the same
// op.Entry.CallJSON path as HTTP, so validation, the apply and confirm rules
// and authorization are identical. A failed call is an isError result whose
// text is {"error": {code, message, suggestions}}.
package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit/op"
)

// Options configures the MCP surface.
type Options struct {
	// Authorizer decides whether a caller may run an operation. Nil means the
	// transport default: op.AllowAll for stdio, whose caller already runs the
	// binary as the user, and op.DenyWrites for HTTP.
	Authorizer op.Authorizer
}

// NewServer returns an MCP server with one tool per MCP operation. httpReq is
// the inbound HTTP request for authorizers, or nil for local transports. A
// nil auth means op.DenyWrites.
func NewServer(reg *op.Registry, auth op.Authorizer, httpReq *http.Request) *sdk.Server {
	if auth == nil {
		auth = op.DenyWrites
	}
	s := sdk.NewServer(&sdk.Implementation{Name: reg.Tool, Version: reg.Version}, nil)
	for _, e := range reg.Entries() {
		if !e.MCP {
			continue
		}
		s.AddTool(Tool(e), func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			out, err := e.CallJSON(ctx, op.Request{Surface: op.SurfaceMCP, HTTP: httpReq}, req.Params.Arguments, auth)
			if err != nil {
				return errorResult(err), nil
			}
			return result(e, out)
		})
	}
	return s
}

// Tool is the MCP tool definition of e. The output schema is set only when
// the output is a JSON object, as MCP requires.
func Tool(e *op.Entry) *sdk.Tool {
	destructive := e.Effect == op.Destructive
	t := &sdk.Tool{
		Name:        e.MCPName(),
		Description: e.Summary,
		InputSchema: e.InputSchema(),
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    e.Effect == op.Read,
			DestructiveHint: &destructive,
		},
	}
	if out := e.OutputSchema(); out.Type == "object" {
		t.OutputSchema = out
	}
	return t
}

func result(e *op.Entry, out any) (*sdk.CallToolResult, error) {
	b, err := json.Marshal(out)
	if err != nil {
		return errorResult(op.Errorf(op.KindError, "encode_failed", "encode output: %v", err)), nil
	}
	r := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}
	if e.OutputSchema().Type == "object" {
		r.StructuredContent = json.RawMessage(b)
	}
	return r, nil
}

// ErrorBody is the text payload of an isError result.
type ErrorBody struct {
	Error *op.Error `json:"error"`
}

func errorResult(err error) *sdk.CallToolResult {
	b, _ := json.Marshal(ErrorBody{Error: op.AsError(err, op.KindError)})
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}
}

// ServeStdio serves MCP on stdin and stdout until the client disconnects or
// ctx is done.
func ServeStdio(ctx context.Context, reg *op.Registry, opts Options) error {
	auth := opts.Authorizer
	if auth == nil {
		auth = op.AllowAll
	}
	return NewServer(reg, auth, nil).Run(ctx, &sdk.StdioTransport{})
}

// Handler serves MCP over streamable HTTP, stateless with JSON responses,
// so it works behind a plain reverse proxy.
func Handler(reg *op.Registry, opts Options) http.Handler {
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		return NewServer(reg, opts.Authorizer, r)
	}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
