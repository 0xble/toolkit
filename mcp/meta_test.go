package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit/mcp"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type metaIn struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

type metaOut struct {
	Meta    map[string]any `json:"meta"`
	ArgMeta map[string]any `json:"arg_meta"`
}

func metaRegistry() *op.Registry {
	r := op.New("t", "v")
	op.Add(r, op.Op[metaIn, metaOut]{Name: "caller.show", Effect: op.Read, MCP: true,
		Handler: func(_ context.Context, req op.Request, in metaIn) (metaOut, error) {
			return metaOut{Meta: req.Meta, ArgMeta: in.Meta}, nil
		}})
	return r
}

// TestRequestMeta checks that a tool call's request _meta reaches the
// handler as op.Request.Meta on the stdio server (in memory, without an HTTP
// request) and on streamable HTTP, and that an argument named _meta stays an
// argument. The client SDK adds its own protocol keys to every _meta, so the
// cases look only at the caller's key.
func TestRequestMeta(t *testing.T) {
	srv := httptest.NewServer(mcp.Handler(metaRegistry(), mcp.Options{}))
	t.Cleanup(srv.Close)
	httpCS, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(),
		&sdk.StreamableClientTransport{Endpoint: srv.URL, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = httpCS.Close() })

	for transport, cs := range map[string]*sdk.ClientSession{"stdio": toolkittest.MCPClient(t, metaRegistry(), nil), "http": httpCS} {
		for name, c := range map[string]struct {
			meta          sdk.Meta
			args          map[string]any
			caller, inArg any
		}{
			"present":  {sdk.Meta{"x/caller": map[string]any{"session_id": "s1"}}, map[string]any{}, map[string]any{"session_id": "s1"}, nil},
			"absent":   {nil, map[string]any{}, nil, nil},
			"argument": {nil, map[string]any{"_meta": map[string]any{"x/caller": "forged"}}, nil, "forged"},
		} {
			res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "caller_show", Arguments: c.args, Meta: c.meta})
			if err != nil || res.IsError {
				t.Errorf("%s %s: %+v %v", transport, name, res, err)
				continue
			}
			var got metaOut
			if err := json.Unmarshal([]byte(res.Content[0].(*sdk.TextContent).Text), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Meta["x/caller"], c.caller) || !reflect.DeepEqual(got.ArgMeta["x/caller"], c.inArg) {
				t.Errorf("%s %s: Meta %v, argument _meta %v", transport, name, got.Meta, got.ArgMeta)
			}
		}
	}
}

// TestRequestMetaNilWhenAbsent sends a raw tool call without _meta and
// checks that the handler sees a nil Meta.
func TestRequestMetaNilWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(mcp.Handler(metaRegistry(), mcp.Options{}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"caller_show","arguments":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var rpc struct {
		Result sdk.CallToolResult `json:"result"`
	}
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &rpc); err != nil || len(rpc.Result.Content) == 0 {
		t.Fatalf("tools/call: %d %s %v", resp.StatusCode, b, err)
	}
	if got := rpc.Result.Content[0].(*sdk.TextContent).Text; got != `{"meta":null,"arg_meta":null}` {
		t.Errorf("without _meta: %s", got)
	}
}
