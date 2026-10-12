package mcp_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xble/toolkit/internal/schemacache"
	toolkitmcp "github.com/0xble/toolkit/mcp"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type cacheLeaf struct {
	ID      string            `json:"id"`
	Values  []string          `json:"values"`
	Labels  map[string]string `json:"labels"`
	Details map[string]any    `json:"details"`
}

type cacheInput struct {
	Name   string            `json:"name"`
	Items  []string          `json:"items,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	A      cacheLeaf         `json:"a"`
	B      cacheLeaf         `json:"b"`
	C      cacheLeaf         `json:"c"`
	D      cacheLeaf         `json:"d"`
	E      cacheLeaf         `json:"e"`
	F      cacheLeaf         `json:"f"`
	G      cacheLeaf         `json:"g"`
	H      cacheLeaf         `json:"h"`
	I      cacheLeaf         `json:"i"`
	J      cacheLeaf         `json:"j"`
	K      cacheLeaf         `json:"k"`
	L      cacheLeaf         `json:"l"`
}

type cacheOutput struct {
	Count int `json:"count"`
}

func cacheRegistry() *op.Registry {
	r := op.New("cache", "v1")
	op.Add(r, op.Op[cacheInput, cacheOutput]{
		Name:    "cache.read",
		Effect:  op.Read,
		MCP:     true,
		Summary: "initial summary",
		Handler: func(context.Context, op.Request, cacheInput) (cacheOutput, error) {
			return cacheOutput{}, nil
		},
	})
	return r
}

func TestConcurrentNewServerDoesNotMutateSharedSchemas(t *testing.T) {
	r := cacheRegistry()
	e := r.Lookup("cache.read")
	wantInput, wantOutput := e.InputSchema(), e.OutputSchema()

	const servers = 64
	var wg sync.WaitGroup
	wg.Add(servers)
	for range servers {
		go func() {
			defer wg.Done()
			_ = toolkitmcp.NewServer(r, op.AllowAll, nil)
		}()
	}
	wg.Wait()

	shared := schemacache.Get(e)
	if !reflect.DeepEqual(shared.Input, wantInput) || !reflect.DeepEqual(shared.Output, wantOutput) {
		t.Fatal("concurrent NewServer changed the shared schemas")
	}
	_ = toolkitmcp.NewServer(r, op.AllowAll, nil)
	if !reflect.DeepEqual(shared.Input, wantInput) || !reflect.DeepEqual(shared.Output, wantOutput) {
		t.Fatal("repeated NewServer changed the shared schemas")
	}
}

func TestNewServerDoesNotCloneSchemasPerCall(t *testing.T) {
	r := cacheRegistry()
	_ = toolkitmcp.NewServer(r, op.AllowAll, nil)
	// AddTool resolves schemas itself, and its race-instrumented allocation
	// cost differs from ordinary builds. Measure that unavoidable SDK cost on
	// the same schema, then allow only cheap toolkit metadata/handler setup.
	tool := toolkitmcp.Tool(r.Lookup("cache.read"))
	noop := func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) { return nil, nil }
	sdkAllocs := testing.AllocsPerRun(20, func() {
		s := sdk.NewServer(&sdk.Implementation{Name: r.Tool, Version: r.Version}, nil)
		s.AddTool(tool, noop)
	})
	const maxSetupAllocs = 100
	maxAllocs := sdkAllocs + maxSetupAllocs
	allocs := testing.AllocsPerRun(20, func() { _ = toolkitmcp.NewServer(r, op.AllowAll, nil) })
	t.Logf("NewServer allocations: %.0f (SDK baseline %.0f)", allocs, sdkAllocs)
	if allocs > maxAllocs {
		t.Fatalf("NewServer allocations = %.0f, want <= %.0f; per-server schema cloning likely returned", allocs, maxAllocs)
	}
}

func TestNewServerRefreshesMutableToolMetadata(t *testing.T) {
	r := cacheRegistry()
	first, err := toolkittest.MCPClient(t, r, op.AllowAll).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Tools) != 1 || first.Tools[0].Description != "initial summary" {
		t.Fatalf("initial tools: %+v", first.Tools)
	}

	e := r.Lookup("cache.read")
	e.Summary = "updated summary"
	e.Effect = op.Destructive
	e.CLIImmediate, e.CLIConfirmed = true, true
	second, err := toolkittest.MCPClient(t, r, op.AllowAll).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Tools) != 1 || second.Tools[0].Description != "updated summary" {
		t.Fatalf("NewServer reused stale tool metadata: %+v", second.Tools)
	}
	tool := second.Tools[0]
	if tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint ||
		tool.Meta[toolkitmcp.MetaCLIImmediate] != true || tool.Meta[toolkitmcp.MetaCLIConfirmed] != true {
		t.Fatalf("NewServer reused stale annotations or CLI flags: %+v", tool)
	}
	if first.Tools[0].Description != "initial summary" || !first.Tools[0].Annotations.ReadOnlyHint {
		t.Fatal("refreshing tool metadata changed the first server's tool")
	}
	e.MCP = false
	third, err := toolkittest.MCPClient(t, r, op.AllowAll).ListTools(context.Background(), nil)
	if err != nil || len(third.Tools) != 0 {
		t.Fatalf("NewServer ignored current MCP visibility: %+v, %v", third, err)
	}
}

func TestSchemasDoNotCrossRegistries(t *testing.T) {
	r1, r2 := cacheRegistry(), cacheRegistry()
	s1 := schemacache.Get(r1.Lookup("cache.read"))
	s2 := schemacache.Get(r2.Lookup("cache.read"))
	if s1.Input == s2.Input || s1.Output == s2.Output {
		t.Fatal("distinct registries share cached schema pointers")
	}
}
