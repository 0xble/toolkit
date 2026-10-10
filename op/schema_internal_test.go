package op

import (
	"encoding/json"
	"reflect"
	"slices"
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
	if p == nil {
		t.Fatal("items property missing")
	}
	// A nil Go slice encodes as null, so the supported shape is array or null.
	types := slices.Clone(p.Types)
	if p.Type != "" {
		types = append(types, p.Type)
	}
	slices.Sort(types)
	if !slices.Equal(types, []string{"array", "null"}) {
		t.Fatalf("composite ,string property types = %v, want array and null", types)
	}
	if p.Items == nil || p.Items.Type != "integer" || len(p.Items.Types) != 0 {
		t.Fatalf("composite ,string items = %+v, want integer", p.Items)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		items []int
		wire  string
	}{
		{[]int{1, 2}, `{"items":[1,2]}`},
		{nil, `{"items":null}`},
	} {
		v := reflect.New(in)
		v.Elem().Field(0).Set(reflect.ValueOf(tc.items))
		b, err := json.Marshal(v.Interface())
		if err != nil || string(b) != tc.wire {
			t.Fatalf("encoding/json composite wire = %s, %v; want %s", b, err, tc.wire)
		}
		decoded := reflect.New(in)
		if err := json.Unmarshal([]byte(tc.wire), decoded.Interface()); err != nil {
			t.Fatal(err)
		}
		if got := decoded.Elem().Field(0).Interface(); !reflect.DeepEqual(got, tc.items) {
			t.Errorf("encoding/json decoded %s as %v, want %v", tc.wire, got, tc.items)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(tc.wire), &obj); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(obj); err != nil {
			t.Errorf("schema rejected supported encoding/json wire %s: %v", tc.wire, err)
		}
	}
	for _, wire := range []string{`{"items":"[1,2]"}`, `{"items":["1",2]}`} {
		if err := json.Unmarshal([]byte(wire), reflect.New(in).Interface()); err == nil {
			t.Errorf("encoding/json accepted invalid composite wire %s", wire)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(wire), &obj); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(obj); err == nil {
			t.Errorf("schema accepted invalid composite wire %s", wire)
		}
	}
}
