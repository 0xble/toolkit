package op_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

const anyJSONSchema = `{"type":["object","array","string","number","boolean","null"]}`

type withRaw struct {
	Raw  json.RawMessage   `json:"raw"`
	Ptr  *json.RawMessage  `json:"ptr,omitempty"`
	List []json.RawMessage `json:"list"`
}

func addOut[Out any](r *op.Registry, name string, v Out) {
	op.Add(r, op.Op[struct{}, Out]{Name: name, Effect: op.Read, MCP: true,
		Handler: func(context.Context, op.Request, struct{}) (Out, error) { return v, nil }})
}

func TestAnyJSONOutputSchemas(t *testing.T) {
	r := op.New("t", "v")
	addOut[any](r, "any", nil)
	addOut[json.RawMessage](r, "raw", nil)
	addOut[map[string]any](r, "object", nil)
	addOut[withRaw](r, "nested", withRaw{})
	want := map[string]string{
		"any":    anyJSONSchema,
		"raw":    anyJSONSchema,
		"object": `{"type":"object","additionalProperties":true}`,
	}
	for name, w := range want {
		if b, _ := json.Marshal(r.Lookup(name).OutputSchema()); !toolkittest.SameJSON(b, []byte(w)) {
			t.Errorf("%s: output schema %s, want %s", name, b, w)
		}
	}
	nested := r.Lookup("nested").OutputSchema()
	for _, f := range []string{"raw", "ptr"} {
		if b, _ := json.Marshal(nested.Properties[f]); !toolkittest.SameJSON(b, []byte(anyJSONSchema)) {
			t.Errorf("nested %s: %s, want %s", f, b, anyJSONSchema)
		}
	}
	if b, _ := json.Marshal(nested.Properties["list"].Items); !toolkittest.SameJSON(b, []byte(anyJSONSchema)) {
		t.Errorf("nested list items: %s", b)
	}

	rs, err := r.Lookup("any").OutputSchema().Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{`{"a":1}`, `[1,"x"]`, `"s"`, `1.5`, `3`, `true`, `null`} {
		var x any
		_ = json.Unmarshal([]byte(v), &x)
		if err := rs.Validate(x); err != nil {
			t.Errorf("any schema rejects %s: %v", v, err)
		}
	}

	b, err := json.Marshal(r.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMetadata(t, b)
}

// TestCourtlistenerRepro is the reproduction from 0xble/courtlistener#1:
// an any output returning an array failed toolkit.metadata.v1.
func TestCourtlistenerRepro(t *testing.T) {
	reg := op.New("repro", "dev")
	op.Add(reg, op.Op[struct{}, any]{Name: "doc", Effect: op.Read,
		Handler: func(context.Context, op.Request, struct{}) (any, error) { return []any{1, "x"}, nil }})
	b, _ := json.Marshal(reg.Metadata())
	toolkittest.CheckMetadata(t, b)
}

func TestRenderAcceptsANilAnyOutput(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[struct{}, any]{Name: "doc", Effect: op.Read,
		Handler: func(context.Context, op.Request, struct{}) (any, error) { return nil, nil },
		Render: func(w io.Writer, v any) error {
			_, err := fmt.Fprintf(w, "%v;", v)
			return err
		}})
	var buf bytes.Buffer
	for _, v := range []any{nil, "x"} {
		if err := r.Lookup("doc").Render(&buf, v); err != nil {
			t.Errorf("render %v: %v", v, err)
		}
	}
	if buf.String() != "<nil>;x;" {
		t.Errorf("rendered %q", buf.String())
	}
}
