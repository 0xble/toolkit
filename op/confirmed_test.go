package op_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

// TestCLIConfirmedIsMetadataOnly checks that CLIConfirmed shows in the
// metadata, leaves the wire schema alone, and changes nothing on the remote
// path: a destructive apply still needs confirm on every surface but the
// CLI, whose adapter supplies it.
func TestCLIConfirmedIsMetadataOnly(t *testing.T) {
	var n int
	r := op.New("t", "v")
	op.Add(r, op.Op[idIn, res]{Name: "item.send_now", CLI: "item send-now", Effect: op.Destructive, CLIImmediate: true, Handler: handler[idIn](&n)})
	op.Add(r, op.Op[idIn, res]{Name: "item.send", CLI: "item send", Effect: op.Destructive, CLIImmediate: true, CLIConfirmed: true, Handler: handler[idIn](&n)})
	b, err := json.Marshal(r.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMetadata(t, b)
	for _, o := range r.Metadata().Operations {
		if o.CLIConfirmed != (o.Name == "item.send") || !o.CLIImmediate {
			t.Errorf("%s: cli_confirmed %v cli_immediate %v", o.Name, o.CLIConfirmed, o.CLIImmediate)
		}
	}
	if strings.Count(string(b), `"cli_confirmed":true`) != 1 {
		t.Errorf("cli_confirmed is omitted unless set: %s", b)
	}
	e := r.Lookup("item.send")
	pb, _ := json.Marshal(r.Lookup("item.send_now").InputSchema())
	cb, _ := json.Marshal(e.InputSchema())
	if string(pb) != string(cb) || !strings.Contains(string(cb), `"confirm"`) {
		t.Errorf("the wire schema must not depend on CLIConfirmed and keeps confirm:\n%s\n%s", pb, cb)
	}
	ctx := context.Background()
	for _, s := range []op.Surface{op.SurfaceHTTP, op.SurfaceMCP} {
		_, err := e.CallJSON(ctx, op.Request{Surface: s}, json.RawMessage(`{"id":"a","apply":true}`), op.AllowAll)
		if op.AsError(err, op.KindError).Code != "confirmation_required" {
			t.Errorf("%s: an apply without confirm is refused: %v", s, err)
		}
		_, err = e.Call(ctx, op.Request{Surface: s, Apply: true}, &idIn{ID: "a"})
		if op.AsError(err, op.KindError).Code != "confirmation_required" {
			t.Errorf("%s: Call does not supply confirm: %v", s, err)
		}
	}
	if n != 0 {
		t.Fatalf("the handler ran %d times without confirm", n)
	}
	_, err = e.CallJSON(ctx, op.Request{Surface: op.SurfaceHTTP}, json.RawMessage(`{"id":"a","apply":true,"confirm":true}`), nil)
	if op.AsError(err, op.KindError).Code != "write_not_authorized" {
		t.Errorf("the served default still refuses applied writes: %v", err)
	}
	out, err := e.CallJSON(ctx, op.Request{Surface: op.SurfaceMCP}, json.RawMessage(`{"id":"a","apply":true,"confirm":true}`), op.AllowAll)
	if err != nil || !out.(res).Applied {
		t.Errorf("apply and confirm apply: %v %v", out, err)
	}
}

func TestAddRejectsMisplacedCLIConfirmed(t *testing.T) {
	var n int
	for _, c := range []struct {
		effect    op.Effect
		immediate bool
	}{{op.Read, false}, {op.Write, false}, {op.Write, true}, {op.Destructive, false}} {
		t.Run(fmt.Sprintf("%s immediate=%v", c.effect, c.immediate), func(t *testing.T) {
			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, "CLIConfirmed needs a destructive effect and CLIImmediate") {
					t.Errorf("Add panic: %q", msg)
				}
			}()
			op.Add(op.New("t", "v"), op.Op[idIn, res]{Name: "a", Effect: c.effect, CLIImmediate: c.immediate, CLIConfirmed: true, Handler: handler[idIn](&n)})
		})
	}
}

func TestMetadataSchemaRestrictsCLIConfirmed(t *testing.T) {
	var s jsonschema.Schema
	if err := json.Unmarshal(op.MetadataSchema(), &s); err != nil {
		t.Fatal(err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for flags, valid := range map[string]bool{
		`"effect":"destructive","cli_immediate":true,"cli_confirmed":true`: true,
		`"effect":"destructive","cli_confirmed":true`:                      false,
		`"effect":"write","cli_immediate":true,"cli_confirmed":true`:       false,
		`"effect":"read","cli_confirmed":true`:                             false,
	} {
		doc := `{"schema":"toolkit.metadata.v1","tool":"t","version":"v","operations":[{"name":"a","cli":"a","summary":"",` +
			flags + `,"mcp":false,"mcp_name":"a","input":{"type":"object"},"output":{}}]}`
		var v any
		if err := json.Unmarshal([]byte(doc), &v); err != nil {
			t.Fatal(err)
		}
		if err := rs.Validate(v); (err == nil) != valid {
			t.Errorf("%s: valid %v, want %v (%v)", flags, err == nil, valid, err)
		}
	}
}
