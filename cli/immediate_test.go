package cli_test

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
)

// immediate registers a write and a destructive operation with CLIImmediate.
// Each counts how often it applied.
func immediate(applied *int) *op.Registry {
	r := op.New("t", "v1")
	for _, eff := range []op.Effect{op.Write, op.Destructive} {
		op.Add(r, op.Op[itemIn, result]{Name: "item." + string(eff), CLI: "item <id> " + string(eff), Effect: eff, CLIImmediate: true,
			Handler: func(_ context.Context, req op.Request, in itemIn) (result, error) {
				if req.Apply {
					*applied++
				}
				return result{In: in, Applied: req.Apply}, nil
			},
		})
	}
	return r
}

func TestCLIImmediateAppliesWithoutApply(t *testing.T) {
	var n int
	r := immediate(&n)
	for _, args := range [][]string{
		{"--agent", "item", "a", "write", "x"},
		{"--agent", "item", "a", "write", "x", "--apply"},
	} {
		if code, out, stderr := run(t, r, args...); code != 0 || !strings.Contains(out, `"applied": true`) {
			t.Errorf("%v applies: exit %d %s %s", args, code, out, stderr)
		}
	}
	if code, out, _ := run(t, r, "--agent", "item", "a", "write", "x", "--dry-run"); code != 0 || !strings.Contains(out, `"applied": false`) {
		t.Errorf("--dry-run previews: exit %d %s", code, out)
	}
	if n != 2 {
		t.Errorf("applied %d times, want 2", n)
	}
	if code, _, stderr := run(t, r, "item", "a", "write", "x", "--dry-run", "--apply"); code != output.ExitUsage || !strings.Contains(stderr, "--apply") {
		t.Errorf("--dry-run with --apply is a usage error: exit %d %s", code, stderr)
	}
}

func TestCLIImmediateDestructiveStillNeedsYes(t *testing.T) {
	var n int
	r := immediate(&n)
	for _, args := range [][]string{
		{"--agent", "item", "a", "destructive", "x"},
		{"--agent", "item", "a", "destructive", "x", "--apply"},
	} {
		code, _, stderr := run(t, r, args...)
		if code != output.ExitUsage || !strings.Contains(stderr, `"confirmation_required"`) {
			t.Errorf("%v without --yes (stdin is not a terminal): exit %d %s", args, code, stderr)
		}
	}
	if code, out, _ := run(t, r, "--agent", "item", "a", "destructive", "x", "--dry-run"); code != 0 || !strings.Contains(out, `"applied": false`) {
		t.Errorf("--dry-run needs no --yes: exit %d %s", code, out)
	}
	if n != 0 {
		t.Fatalf("applied %d times without confirmation", n)
	}
	if code, out, _ := run(t, r, "--agent", "-y", "item", "a", "destructive", "x"); code != 0 || !strings.Contains(out, `"applied": true`) {
		t.Errorf("--yes applies: exit %d %s", code, out)
	}
}

func TestCLIImmediateHelp(t *testing.T) {
	var n int
	// The root --yes help mentions --apply, so match the flag's own line.
	applyFlag := regexp.MustCompile(`(?m)^\s+--apply\s`)
	_, out, _ := run(t, immediate(&n), "item", "a", "write", "--help")
	if !strings.Contains(out, "--dry-run") || applyFlag.MatchString(out) {
		t.Errorf("an immediate command lists --dry-run and hides --apply:\n%s", out)
	}
	_, out, _ = run(t, registry(nil), "item", "a", "write", "--help")
	if strings.Contains(out, "--dry-run") || !applyFlag.MatchString(out) {
		t.Errorf("a default write lists --apply and has no --dry-run:\n%s", out)
	}
}

func TestRenderWithInputSeesTheCall(t *testing.T) {
	r := op.New("t", "v1")
	op.Add(r, op.Op[listIn, []string]{Name: "items.show", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in listIn) ([]string, error) {
			partial := []string{"a"}
			if in.Tag == "fail" {
				return partial, &op.Error{Kind: op.KindPartial, Code: "partial", Message: "stopped", Result: partial}
			}
			return partial, nil
		},
		RenderWithInput: func(w io.Writer, in listIn, rows []string) error {
			_, err := fmt.Fprintf(w, "%s (limit %d, tag %q)\n", strings.Join(rows, ","), in.Limit, in.Tag)
			return err
		},
	})
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"items", "show"}, 0, "a (limit 20, tag \"\")\n"},
		{[]string{"--limit", "5", "items", "show", "--tag", "x"}, 0, "a (limit 5, tag \"x\")\n"},
		{[]string{"items", "show", "--tag", "fail"}, 10, "a (limit 20, tag \"fail\")\n"},
	} {
		code, out, stderr := run(t, r, c.args...)
		if code != c.code || out != c.want {
			t.Errorf("%v: exit %d %q %s; want %d %q", c.args, code, out, stderr, c.code, c.want)
		}
	}
	if _, out, _ := run(t, r, "--json", "items", "show"); !strings.HasPrefix(out, "[") {
		t.Errorf("--json skips the hook: %q", out)
	}
}
