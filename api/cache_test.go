package api

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/0xble/toolkit/op"
)

type handlerCacheInput struct {
	A string `json:"a"`
	B string `json:"b"`
	C string `json:"c"`
	D string `json:"d"`
	E string `json:"e"`
	F string `json:"f"`
	G string `json:"g"`
	H string `json:"h"`
	I string `json:"i"`
	J string `json:"j"`
	K string `json:"k"`
	L string `json:"l"`
}

type handlerCacheOutput struct {
	OK bool `json:"ok"`
}

func handlerCacheRegistry() *op.Registry {
	r := op.New("handler-cache", "v1")
	op.Add(r, op.Op[handlerCacheInput, handlerCacheOutput]{
		Name:   "cache.read",
		Effect: op.Read,
		Handler: func(context.Context, op.Request, handlerCacheInput) (handlerCacheOutput, error) {
			return handlerCacheOutput{OK: true}, nil
		},
	})
	return r
}

func TestHandlerCachesOpenAPIDocumentPerRegistry(t *testing.T) {
	r := handlerCacheRegistry()
	first := cachedJSON(r)
	second := cachedJSON(r)
	if !reflect.ValueOf(first).IsValid() || &first[0] != &second[0] {
		t.Fatal("OpenAPI JSON was rebuilt for the same registry")
	}
	other := handlerCacheRegistry()
	var otherDoc map[string]any
	if err := json.Unmarshal(cachedJSON(other), &otherDoc); err != nil {
		t.Fatal(err)
	}
	if got := otherDoc["info"].(map[string]any)["title"]; got != other.Tool {
		t.Fatalf("distinct registries share an OpenAPI document: %v", got)
	}

	fresh := OpenAPI(r)
	fresh["info"].(map[string]any)["title"] = "changed"
	var cachedDoc map[string]any
	if err := json.Unmarshal(cachedJSON(r), &cachedDoc); err != nil {
		t.Fatal(err)
	}
	if got := cachedDoc["info"].(map[string]any)["title"]; got != r.Tool {
		t.Fatalf("mutating exported OpenAPI changed cached document: %v", got)
	}
}

func TestHandlerConstructionDoesNotCloneSchemasPerCall(t *testing.T) {
	r := handlerCacheRegistry()
	_ = Handler(r, Options{}) // populate the per-registry document cache
	allocs := testing.AllocsPerRun(20, func() { _ = Handler(r, Options{}) })
	const maxAllocs = 500
	if allocs > maxAllocs {
		t.Fatalf("Handler allocations = %.0f, want <= %d; per-handler schema cloning likely returned", allocs, maxAllocs)
	}

	exportedJSON, err := json.Marshal(OpenAPI(r))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cachedJSON(r), exportedJSON) {
		t.Fatal("cached and exported OpenAPI documents differ")
	}
}
