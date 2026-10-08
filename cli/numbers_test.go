package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
)

type dialIn struct {
	Level float64 `json:"level" arg:"" help:"Level"`
	Side  string  `json:"side,omitempty" help:"Side"`
	Fine  bool    `json:"fine,omitempty" help:"Fine steps"`
}

type tempIn struct {
	Value string `json:"value" arg:"" help:"Temperature or level"`
	Side  string `json:"side,omitempty" help:"Side"`
}

type boundsIn struct {
	Min   int      `json:"min,omitempty" short:"n" help:"Minimum"`
	Ratio float64  `json:"ratio,omitempty" help:"Ratio"`
	Tag   string   `json:"tag,omitempty" help:"Tag"`
	IDs   []int    `json:"ids,omitempty" name:"ids" help:"IDs"`
	Words []string `json:"words,omitempty" arg:"" optional:"" help:"Words"`
}

type digitIn struct {
	One   bool `json:"one,omitempty" short:"1" help:"A digit short flag"`
	Count int  `json:"count,omitempty" help:"Count"`
}

func numbers() *op.Registry {
	r := registry(nil)
	op.Add(r, op.Op[dialIn, result]{Name: "dial", Effect: op.Write, CLIImmediate: true,
		Handler: func(_ context.Context, req op.Request, in dialIn) (result, error) {
			return result{In: in, Applied: req.Apply}, nil
		}})
	op.Add(r, op.Op[tempIn, result]{Name: "temp", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in tempIn) (result, error) { return result{In: in}, nil }})
	op.Add(r, op.Op[boundsIn, result]{Name: "bounds", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in boundsIn) (result, error) { return result{In: in}, nil }})
	return r
}

// inOf runs args and returns the input the handler saw.
func inOf(t *testing.T, r *op.Registry, args ...string) map[string]any {
	t.Helper()
	code, out, stderr := run(t, r, args...)
	if code != 0 {
		t.Fatalf("%q: exit %d %s", args, code, stderr)
	}
	var res struct {
		In map[string]any `json:"in"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%q: %v: %s", args, err, out)
	}
	return res.In
}

func TestNegativeNumbers(t *testing.T) {
	r := numbers()
	for _, c := range []struct {
		args []string
		want map[string]any
	}{
		{[]string{"--json", "bounds", "--min", "-5"}, map[string]any{"min": -5.0}},
		{[]string{"--json", "bounds", "-n", "-5"}, map[string]any{"min": -5.0}},
		{[]string{"--json", "bounds", "--min=-5"}, map[string]any{"min": -5.0}},
		{[]string{"--json", "bounds", "--ratio", "-0.5", "--tag", "-1e3"}, map[string]any{"ratio": -0.5, "tag": "-1e3"}},
		{[]string{"--json", "bounds", "--ids", "-1", "--ids", "-2"}, map[string]any{"ids": []any{-1.0, -2.0}}},
		{[]string{"--json", "bounds", "-1", "-.5", "x"}, map[string]any{"words": []any{"-1", "-.5", "x"}}},
		{[]string{"--json", "dial", "-40", "--side", "right"}, map[string]any{"level": -40.0, "side": "right"}},
		{[]string{"--json", "dial", "--side", "right", "-40"}, map[string]any{"level": -40.0, "side": "right"}},
		{[]string{"dial", "-0.5", "-j"}, map[string]any{"level": -0.5}},
		{[]string{"dial", "--fine", "-1e3", "--agent"}, map[string]any{"level": -1000.0, "fine": true}},
		{[]string{"--json", "temp", "-40", "--side", "right"}, map[string]any{"value": "-40", "side": "right"}},
		{[]string{"--json", "temp", "--side", "right", "--", "-40"}, map[string]any{"value": "-40", "side": "right"}},
		{[]string{"--json", "temp", "--", "-foo"}, map[string]any{"value": "-foo"}},
		{[]string{"--json", "--limit", "-5", "items", "list"}, map[string]any{"limit": -5.0}},
	} {
		in := inOf(t, r, c.args...)
		for k, want := range c.want {
			if got, _ := json.Marshal(in[k]); string(got) != mustJSON(t, want) {
				t.Errorf("%q: %s = %s, want %s", c.args, k, got, mustJSON(t, want))
			}
		}
	}
}

// TestDigitShortFlag checks that a declared short flag such as -1 stays a
// flag. The check is tree-wide, since arguments are marked before kong knows
// the command.
func TestDigitShortFlag(t *testing.T) {
	r := numbers()
	op.Add(r, op.Op[digitIn, result]{Name: "digit", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in digitIn) (result, error) { return result{In: in}, nil }})
	in := inOf(t, r, "--json", "digit", "-1", "--count", "-2")
	if in["one"] != true || in["count"] != -2.0 {
		t.Errorf("digit -1 --count -2: %v", in)
	}
	if code, _, stderr := run(t, r, "bounds", "--min", "-1"); code != output.ExitUsage || !strings.Contains(stderr, "expected int value") {
		t.Errorf("-1 is a declared short flag: exit %d %s", code, stderr)
	}
}

func TestNegativeNumbersLeaveFlagsAlone(t *testing.T) {
	r := numbers()
	if code, out, _ := run(t, r, "dial", "-40", "-j", "--dry-run"); code != 0 || !strings.Contains(out, `"applied": false`) {
		t.Errorf("-j after a negative positional: exit %d %s", code, out)
	}
	if code, out, stderr := run(t, r, "--agent", "-y", "item", "a", "destructive", "-5", "--apply"); code != 0 || !strings.Contains(out, `"applied": true`) {
		t.Errorf("-y with a negative positional: exit %d %s %s", code, out, stderr)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"dial", "-40", "-x"}, "unknown flag -x"},
		{[]string{"temp", "-foo"}, "unknown flag -f"},
		{[]string{"bounds", "--min", "-x"}, "expected int value"},
		{[]string{"bounds", "--tag", "-j"}, "expected string value"},
		{[]string{"items", "list", "-40"}, `unexpected argument -40`},
	} {
		code, _, stderr := run(t, r, c.args...)
		if code != output.ExitUsage || !strings.Contains(stderr, c.want) || strings.Contains(stderr, "\x00") {
			t.Errorf("%q: exit %d %q; want usage error with %q", c.args, code, stderr, c.want)
		}
	}
}

type knobIn struct {
	Level   int    `json:"level,omitempty" default:"50" env:"CLITEST_LEVEL" help:"Level"`
	Enabled bool   `json:"enabled,omitempty" default:"true" negatable:"" help:"Enabled"`
	Label   string `json:"label,omitempty" default:"auto" help:"Label"`
	Note    string `json:"note,omitempty" arg:"" optional:"" default:"none" help:"Note"`
}

// knobView echoes a knobIn without omitempty, so zeros show.
type knobView struct {
	Level   int    `json:"level"`
	Enabled bool   `json:"enabled"`
	Label   string `json:"label"`
	Note    string `json:"note"`
}

func knobs() *op.Registry {
	r := registry(nil)
	op.Add(r, op.Op[knobIn, result]{Name: "knob", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in knobIn) (result, error) {
			return result{In: knobView(in)}, nil
		}})
	return r
}

func TestExplicitZeroKeepsItsValue(t *testing.T) {
	r := knobs()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--json", "knob"}, `{"enabled":true,"label":"auto","level":50,"note":"none"}`},
		{[]string{"--json", "knob", "--level", "0", "--enabled=false", "--label", "", ""}, `{"enabled":false,"label":"","level":0,"note":""}`},
		{[]string{"--json", "knob", "--level=0", "--no-enabled", "--label="}, `{"enabled":false,"label":"","level":0,"note":"none"}`},
		{[]string{"--json", "knob", "--level", "7", "--enabled", "--label", "x", "y"}, `{"enabled":true,"label":"x","level":7,"note":"y"}`},
	} {
		if got := mustJSON(t, inOf(t, r, c.args...)); got != c.want {
			t.Errorf("%q: %s, want %s", c.args, got, c.want)
		}
	}
	if _, out, _ := run(t, r, "--limit", "0", "items", "list"); out != "rendered limit=0\n" {
		t.Errorf("an explicit root flag 0 over an input default of 20: %q", out)
	}
	t.Setenv("CLITEST_LEVEL", "0")
	if got := mustJSON(t, inOf(t, r, "--json", "knob")["level"]); got != "0" {
		t.Errorf("a 0 from the flag's env var: %s", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
