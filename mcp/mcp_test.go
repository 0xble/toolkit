package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit/mcp"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type empty struct{}

type obj struct {
	N int `json:"n"`
}

func TestOutputSchemaOnlyForObjects(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[empty, obj]{Name: "obj.get", Effect: op.Read, MCP: true, Handler: func(context.Context, op.Request, empty) (obj, error) {
		return obj{N: 1}, nil
	}})
	op.Add(r, op.Op[empty, []string]{Name: "names.list", Effect: op.Read, MCP: true, Handler: func(context.Context, op.Request, empty) ([]string, error) {
		return []string{"a"}, nil
	}})
	op.Add(r, op.Op[empty, obj]{Name: "hidden", Effect: op.Read, Handler: func(context.Context, op.Request, empty) (obj, error) {
		return obj{}, nil
	}})
	if mcp.Tool(r.Lookup("names.list")).OutputSchema != nil || mcp.Tool(r.Lookup("obj.get")).OutputSchema == nil {
		t.Error("output schema must be set exactly for object outputs")
	}
	cs := toolkittest.MCPClient(t, r, nil)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMCPTools(t, r, tools.Tools)

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "obj_get", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("obj_get: %+v %v", res, err)
	}
	if b, _ := json.Marshal(res.StructuredContent); string(b) != `{"n":1}` {
		t.Errorf("structured content: %s", b)
	}
	res, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "names_list", Arguments: map[string]any{}})
	if err != nil || res.IsError || res.StructuredContent != nil || res.Content[0].(*sdk.TextContent).Text != `["a"]` {
		t.Errorf("names_list: %+v %v", res, err)
	}
	res, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "obj_get", Arguments: map[string]any{"x": 1}})
	if err != nil || !res.IsError || toolkittest.ErrorCode([]byte(res.Content[0].(*sdk.TextContent).Text)) != "invalid_input" {
		t.Errorf("bad input: %+v %v", res, err)
	}
}
