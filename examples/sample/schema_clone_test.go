package main

import (
	"encoding/json"
	"testing"
)

func TestRegistrySchemasCloneJSONMatches(t *testing.T) {
	reg, _ := New()
	for _, e := range reg.Entries() {
		for _, kind := range []string{"input", "output"} {
			var schema any
			switch kind {
			case "input":
				schema = e.InputSchema()
			case "output":
				schema = e.OutputSchema()
			}
			want, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("%s %s: marshal source schema: %v", e.Name, kind, err)
			}
			copySchema := e.InputSchema()
			if kind == "output" {
				copySchema = e.OutputSchema()
			}
			got, err := json.Marshal(copySchema)
			if err != nil {
				t.Fatalf("%s %s: marshal copied schema: %v", e.Name, kind, err)
			}
			if string(got) != string(want) {
				t.Errorf("%s %s schema JSON differs:\n got: %s\nwant: %s", e.Name, kind, got, want)
			}
		}
	}
}
