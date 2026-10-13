package op_test

import (
	"context"
	"testing"

	"github.com/0xble/toolkit/op"
)

type schemaCloneLeaf struct {
	ID      string          `json:"id"`
	Values  []string        `json:"values,omitempty"`
	Flags   map[string]bool `json:"flags,omitempty"`
	Details map[string]any  `json:"details,omitempty"`
}

type schemaCloneInput struct {
	A     schemaCloneLeaf            `json:"a"`
	B     schemaCloneLeaf            `json:"b"`
	C     schemaCloneLeaf            `json:"c"`
	D     schemaCloneLeaf            `json:"d"`
	E     schemaCloneLeaf            `json:"e"`
	F     schemaCloneLeaf            `json:"f"`
	G     schemaCloneLeaf            `json:"g"`
	H     schemaCloneLeaf            `json:"h"`
	Items []schemaCloneLeaf          `json:"items"`
	ByID  map[string]schemaCloneLeaf `json:"by_id"`
}

type schemaCloneOutput struct{}

func TestInputSchemaCloneAllocationGuard(t *testing.T) {
	reg := op.New("cost", "v")
	op.Add(reg, op.Op[schemaCloneInput, schemaCloneOutput]{
		Name:   "schema.clone",
		Effect: op.Read,
		Handler: func(context.Context, op.Request, schemaCloneInput) (schemaCloneOutput, error) {
			return schemaCloneOutput{}, nil
		},
	})
	e := reg.Lookup("schema.clone")
	_ = e.InputSchema()
	allocs := testing.AllocsPerRun(20, func() { _ = e.InputSchema() })
	const maxAllocs = 1000
	if allocs > maxAllocs {
		t.Fatalf("InputSchema allocations = %.0f, want <= %d; JSON round-tripping likely returned", allocs, maxAllocs)
	}
}
