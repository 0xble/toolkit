package op_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type Source struct {
	File string `json:"file,omitempty" arg:"" optional:"" help:"File to read" toolkit:"cli-only"`
}

type exportIn struct {
	Source
	ID      string `json:"id"`
	Out     string `json:"out,omitempty" help:"File to write" toolkit:"cli-only"`
	IDsFrom string `json:"ids_from,omitempty" toolkit:"cli-only"`
}

func TestCLIOnlyRejectsBadDeclarations(t *testing.T) {
	cases := map[string]struct {
		add  func(r *op.Registry)
		want string
	}{
		"required": {func(r *op.Registry) {
			type in struct {
				Path string `json:"path" toolkit:"cli-only"`
			}
			op.Add(r, op.Op[in, res]{Name: "a", Effect: op.Read, Handler: handler[in](new(int))})
		}, "must be optional"},
		"default": {func(r *op.Registry) {
			type in struct {
				Path string `json:"path,omitempty" default:"out.json" toolkit:"cli-only"`
			}
			op.Add(r, op.Op[in, res]{Name: "a", Effect: op.Read, Handler: handler[in](new(int))})
		}, "must not have a default"},
		"unknown tag": {func(r *op.Registry) {
			type in struct {
				Path string `json:"path,omitempty" toolkit:"cli_only"`
			}
			op.Add(r, op.Op[in, res]{Name: "a", Effect: op.Read, Handler: handler[in](new(int))})
		}, `unknown toolkit tag "cli_only"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if msg := fmt.Sprint(recover()); !strings.Contains(msg, c.want) {
					t.Errorf("Add panicked with %q, want it to mention %q", msg, c.want)
				}
			}()
			c.add(op.New("t", "v"))
		})
	}
}

func TestCLIOnlyInputs(t *testing.T) {
	var calls int
	r := op.New("t", "v")
	op.Add(r, op.Op[exportIn, res]{Name: "item.export", Effect: op.Destructive, MCP: true, Handler: handler[exportIn](&calls)})
	e := r.Lookup("item.export")
	if want := []string{"file", "ids_from", "out"}; !slices.Equal(e.CLIOnlyInputs, want) {
		t.Errorf("CLIOnlyInputs = %v, want %v", e.CLIOnlyInputs, want)
	}
	props := e.InputSchema().Properties
	for _, name := range e.CLIOnlyInputs {
		if _, ok := props[name]; ok {
			t.Errorf("the wire schema has cli-only input %s", name)
		}
	}
	if _, ok := props["id"]; !ok {
		t.Error("the wire schema lost id")
	}
	b, err := json.Marshal(r.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	toolkittest.CheckMetadata(t, b)
	if !strings.Contains(string(b), `"cli_only_inputs":["file","ids_from","out"]`) {
		t.Errorf("metadata does not list the cli-only inputs: %s", b)
	}

	ctx := context.Background()
	for raw, flag := range map[string]string{
		`{"id":"a","out":"/etc/x"}`:      "--out",
		`{"id":"a","ids_from":"/etc/x"}`: "--i-ds-from",
		`{"id":"a","file":"/etc/x"}`:     "<file>",
	} {
		for _, s := range []op.Surface{op.SurfaceHTTP, op.SurfaceMCP, ""} {
			// Refused before confirmation and authorization, even for an
			// applied destructive call under the served default.
			body := strings.TrimSuffix(raw, "}") + `,"apply":true}`
			_, err := e.CallJSON(ctx, op.Request{Surface: s}, json.RawMessage(body), nil)
			oe := op.AsError(err, op.KindError)
			if oe.Kind != op.KindUsage || oe.Code != "cli_only" ||
				oe.Message != flag+" names a local path and is accepted only on the command line" {
				t.Errorf("%s %s: %v %q, want usage cli_only for %s", s, raw, oe.Code, oe.Message, flag)
			}
		}
		in, _, _, err := e.Decode(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Call(ctx, op.Request{Surface: op.SurfaceMCP, Apply: true, Confirm: true}, in); op.AsError(err, op.KindError).Code != "cli_only" {
			t.Errorf("Call on MCP with %s: %v, want cli_only", raw, err)
		}
		if out, err := e.Call(ctx, op.Request{Surface: op.SurfaceCLI, Apply: true, Confirm: true}, in); err != nil || !out.(res).Applied {
			t.Errorf("Call on the CLI with %s: %v %v, want applied", raw, out, err)
		}
	}
	if calls != 3 {
		t.Errorf("handler ran %d times, want 3: only the CLI calls", calls)
	}
	out, err := e.CallJSON(ctx, op.Request{Surface: op.SurfaceHTTP}, json.RawMessage(`{"id":"a","out":""}`), nil)
	if err != nil || out.(res).Applied {
		t.Errorf("an empty cli-only input is not set, so the remote preview runs: %v %v", out, err)
	}
}
