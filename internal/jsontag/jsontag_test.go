package jsontag

import (
	"reflect"
	"testing"
)

type myPtr *int

type tagged struct {
	S   string          `json:"s,string"`
	I   int             `json:"i,omitempty,string"`
	B   bool            `json:"b,string"`
	F   float64         `json:"f,string"`
	P   *int            `json:"p,string"`
	PP  **int           `json:"pp,string"`
	NP  myPtr           `json:"np,string"`
	A   []int           `json:"a,string"`
	M   map[string]int  `json:"m,string"`
	O   struct{ X int } `json:"o,string"`
	Raw int             `json:"raw"`
}

func TestQuotedFollowsEncodingJSON(t *testing.T) {
	want := map[string]bool{"S": true, "I": true, "B": true, "F": true, "P": true,
		"PP": false, "NP": false, "A": false, "M": false, "O": false, "Raw": false}
	typ := reflect.TypeFor[tagged]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if got := Quoted(f); got != want[f.Name] {
			t.Errorf("%s: Quoted = %v, want %v", f.Name, got, want[f.Name])
		}
	}
}
