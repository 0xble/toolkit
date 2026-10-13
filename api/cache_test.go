package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/0xble/toolkit/internal/schemacache"
	"github.com/0xble/toolkit/op"
)

type handlerCacheLeaf struct {
	ID      string            `json:"id"`
	Names   []string          `json:"names"`
	Labels  map[string]string `json:"labels"`
	Details map[string]any    `json:"details"`
}

type handlerCacheInput struct {
	A handlerCacheLeaf `json:"a"`
	B handlerCacheLeaf `json:"b"`
	C handlerCacheLeaf `json:"c"`
	D handlerCacheLeaf `json:"d"`
	E handlerCacheLeaf `json:"e"`
	F handlerCacheLeaf `json:"f"`
	G handlerCacheLeaf `json:"g"`
	H handlerCacheLeaf `json:"h"`
	I handlerCacheLeaf `json:"i"`
	J handlerCacheLeaf `json:"j"`
	K handlerCacheLeaf `json:"k"`
	L handlerCacheLeaf `json:"l"`
}

type handlerCacheOutput struct {
	OK bool `json:"ok"`
}

func handlerCacheRegistry() *op.Registry {
	r := op.New("handler-cache", "v1")
	op.Add(r, op.Op[handlerCacheInput, handlerCacheOutput]{
		Name:    "cache.read",
		Effect:  op.Read,
		MCP:     true,
		Summary: "initial summary",
		Handler: func(context.Context, op.Request, handlerCacheInput) (handlerCacheOutput, error) {
			return handlerCacheOutput{OK: true}, nil
		},
	})
	return r
}

func openAPIResponse(t testing.TB, r *op.Registry) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	Handler(r, Options{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json: status %d", w.Code)
	}
	return bytes.TrimSpace(w.Body.Bytes())
}

func TestOpenAPIBuildsCurrentRegistryPerRequest(t *testing.T) {
	r := handlerCacheRegistry()
	first := openAPIResponse(t, r)
	if !bytes.Contains(first, []byte(`"/ops/cache.read"`)) {
		t.Fatalf("initial operation missing: %s", first)
	}

	op.Add(r, op.Op[handlerCacheInput, handlerCacheOutput]{
		Name:   "cache.second",
		Effect: op.Read,
		Handler: func(context.Context, op.Request, handlerCacheInput) (handlerCacheOutput, error) {
			return handlerCacheOutput{}, nil
		},
	})
	second := openAPIResponse(t, r)
	if !bytes.Contains(second, []byte(`"/ops/cache.second"`)) {
		t.Fatalf("OpenAPI document stayed stale after op.Add: %s", second)
	}

	want, err := json.Marshal(OpenAPI(r))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second, want) {
		t.Fatal("served OpenAPI document differs byte-for-byte from OpenAPI(reg)")
	}
}

func TestHandlerConstructionDoesNotCloneSchemasPerCall(t *testing.T) {
	r := handlerCacheRegistry()
	_ = Handler(r, Options{})
	allocs := testing.AllocsPerRun(20, func() { _ = Handler(r, Options{}) })
	t.Logf("Handler allocations: %.0f", allocs)
	const maxAllocs = 100
	if allocs > maxAllocs {
		t.Fatalf("Handler allocations = %.0f, want <= %d; per-handler schema cloning likely returned", allocs, maxAllocs)
	}

	want, err := json.Marshal(OpenAPI(r))
	if err != nil {
		t.Fatal(err)
	}
	if got := openAPIResponse(t, r); !bytes.Equal(got, want) {
		t.Fatal("served OpenAPI document differs byte-for-byte from OpenAPI(reg)")
	}
}

func TestOpenAPIRawSchemasMatchPublicJSON(t *testing.T) {
	type input struct {
		Z   string              `json:"z" help:"<z> & quoted \"text\"" default:"<default>&"`
		A   map[string][]string `json:"a,omitempty"`
		Any any                 `json:"any,omitempty"`
		Raw json.RawMessage     `json:"raw,omitempty"`
	}
	r := op.New("<tool>&\u2028\u2029", "v\"1")
	op.Add(r, op.Op[input, handlerCacheOutput]{
		Name: "escape.read", Effect: op.Read, Summary: "<summary>&\u2028\u2029",
		Handler: func(context.Context, op.Request, input) (handlerCacheOutput, error) { return handlerCacheOutput{}, nil },
	})
	op.Add(r, op.Op[struct{}, any]{
		Name: "any.read", Effect: op.Read,
		Handler: func(context.Context, op.Request, struct{}) (any, error) { return nil, nil },
	})
	op.Add(r, op.Op[struct{}, json.RawMessage]{
		Name: "raw.read", Effect: op.Read,
		Handler: func(context.Context, op.Request, struct{}) (json.RawMessage, error) { return nil, nil },
	})
	want, err := json.Marshal(OpenAPI(r))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if got := openAPIResponse(t, r); !bytes.Equal(got, want) {
			t.Fatalf("RawMessage embedding changed JSON escaping or key order:\n got: %s\nwant: %s", got, want)
		}
	}
	if !bytes.Contains(want, []byte(`\u003cz\u003e \u0026`)) || !bytes.Contains(want, []byte(`\u2028\u2029`)) {
		t.Fatalf("fixture did not exercise HTML and Unicode escaping: %s", want)
	}

	// The public document and schema accessors retain caller-owned copies.
	e := r.Lookup("escape.read")
	shared := schemacache.Get(e)
	doc := OpenAPI(r)
	post := doc["paths"].(map[string]any)["/ops/escape.read"].(map[string]any)["post"].(map[string]any)
	in := post["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(*jsonschema.Schema)
	out := post["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(*jsonschema.Schema)
	in.Properties["z"].Description = "changed"
	out.Properties["ok"].Type = "string"
	e.InputSchema().Properties["z"].Description = "also changed"
	e.OutputSchema().Properties["ok"].Type = "string"
	if shared.Input.Properties["z"].Description != `<z> & quoted "text"` || shared.Output.Properties["ok"].Type != "boolean" {
		t.Fatal("mutating public schema copies changed the shared transport schemas")
	}
	if got := openAPIResponse(t, r); !bytes.Equal(got, want) {
		t.Fatal("mutating public OpenAPI or Entry schema copies changed served JSON")
	}
}

func TestOpenAPIRequestDoesNotCloneOrMarshalEntrySchemas(t *testing.T) {
	r := handlerCacheRegistry()
	h := Handler(r, Options{})
	request := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	h.ServeHTTP(httptest.NewRecorder(), request)
	allocs := testing.AllocsPerRun(20, func() {
		h.ServeHTTP(httptest.NewRecorder(), request)
	})
	t.Logf("GET /openapi.json allocations: %.0f", allocs)
	const maxAllocs = 700
	if allocs > maxAllocs {
		t.Fatalf("OpenAPI request allocations = %.0f, want <= %d; per-request schema copying or marshaling likely returned", allocs, maxAllocs)
	}
}

func TestOpenAPIUsesCurrentMutableMetadata(t *testing.T) {
	r := handlerCacheRegistry()
	_ = openAPIResponse(t, r)
	r.Lookup("cache.read").Summary = "updated summary"
	got := string(openAPIResponse(t, r))
	if !strings.Contains(got, `"summary":"updated summary"`) {
		t.Fatalf("OpenAPI document did not reflect current entry metadata: %s", got)
	}
}
