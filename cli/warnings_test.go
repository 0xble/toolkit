package cli_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/0xble/toolkit/op"
)

type warned struct {
	Rows     []string `json:"rows"`
	Warnings []string `json:"warnings"`
}

func warnings(render bool) *op.Registry {
	r := op.New("t", "v1")
	o := op.Op[struct{}, warned]{Name: "rows", Effect: op.Read,
		Handler: func(context.Context, op.Request, struct{}) (warned, error) {
			return warned{Rows: []string{"a"}, Warnings: []string{"filtered locally", "slow"}}, nil
		},
		Warnings: func(w warned) []string { return w.Warnings },
	}
	if render {
		o.Render = func(w io.Writer, v warned) error {
			_, err := fmt.Fprintln(w, strings.Join(v.Rows, ","))
			return err
		}
	}
	op.Add(r, o)
	op.Add(r, op.Op[struct{}, warned]{Name: "fix", Effect: op.Write,
		Handler: func(context.Context, op.Request, struct{}) (warned, error) {
			partial := warned{Warnings: []string{"stopped early"}}
			return partial, &op.Error{Kind: op.KindPartial, Code: "stopped", Message: "stopped", Result: partial}
		},
		Warnings: func(w warned) []string { return w.Warnings },
	})
	return r
}

func TestWarningsGoToStderrInHumanOutput(t *testing.T) {
	code, out, stderr := run(t, warnings(true), "rows")
	if code != 0 || out != "a\n" || stderr != "warning: filtered locally\nwarning: slow\n" {
		t.Errorf("human: exit %d stdout %q stderr %q", code, out, stderr)
	}
	code, out, stderr = run(t, warnings(false), "rows")
	if code != 0 || !strings.Contains(out, `"filtered locally"`) || stderr != "warning: filtered locally\nwarning: slow\n" {
		t.Errorf("human without a render hook: exit %d stdout %q stderr %q", code, out, stderr)
	}
	for _, flag := range []string{"--json", "--agent"} {
		code, out, stderr = run(t, warnings(true), flag, "rows")
		if code != 0 || !strings.Contains(out, `"filtered locally"`) || stderr != "" {
			t.Errorf("%s: warnings stay in the result only: exit %d stdout %q stderr %q", flag, code, out, stderr)
		}
	}
	code, _, stderr = run(t, warnings(true), "fix")
	if code != 10 || !strings.HasPrefix(stderr, "warning: stopped early\nerror: stopped") {
		t.Errorf("an error result's warnings come before the error: exit %d stderr %q", code, stderr)
	}
}
