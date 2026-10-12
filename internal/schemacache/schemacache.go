// Package schemacache shares the immutable schema copies used by the
// transport builders. It is internal because callers must not mutate the
// returned schemas; public Entry schema accessors continue to return copies.
package schemacache

import (
	"sync"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/0xble/toolkit/op"
)

type entrySchemas struct {
	once  sync.Once
	input *jsonschema.Schema
	out   *jsonschema.Schema
}

var cache sync.Map // *op.Entry -> *entrySchemas

// Get returns one immutable schema copy pair for e. The returned pointers are
// for internal transport construction and must not be modified.
func Get(e *op.Entry) (input, output *jsonschema.Schema) {
	v, _ := cache.LoadOrStore(e, new(entrySchemas))
	s := v.(*entrySchemas)
	s.once.Do(func() {
		s.input = e.InputSchema()
		s.out = e.OutputSchema()
	})
	return s.input, s.out
}
