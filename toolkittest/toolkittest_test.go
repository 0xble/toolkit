package toolkittest_test

import (
	"context"
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
		op.Add(r, op.Op[in, out]{Name: "count." + string(eff), Effect: eff, MCP: true, Handler: func(_ context.Context, req op.Request, _ in) (out, error) {
			if req.Apply || broken == "preview_mutates" {
				count++
			}
			return out{Applied: req.Apply}, nil
		}})
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
