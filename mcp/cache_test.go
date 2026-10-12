package mcp

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/0xble/toolkit/op"
)

type cacheInput struct {
	Name   string            `json:"name"`
	Items  []string          `json:"items,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

type cacheOutput struct {
	Count int `json:"count"`
}

func cacheRegistry() *op.Registry {
	r := op.New("cache", "v1")
	op.Add(r, op.Op[cacheInput, cacheOutput]{
		Name:   "cache.read",
		Effect: op.Read,
		MCP:    true,
		Handler: func(context.Context, op.Request, cacheInput) (cacheOutput, error) {
			return cacheOutput{}, nil
		},
	})
	return r
}

func TestSharedToolSchemaIsSafeForConcurrentServers(t *testing.T) {
	r := cacheRegistry()
	e := r.Lookup("cache.read")
	shared := sharedTool(e)
	input, ok := shared.InputSchema.(*jsonschema.Schema)
	if !ok {
		t.Fatalf("input schema type = %T, want *jsonschema.Schema", shared.InputSchema)
	}
	want := e.InputSchema()
	if !reflect.DeepEqual(input, want) {
		t.Fatal("cached schema differs from the entry schema")
	}

	const servers = 64
	var wg sync.WaitGroup
	wg.Add(servers)
	for range servers {
		go func() {
			defer wg.Done()
			_ = NewServer(r, op.AllowAll, nil)
		}()
	}
	wg.Wait()

	if !reflect.DeepEqual(input, want) {
		t.Fatal("concurrent NewServer changed the shared input schema")
	}
	if got := sharedTool(e); got != shared {
		t.Fatal("cached tool was rebuilt")
	}
}

func TestSharedToolsDoNotCrossRegistries(t *testing.T) {
	r1, r2 := cacheRegistry(), cacheRegistry()
	t1 := sharedTool(r1.Lookup("cache.read"))
	t2 := sharedTool(r2.Lookup("cache.read"))
	if t1 == t2 || t1.InputSchema.(*jsonschema.Schema) == t2.InputSchema.(*jsonschema.Schema) {
		t.Fatal("distinct registries share a cached MCP tool or schema")
	}
}
