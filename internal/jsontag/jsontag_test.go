package jsontag

import (
	"reflect"
	"testing"
)

type myPtr *int

type tagged struct {
	S   string  `json:"s,string"`
	I   int     `json:"i,omitempty,string"`
	B   bool    `json:"b,string"`
	F   float64 `json:"f,string"`
	P   *int    `json:"p,string"`
	Raw int     `json:"raw"`
}

func TestQuotedFollowsEncodingJSON(t *testing.T) {
	want := map[string]bool{"S": true, "I": true, "B": true, "F": true, "P": true, "Raw": false}
	typ := reflect.TypeFor[tagged]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if got := Quoted(f); got != want[f.Name] {
			t.Errorf("%s: Quoted = %v, want %v", f.Name, got, want[f.Name])
		}
	}
}

// TestQuotedIgnoresOtherTypes covers the types encoding/json ignores
// ",string" on. The fields are built at run time because staticcheck rejects
// those tags in source (SA5008), which is the very misuse being tested.
func TestQuotedIgnoresOtherTypes(t *testing.T) {
	for name, typ := range map[string]reflect.Type{
		"pointer to pointer": reflect.TypeFor[**int](),
		"named pointer":      reflect.TypeFor[myPtr](),
		"slice":              reflect.TypeFor[[]int](),
		"map":                reflect.TypeFor[map[string]int](),
		"struct":             reflect.TypeFor[struct{ X int }](),
	} {
		f := reflect.StructField{Name: "V", Type: typ, Tag: `json:"v,string"`}
		if Quoted(f) {
			t.Errorf("%s: Quoted = true, want false", name)
		}
	}
}
