package toolkittest_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/0xble/toolkit"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/toolkittest"
)

type in struct {
	ID string `json:"id"`
}

type out struct {
	Applied bool   `json:"applied"`
	Surface string `json:"surface,omitempty"`
}

// fixture is a counter tool. broken selects one defect for the negative tests.
func fixture(broken string) toolkittest.Fixture {
	count := 0
	r := op.New("counter", "v")
	op.Add(r, op.Op[in, out]{Name: "count.get", Effect: op.Read, MCP: true, Handler: func(_ context.Context, req op.Request, _ in) (out, error) {
		o := out{}
		if broken == "parity" {
			o.Surface = string(req.Surface)
		}
		return o, nil
	}})
	for _, eff := range []op.Effect{op.Write, op.Destructive} {
		for _, now := range []bool{false, true} {
			name := "count." + string(eff)
			if now {
				name += "_now"
			}
			op.Add(r, op.Op[in, out]{Name: name, CLI: strings.NewReplacer(".", " ", "_", "-").Replace(name), Effect: eff, MCP: true, CLIImmediate: now,
				Handler: func(_ context.Context, req op.Request, _ in) (out, error) {
					noop := now && broken == "immediate_noop"
					if (req.Apply && !noop) || broken == "preview_mutates" {
						count++
					}
					return out{Applied: req.Apply}, nil
				}})
		}
	}
	if broken == "reserved_flag" {
		type versioned struct {
			Version string `json:"version" help:"Swallowed by the root --version"`
		}
		op.Add(r, op.Op[versioned, out]{Name: "count.at", Effect: op.Read,
			Handler: func(context.Context, op.Request, versioned) (out, error) { return out{}, nil }})
	}
	state := func() any { return count }
	if broken == "constant_state" {
		state = func() any { return 0 }
	}
	return toolkittest.Fixture{Registry: r, State: state}
}

func suite(broken string) toolkittest.Suite {
	cases := map[string]toolkittest.Case{
		"count.get":         {Input: map[string]any{"id": "a"}, Args: []string{"count", "get", "--id", "a"}},
		"count.write":       {Input: map[string]any{"id": "a"}, Args: []string{"count", "write", "--id", "a"}},
		"count.destructive": {Input: map[string]any{"id": "a"}, Args: []string{"count", "destructive", "--id", "a"}},
		// CLIImmediate: the kit adds --dry-run to preview and nothing to apply.
		"count.write_now":       {Input: map[string]any{"id": "a"}, Args: []string{"count", "write-now", "--id", "a"}},
		"count.destructive_now": {Input: map[string]any{"id": "a"}, Args: []string{"count", "destructive-now", "--id", "a"}},
	}
	if broken == "missing_case" {
		delete(cases, "count.destructive")
	}
	return toolkittest.Suite{
		New:     func(testing.TB) toolkittest.Fixture { return fixture(broken) },
		Options: toolkit.Options{},
		Cases:   cases,
	}
}

func TestConformingFixturePasses(t *testing.T) {
	toolkittest.Run(t, suite(""))
}

// TestBrokenFixturesFail runs each broken fixture in a child test process and
// checks that the kit fails it for the right reason.
func TestBrokenFixturesFail(t *testing.T) {
	if b := os.Getenv("TOOLKITTEST_BROKEN"); b != "" {
		toolkittest.Run(t, suite(b))
		return
	}
	for broken, want := range map[string]string{
		"preview_mutates": "preview without apply changed the state",
		"constant_state":  "CLI --apply did not change the state",
		"missing_case":    "count.destructive is destructive and has no conformance case",
		"parity":          "HTTP output vs CLI output differ",
		"immediate_noop":  "CLI without --apply (CLIImmediate) did not change the state",
		"reserved_flag":   "operation count.at: flag --version collides with the toolkit's root flag --version",
	} {
		t.Run(broken, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestBrokenFixturesFail$", "-test.count=1")
			cmd.Env = append(os.Environ(), "TOOLKITTEST_BROKEN="+broken)
			b, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the kit passed a broken fixture:\n%s", b)
			}
			if !strings.Contains(string(b), want) {
				t.Errorf("want failure %q, got:\n%s", want, b)
			}
		})
	}
}

// TestAnyJSONOutputConforms runs the kit over operations whose output may be
// any JSON value, including a destructive raw passthrough called through its
// CLI alias.
func TestAnyJSONOutputConforms(t *testing.T) {
	newFixture := func(testing.TB) toolkittest.Fixture {
		sent := 0
		r := op.New("raw", "v")
		for name, v := range map[string]any{"doc.array": []any{1, "x"}, "doc.string": "s", "doc.null": nil, "doc.object": map[string]any{"a": 1}} {
			op.Add(r, op.Op[struct{}, any]{Name: name, Effect: op.Read, MCP: true,
				Handler: func(context.Context, op.Request, struct{}) (any, error) { return v, nil }})
		}
		op.Add(r, op.Op[in, json.RawMessage]{Name: "raw", Effect: op.Destructive, MCP: true, Aliases: []string{"passthrough"},
			Handler: func(_ context.Context, req op.Request, in in) (json.RawMessage, error) {
				if !req.Apply {
					return json.RawMessage(`{"applied":false}`), nil
				}
				sent++
				return json.RawMessage(`[1,2]`), nil
			}})
		return toolkittest.Fixture{Registry: r, State: func() any { return sent }}
	}
	toolkittest.Run(t, toolkittest.Suite{
		New: newFixture,
		Cases: map[string]toolkittest.Case{
			"doc.array":  {Args: []string{"doc", "array"}},
			"doc.string": {Args: []string{"doc", "string"}},
			"doc.null":   {Args: []string{"doc", "null"}},
			"doc.object": {Args: []string{"doc", "object"}},
			"raw":        {Input: map[string]any{"id": "a"}, Args: []string{"passthrough", "--id", "a"}},
		},
	})
}

type exportIn struct {
	ID  string `json:"id"`
	Out string `json:"out,omitempty" toolkit:"cli-only"`
}

// TestCLIOnlyInputsConform runs the kit over operations whose cases set a
// cli-only input. Their CLI output differs from the remote one, which never
// carries it, and the kit checks that HTTP and MCP refuse it.
func TestCLIOnlyInputsConform(t *testing.T) {
	newFixture := func(testing.TB) toolkittest.Fixture {
		count := 0
		r := op.New("export", "v")
		for _, eff := range []op.Effect{op.Read, op.Write, op.Destructive} {
			op.Add(r, op.Op[exportIn, out]{Name: "count." + string(eff), Effect: eff, MCP: true,
				Handler: func(_ context.Context, req op.Request, in exportIn) (out, error) {
					if req.Apply {
						count++
					}
					return out{Applied: req.Apply, Surface: in.Out}, nil
				}})
		}
		return toolkittest.Fixture{Registry: r, State: func() any { return count }}
	}
	cases := map[string]toolkittest.Case{}
	for _, eff := range []string{"read", "write", "destructive"} {
		cases["count."+eff] = toolkittest.Case{Input: map[string]any{"id": "a", "out": "/tmp/x"},
			Args: []string{"count", eff, "--id", "a", "--out", "/tmp/x"}}
	}
	toolkittest.Run(t, toolkittest.Suite{New: newFixture, Cases: cases})
}
