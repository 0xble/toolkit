package op_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

func TestAliasesAreValidated(t *testing.T) {
	var n int
	get := op.Op[idIn, res]{Name: "item.get", CLI: "item <id> get", Effect: op.Read, Handler: handler[idIn](&n)}
	cases := map[string]func(r *op.Registry){
		"bad word":     func(r *op.Registry) { o := get; o.Aliases = []string{"G"}; op.Add(r, o) },
		"placeholder":  func(r *op.Registry) { o := get; o.Aliases = []string{"<id>"}; op.Add(r, o) },
		"same as word": func(r *op.Registry) { o := get; o.Aliases = []string{"get"}; op.Add(r, o) },
		"repeated":     func(r *op.Registry) { o := get; o.Aliases = []string{"g", "g"}; op.Add(r, o) },
		"sibling word": func(r *op.Registry) {
			op.Add(r, get)
			o := get
			o.Name = "item.show"
			o.CLI = "item <id> show"
			o.Aliases = []string{"get"}
			op.Add(r, o)
		},
		"sibling alias": func(r *op.Registry) {
			o := get
			o.Aliases = []string{"g"}
			op.Add(r, o)
			o.Name = "item.show"
			o.CLI = "item <id> show"
			op.Add(r, o)
		},
		"word of later": func(r *op.Registry) {
			o := get
			o.Aliases = []string{"show"}
			op.Add(r, o)
			o.Name = "item.show"
			o.CLI = "item show"
			o.Aliases = nil
			op.Add(r, o)
		},
		"group prefix": func(r *op.Registry) {
			o := get
			o.Name = "items"
			o.CLI = "items"
			o.Aliases = []string{"item"}
			op.Add(r, get)
			op.Add(r, o)
		},
	}
	for name, add := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Add did not panic")
				}
			}()
			add(op.New("t", "v"))
		})
	}
}

func TestAliasesAreCLIOnly(t *testing.T) {
	var n int
	r := op.New("t", "v")
	op.Add(r, op.Op[idIn, res]{Name: "item.get", CLI: "item <id> get", Effect: op.Read, MCP: true, Aliases: []string{"g", "show"}, Handler: handler[idIn](&n)})
	op.Add(r, op.Op[idIn, res]{Name: "item.list", CLI: "items", Effect: op.Read, Handler: handler[idIn](&n)})
	e := r.Lookup("item.get")
	if e.MCPName() != "item_get" || e.CLI() != "item <id> get" || e.CommandKey() != "item get" || r.Lookup("g") != nil {
		t.Errorf("aliases changed the operation identity: %s %s %s", e.MCPName(), e.CLI(), e.CommandKey())
	}
	b, err := json.Marshal(r.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMetadata(t, b)
	if m := r.Metadata(); strings.Join(m.Operations[0].Aliases, ",") != "g,show" {
		t.Errorf("metadata does not carry the aliases: %+v", m.Operations[0])
	}
	if strings.Count(string(b), `"aliases"`) != 1 {
		t.Errorf("an operation without aliases has no aliases key: %s", b)
	}
}
