// Package schemacache shares the immutable schema copies used by the
// transport builders. It is internal because callers must not mutate the
// returned schemas; public Entry schema accessors continue to return copies.
package schemacache

import (
	"encoding/json"
	"runtime"
	"sync"
	"weak"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/0xble/toolkit/op"
)

type entrySchemas struct {
	once    sync.Once
	schemas Schemas
}

// Schemas contains immutable, transport-owned schema values. None of its
// fields refer back to the entry, so the cache can use weak entry keys.
type Schemas struct {
	Input  *jsonschema.Schema
	Output *jsonschema.Schema

	jsonOnce   sync.Once
	inputJSON  json.RawMessage
	outputJSON json.RawMessage
}

// JSON returns the schema encodings, marshaled once on demand. MCP server
// construction does not need JSON, so it pays only for the shared schema copies.
// The returned byte slices must not be modified.
func (s *Schemas) JSON() (input, output json.RawMessage) {
	s.jsonOnce.Do(func() {
		var err error
		if s.inputJSON, err = json.Marshal(s.Input); err != nil {
			panic(err)
		}
		if s.outputJSON, err = json.Marshal(s.Output); err != nil {
			panic(err)
		}
	})
	return s.inputJSON, s.outputJSON
}

var cache sync.Map // weak.Pointer[op.Entry] -> *entrySchemas

func deleteEntry(key weak.Pointer[op.Entry]) { cache.Delete(key) }

// Get returns one immutable schema copy pair for e. The returned values are
// for internal transport construction and must not be modified.
func Get(e *op.Entry) *Schemas {
	// Only registration writes Entry.wire and Entry.out; neither schema
	// changes with the exported, mutable operation metadata.
	key := weak.Make(e)
	value, ok := cache.Load(key)
	if !ok {
		var loaded bool
		value, loaded = cache.LoadOrStore(key, new(entrySchemas))
		if !loaded {
			// The LoadOrStore winner alone registers cleanup. Neither the key nor
			// deleteEntry captures e, and KeepAlive below protects initialization.
			runtime.AddCleanup(e, deleteEntry, key)
		}
	}
	s := value.(*entrySchemas)
	s.once.Do(func() {
		s.schemas.Input = e.InputSchema()
		s.schemas.Output = e.OutputSchema()
	})
	runtime.KeepAlive(e)
	return &s.schemas
}
