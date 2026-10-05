package op_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

// TestCLIImmediateIsMetadataOnly checks that CLIImmediate shows in the
// metadata, leaves the wire schema alone, and changes nothing on the remote
// path: apply is still required, and a destructive apply still needs
// confirm.
func TestCLIImmediateIsMetadataOnly(t *testing.T) {
	var n int
	r := op.New("t", "v")
	for _, eff := range []op.Effect{op.Write, op.Destructive} {
		for _, now := range []bool{false, true} {
			name := "item." + string(eff)
			if now {
				name += "_now"
			}
			op.Add(r, op.Op[idIn, res]{Name: name, CLI: strings.NewReplacer(".", " ", "_", "-").Replace(name), Effect: eff, CLIImmediate: now, Handler: handler[idIn](&n)})
		}
	}
	b, err := json.Marshal(r.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMetadata(t, b)
	for _, o := range r.Metadata().Operations {
		if o.CLIImmediate != strings.HasSuffix(o.Name, "_now") {
			t.Errorf("%s: cli_immediate %v", o.Name, o.CLIImmediate)
		}
	}
	if strings.Count(string(b), `"cli_immediate":true`) != 2 {
		t.Errorf("cli_immediate is omitted unless set: %s", b)
	}
	for _, eff := range []string{"write", "destructive"} {
		plain, now := r.Lookup("item."+eff), r.Lookup("item."+eff+"_now")
		pb, _ := json.Marshal(plain.InputSchema())
		nb, _ := json.Marshal(now.InputSchema())
		if string(pb) != string(nb) {
			t.Errorf("%s: the wire schema must not depend on CLIImmediate:\n%s\n%s", eff, pb, nb)
		}
		out, err := now.CallJSON(context.Background(), op.Request{Surface: op.SurfaceHTTP}, json.RawMessage(`{"id":"a"}`), op.AllowAll)
		if err != nil || out.(res).Applied {
			t.Errorf("%s: without apply the remote path previews: %v %v", eff, out, err)
		}
	}
	_, err = r.Lookup("item.destructive_now").CallJSON(context.Background(), op.Request{}, json.RawMessage(`{"id":"a","apply":true}`), op.AllowAll)
	if op.AsError(err, op.KindError).Code != "confirmation_required" {
		t.Errorf("a destructive apply still needs confirm: %v", err)
	}
	_, err = r.Lookup("item.write_now").CallJSON(context.Background(), op.Request{}, json.RawMessage(`{"id":"a","apply":true}`), nil)
	if op.AsError(err, op.KindError).Code != "write_not_authorized" {
		t.Errorf("the served default still refuses applied writes: %v", err)
	}
}

func TestMetadataSchemaRejectsImmediateRead(t *testing.T) {
	doc := `{"schema":"toolkit.metadata.v1","tool":"t","version":"v","operations":[{"name":"a","cli":"a","summary":"","effect":"read","mcp":false,"mcp_name":"a","cli_immediate":true,"input":{"type":"object"},"output":{}}]}`
	var s jsonschema.Schema
	if err := json.Unmarshal(op.MetadataSchema(), &s); err != nil {
		t.Fatal(err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for eff, valid := range map[string]bool{"read": false, "write": true, "destructive": true} {
		var v any
		if err := json.Unmarshal([]byte(strings.Replace(doc, `"read"`, `"`+eff+`"`, 1)), &v); err != nil {
			t.Fatal(err)
		}
		if err := rs.Validate(v); (err == nil) != valid {
			t.Errorf("cli_immediate on %s: valid %v, want %v (%v)", eff, err == nil, valid, err)
		}
	}
}

func TestRenderWithInput(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[listIn, res]{Name: "items.list", Effect: op.Read,
		Handler: func(context.Context, op.Request, listIn) (res, error) { return res{}, nil },
		RenderWithInput: func(w io.Writer, in listIn, _ res) error {
			_, err := fmt.Fprintf(w, "limit=%d tag=%q", in.Limit, in.Tag)
			return err
		},
	})
	e := r.Lookup("items.list")
	in, _, _, err := e.Decode(json.RawMessage(`{"tag":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		in   any
		want string
	}{
		{in, `limit=20 tag="x"`},
		{listIn{Limit: 3}, `limit=3 tag=""`},
		{nil, `limit=0 tag=""`},
	} {
		var b bytes.Buffer
		if err := e.RenderWithInput(&b, c.in, res{}); err != nil || b.String() != c.want {
			t.Errorf("RenderWithInput(%#v): %q %v, want %q", c.in, b.String(), err, c.want)
		}
	}
	var b bytes.Buffer
	if err := e.Render(&b, res{}); err != nil || b.String() != `limit=0 tag=""` {
		t.Errorf("Render passes the zero input: %q %v", b.String(), err)
	}
	if err := e.RenderWithInput(&b, idIn{}, res{}); err == nil {
		t.Error("an input of the wrong type must be an error")
	}
	if !e.CanRender() {
		t.Error("CanRender is true for RenderWithInput")
	}
}
