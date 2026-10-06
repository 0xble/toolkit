package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
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

func TestErrorResultInIsErrorText(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[empty, obj]{Name: "obj.fix", Effect: op.Read, MCP: true, Handler: func(context.Context, op.Request, empty) (obj, error) {
		return obj{}, &op.Error{Kind: op.KindPartial, Code: "stopped", Message: "stopped", Result: obj{N: 2}}
	}})
	res, err := toolkittest.MCPClient(t, r, nil).CallTool(context.Background(), &sdk.CallToolParams{Name: "obj_fix", Arguments: map[string]any{}})
	if err != nil || !res.IsError || res.Content[0].(*sdk.TextContent).Text != `{"error":{"code":"stopped","message":"stopped"},"result":{"n":2}}` {
		t.Errorf("obj_fix: %+v %v", res, err)
	}
}

// TestAnyJSONOutputIsTextOnly checks that an output that may be any JSON
// value has no output schema and comes back as text without structured
// content, and that a nil map does not become null structured content.
func TestAnyJSONOutputIsTextOnly(t *testing.T) {
	r := op.New("t", "v")
	add := func(name string, f func() (any, error)) {
		op.Add(r, op.Op[empty, any]{Name: name, Effect: op.Read, MCP: true,
			Handler: func(context.Context, op.Request, empty) (any, error) { return f() }})
	}
	add("arr", func() (any, error) { return []any{1, "x"}, nil })
	add("str", func() (any, error) { return "s", nil })
	add("null", func() (any, error) { return nil, nil })
	add("obj", func() (any, error) { return map[string]any{"a": 1}, nil })
	op.Add(r, op.Op[empty, json.RawMessage]{Name: "raw", Effect: op.Read, MCP: true,
		Handler: func(context.Context, op.Request, empty) (json.RawMessage, error) {
			return json.RawMessage(`[true]`), nil
		}})
	op.Add(r, op.Op[empty, map[string]any]{Name: "nilmap", Effect: op.Read, MCP: true,
		Handler: func(context.Context, op.Request, empty) (map[string]any, error) { return nil, nil }})
	op.Add(r, op.Op[empty, map[string]any]{Name: "map", Effect: op.Read, MCP: true,
		Handler: func(context.Context, op.Request, empty) (map[string]any, error) { return map[string]any{"k": "v"}, nil }})

	for _, name := range []string{"arr", "str", "null", "obj", "raw"} {
		if mcp.Tool(r.Lookup(name)).OutputSchema != nil {
			t.Errorf("%s: an any output must have no MCP output schema", name)
		}
	}
	if mcp.Tool(r.Lookup("map")).OutputSchema == nil {
		t.Error("map[string]any must keep its object output schema")
	}
	cs := toolkittest.MCPClient(t, r, nil)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMCPTools(t, r, tools.Tools)
	for name, want := range map[string]string{
		"arr": `[1,"x"]`, "str": `"s"`, "null": `null`, "obj": `{"a":1}`, "raw": `[true]`, "nilmap": `null`, "map": `{"k":"v"}`,
	} {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil || res.IsError {
			t.Errorf("%s: %+v %v", name, res, err)
			continue
		}
		if got := res.Content[0].(*sdk.TextContent).Text; got != want {
			t.Errorf("%s: text %s, want %s", name, got, want)
		}
		if structured := res.StructuredContent != nil; structured != (name == "map") {
			t.Errorf("%s: structured content %v", name, res.StructuredContent)
		}
	}
}

func TestAliasesAreNotTools(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[empty, obj]{Name: "obj.get", Effect: op.Read, MCP: true, Aliases: []string{"g"}, Handler: func(context.Context, op.Request, empty) (obj, error) {
		return obj{}, nil
	}})
	tools, err := toolkittest.MCPClient(t, r, nil).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "obj_get" {
		t.Errorf("tools: %+v", tools.Tools)
	}
}

func TestCLIImmediateIsToolMeta(t *testing.T) {
	r := op.New("t", "v")
	set := func(_ context.Context, req op.Request, _ empty) (obj, error) {
		if req.Apply {
			return obj{N: 1}, nil
		}
		return obj{}, nil
	}
	op.Add(r, op.Op[empty, obj]{Name: "obj.set", Effect: op.Write, MCP: true, Handler: set})
	op.Add(r, op.Op[empty, obj]{Name: "obj.set_now", CLI: "obj set-now", Effect: op.Write, MCP: true, CLIImmediate: true, Handler: set})
	cs := toolkittest.MCPClient(t, r, op.AllowAll)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMCPTools(t, r, tools.Tools)
	for _, tool := range tools.Tools {
		if _, ok := tool.Meta[mcp.MetaCLIImmediate]; ok != (tool.Name == "obj_set_now") {
			t.Errorf("%s: _meta %v", tool.Name, tool.Meta)
		}
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "obj_set_now", Arguments: map[string]any{}})
	if err != nil || res.IsError || res.Content[0].(*sdk.TextContent).Text != `{"n":0}` {
		t.Errorf("without apply the tool previews: %+v %v", res, err)
	}
}

func TestCLIConfirmedIsToolMetaAndStillNeedsConfirm(t *testing.T) {
	r := op.New("t", "v")
	set := func(_ context.Context, req op.Request, _ empty) (obj, error) {
		if req.Apply {
			return obj{N: 1}, nil
		}
		return obj{}, nil
	}
	op.Add(r, op.Op[empty, obj]{Name: "obj.wipe_now", CLI: "obj wipe-now", Effect: op.Destructive, MCP: true, CLIImmediate: true, Handler: set})
	op.Add(r, op.Op[empty, obj]{Name: "obj.wipe", Effect: op.Destructive, MCP: true, CLIImmediate: true, CLIConfirmed: true, Handler: set})
	cs := toolkittest.MCPClient(t, r, op.AllowAll)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMCPTools(t, r, tools.Tools)
	for _, tool := range tools.Tools {
		if _, ok := tool.Meta[mcp.MetaCLIConfirmed]; ok != (tool.Name == "obj_wipe") || tool.Meta[mcp.MetaCLIImmediate] != true {
			t.Errorf("%s: _meta %v", tool.Name, tool.Meta)
		}
	}
	for _, args := range []map[string]any{{"apply": true}, {"apply": true, "confirm": false}} {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "obj_wipe", Arguments: args})
		if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, `"confirmation_required"`) {
			t.Errorf("%v: %+v %v; want isError and confirmation_required", args, res, err)
		}
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "obj_wipe", Arguments: map[string]any{"apply": true, "confirm": true}})
	if err != nil || res.IsError || res.Content[0].(*sdk.TextContent).Text != `{"n":1}` {
		t.Errorf("apply and confirm apply: %+v %v", res, err)
	}
}

type exportIn struct {
	Out string `json:"out,omitempty" toolkit:"cli-only"`
}

// TestCLIOnlyInputRefused checks that a tool's input schema omits a cli-only
// input and that a call setting it is refused before the handler runs.
func TestCLIOnlyInputRefused(t *testing.T) {
	var calls int
	r := op.New("t", "v")
	op.Add(r, op.Op[exportIn, obj]{Name: "notes.export", Effect: op.Read, MCP: true, Handler: func(context.Context, op.Request, exportIn) (obj, error) {
		calls++
		return obj{}, nil
	}})
	if _, ok := mcp.Tool(r.Lookup("notes.export")).InputSchema.(*jsonschema.Schema).Properties["out"]; ok {
		t.Error("the tool input schema shows the cli-only input")
	}
	cs := toolkittest.MCPClient(t, r, op.AllowAll)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMCPTools(t, r, tools.Tools)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "notes_export", Arguments: map[string]any{"out": "/tmp/x"}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*sdk.TextContent).Text
	if !res.IsError || toolkittest.ErrorCode([]byte(text)) != "cli_only" {
		t.Errorf("cli-only input over MCP: %s %v", text, err)
	}
	if calls != 0 {
		t.Errorf("handler ran %d times", calls)
	}
}
