package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
)

type globals struct {
	Limit   int    `json:"limit" help:"Page size"`
	Account string `json:"account" env:"CLITEST_ACCOUNT" help:"Account"`
	Verbose bool   `help:"Tool-owned root flag without a json tag"`
}

type paging struct {
	Limit int `json:"limit,omitempty" default:"20" help:"Page size"`
}

type listIn struct {
	paging
	Account string `json:"account,omitempty"`
	Tag     string `json:"tag,omitempty" help:"Tag"`
}

type itemIn struct {
	ID   string `json:"id" help:"Item ID"`
	Note string `json:"note" arg:"" help:"Note"`
}

type result struct {
	In      any  `json:"in"`
	Applied bool `json:"applied"`
}

type hand struct {
	Name string `arg:""`
}

func (h *hand) Run(c *cli.Context) error {
	_, err := fmt.Fprintf(c.Stdout, "hello %s json=%v ops=%d\n", h.Name, c.JSON, len(c.Registry.Entries()))
	return err
}

func registry(fail error) *op.Registry {
	r := op.New("t", "v1.2.3")
	op.Add(r, op.Op[listIn, result]{Name: "items.list", Summary: "List", Effect: op.Read,
		Handler: func(_ context.Context, req op.Request, in listIn) (result, error) {
			return result{In: in, Applied: req.Apply}, fail
		},
		Render: func(w io.Writer, r result) error {
			_, err := fmt.Fprintf(w, "rendered limit=%d\n", r.In.(listIn).Limit)
			return err
		},
	})
	for _, eff := range []op.Effect{op.Write, op.Destructive} {
		op.Add(r, op.Op[itemIn, result]{Name: "item." + string(eff), CLI: "item <id> " + string(eff), Effect: eff,
			Handler: func(_ context.Context, req op.Request, in itemIn) (result, error) {
				return result{In: in, Applied: req.Apply}, nil
			},
		})
	}
	return r
}

func run(t *testing.T, r *op.Registry, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), r, cli.Options{
		Globals: &globals{}, Commands: []cli.Command{{Name: "hello", Help: "Hand-written", Cmd: &hand{}}},
		Stdin: strings.NewReader("y\n"), Stdout: &stdout, Stderr: &stderr,
	}, args)
	return code, stdout.String(), stderr.String()
}

func TestRootFlagsBindIntoInputs(t *testing.T) {
	r := registry(nil)
	for _, args := range [][]string{
		{"--json", "--limit", "5", "items", "list"},
		{"--json", "items", "list", "--limit", "5"},
		{"items", "--limit=5", "list", "--agent"},
	} {
		code, out, stderr := run(t, r, args...)
		if code != 0 || !strings.Contains(out, `"limit": 5`) {
			t.Errorf("%v: exit %d, %s %s", args, code, out, stderr)
		}
	}
	if _, out, _ := run(t, r, "--json", "items", "list"); !strings.Contains(out, `"limit": 20`) {
		t.Errorf("without the flag the input default applies: %s", out)
	}
	t.Setenv("CLITEST_ACCOUNT", "acme")
	if _, out, _ := run(t, r, "--json", "items", "list"); !strings.Contains(out, `"account": "acme"`) {
		t.Errorf("a root flag's env var binds too: %s", out)
	}
}

func TestPlaceholdersAndApply(t *testing.T) {
	r := registry(nil)
	code, out, _ := run(t, r, "--json", "item", "abc", "write", "hi")
	if code != 0 || !strings.Contains(out, `"id": "abc"`) || !strings.Contains(out, `"note": "hi"`) || !strings.Contains(out, `"applied": false`) {
		t.Errorf("preview: %d %s", code, out)
	}
	if _, out, _ := run(t, r, "--json", "item", "abc", "write", "hi", "--apply"); !strings.Contains(out, `"applied": true`) {
		t.Errorf("--apply: %s", out)
	}
	if code, _, _ := run(t, r, "items", "list", "--apply"); code != output.ExitUsage {
		t.Errorf("reads have no --apply: exit %d", code)
	}
}

func TestDestructiveNeedsYes(t *testing.T) {
	r := registry(nil)
	code, _, stderr := run(t, r, "--agent", "item", "abc", "destructive", "x", "--apply")
	if code != output.ExitUsage || !strings.Contains(stderr, `"confirmation_required"`) {
		t.Errorf("without --yes (stdin is not a terminal, so no prompt): %d %s", code, stderr)
	}
	if _, out, _ := run(t, r, "--agent", "item", "abc", "destructive", "x"); !strings.Contains(out, `"applied": false`) {
		t.Errorf("a preview never needs --yes: %s", out)
	}
	if _, out, _ := run(t, r, "--agent", "-y", "item", "abc", "destructive", "x", "--apply"); !strings.Contains(out, `"applied": true`) {
		t.Errorf("--yes --apply: %s", out)
	}
}

func TestOutputFormats(t *testing.T) {
	r := registry(nil)
	if _, out, _ := run(t, r, "items", "list"); out != "rendered limit=20\n" {
		t.Errorf("human output uses the render hook: %q", out)
	}
	if _, out, _ := run(t, r, "items", "list", "--agent"); !strings.HasPrefix(out, "{") {
		t.Errorf("--agent is JSON: %q", out)
	}
	if _, out, _ := run(t, r, "item", "a", "write", "n"); !strings.HasPrefix(out, "{") {
		t.Errorf("no render hook means JSON: %q", out)
	}
	if _, out, _ := run(t, r, "--json", "--fields", "applied", "items", "list"); strings.TrimSpace(out) != "{\n  \"applied\": false\n}" {
		t.Errorf("--fields: %q", out)
	}
}

func TestErrorsMapToExitCodes(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
	}{
		{op.Errorf(op.KindNotFound, "nf", "missing"), 3},
		{op.Errorf(op.KindConflict, "c", "conflict"), 4},
		{op.Errorf(op.KindAuth, "a", "auth"), 5},
		{op.Errorf(op.KindRate, "r", "rate"), 6},
		{op.Errorf(op.KindPartial, "p", "partial"), 10},
		{fmt.Errorf("wrapped: %w", context.DeadlineExceeded), 7},
		{errors.New("plain"), 1},
	} {
		code, _, stderr := run(t, registry(c.err), "--agent", "items", "list")
		if code != c.code || !strings.HasPrefix(stderr, `{"error":{"code":`) {
			t.Errorf("%v: exit %d, %s; want %d", c.err, code, stderr, c.code)
		}
	}
	code, _, stderr := run(t, registry(nil), "--json", "items", "list", "--bogus")
	if code != output.ExitUsage || !strings.Contains(stderr, `"code":"usage"`) {
		t.Errorf("parse error: %d %s", code, stderr)
	}
	if code, _, stderr := run(t, registry(nil), "items", "list", "--bogus"); code != 2 || !strings.HasPrefix(stderr, "error: ") {
		t.Errorf("human parse error: %d %s", code, stderr)
	}
}

func TestHandWrittenCommandsAndBuiltins(t *testing.T) {
	r := registry(nil)
	if code, out, _ := run(t, r, "--json", "hello", "x"); code != 0 || out != "hello x json=true ops=3\n" {
		t.Errorf("hand-written command: %d %q", code, out)
	}
	if code, out, _ := run(t, r, "--version"); code != 0 || strings.TrimSpace(out) != "v1.2.3" {
		t.Errorf("--version: %d %q", code, out)
	}
	if code, out, _ := run(t, r, "--help"); code != 0 || !strings.Contains(out, "item <id> destructive") {
		t.Errorf("--help: %d %q", code, out)
	}
}

func TestValidateRejectsMistypedGlobal(t *testing.T) {
	type bad struct {
		Limit string `json:"limit"`
	}
	if err := cli.Validate(registry(nil), cli.Options{Globals: &bad{}}); err == nil {
		t.Error("a root flag bound to an input field of another type must be rejected")
	}
	if err := cli.Validate(registry(nil), cli.Options{Globals: globals{}}); err == nil {
		t.Error("Globals must be a pointer")
	}
	if err := cli.Validate(registry(nil), cli.Options{Globals: &globals{}}); err != nil {
		t.Error(err)
	}
}
