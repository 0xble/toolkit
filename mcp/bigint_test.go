package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit/mcp"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type bigIn struct {
	ID    int64   `json:"id"`
	Max   uint64  `json:"max"`
	Ratio float64 `json:"ratio,omitempty"`
}

const bigJSON = `{"id":5312241539987020022,"max":18446744073709551615,"ratio":1.5}`

func bigRegistry() *op.Registry {
	r := op.New("t", "v")
	op.Add(r, op.Op[bigIn, bigIn]{Name: "big", Effect: op.Read, MCP: true,
		Handler: func(_ context.Context, _ op.Request, in bigIn) (bigIn, error) { return in, nil }})
	return r
}

// TestIntegersAbove2To53 checks that an int64 or uint64 above 2^53 reaches
// the handler and the text result exactly.
func TestIntegersAbove2To53(t *testing.T) {
	cs := toolkittest.MCPClient(t, bigRegistry(), nil)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "big", Arguments: json.RawMessage(bigJSON)})
	if err != nil || res.IsError {
		t.Fatalf("big: %+v %v", res, err)
	}
	if got := res.Content[0].(*sdk.TextContent).Text; got != bigJSON {
		t.Errorf("text: %s; want %s", got, bigJSON)
	}
	res, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "big", Arguments: json.RawMessage(`{"id":"1"}`)})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, `has type \"string\", want \"integer\"`) {
		t.Errorf("a string for an integer: %+v %v", res, err)
	}
}

// TestStructuredContentKeepsIntegersAbove2To53 reads the raw tools/call
// response, since the SDK client decodes structured content into float64.
func TestStructuredContentKeepsIntegersAbove2To53(t *testing.T) {
	srv := httptest.NewServer(mcp.Handler(bigRegistry(), mcp.Options{}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"big","arguments":`+bigJSON+`}}`))
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
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &rpc); err != nil {
		t.Fatalf("tools/call: %d %s %v", resp.StatusCode, b, err)
	}
	if got := string(rpc.Result.StructuredContent); got != bigJSON {
		t.Errorf("structured content: %s; want %s", got, bigJSON)
	}
}
