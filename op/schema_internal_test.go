package op

import (
	"reflect"
	"testing"
)

// TestSchemaIgnoresJSONStringOnCompositeTypes checks that ",string" on a
// type encoding/json ignores it for leaves the schema unchanged. The input
// type is built at run time because staticcheck rejects that tag in source
// (SA5008), which is the very misuse being tested.
func TestSchemaIgnoresJSONStringOnCompositeTypes(t *testing.T) {
	in := reflect.StructOf([]reflect.StructField{{
		Name: "Items", Type: reflect.TypeFor[[]int](), Tag: `json:"items,string"`,
	}})
	s, err := schemaFor(in)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Properties["items"]
	if p == nil || p.Type == "string" || p.Items == nil {
		t.Fatalf("composite ,string tag changed the schema: %+v", p)
	}
}
