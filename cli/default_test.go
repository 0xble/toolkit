package cli_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/op"
)

type showIn struct {
	ID      string `json:"id" help:"Item ID"`
	Version int    `json:"version,omitempty" name:"at-version" help:"Saved version"`
}

type report struct {
	Done int `json:"done"`
}

func defaults() *op.Registry {
	r := op.New("t", "v1")
	op.Add(r, op.Op[listIn, result]{Name: "items.list", CLI: "items list", Summary: "List items", Effect: op.Read, DefaultCommand: true,
		Handler: func(_ context.Context, _ op.Request, in listIn) (result, error) { return result{In: in}, nil },
	})
	op.Add(r, op.Op[listIn, report]{Name: "items.fix", CLI: "items fix", Summary: "Fix items", Effect: op.Write,
		Handler: func(_ context.Context, req op.Request, _ listIn) (report, error) {
			if !req.Apply {
				return report{}, nil
			}
			partial := report{Done: 1}
			return partial, &op.Error{Kind: op.KindPartial, Code: "stopped", Message: "stopped after 1", Result: partial}
		},
		Render: func(w io.Writer, r report) error {
			_, err := fmt.Fprintf(w, "done %d\n", r.Done)
			return err
		},
	})
	op.Add(r, op.Op[showIn, result]{Name: "item.show", CLI: "item <id> show", Summary: "Show one item", Effect: op.Read, DefaultCommand: true,
		Handler: func(_ context.Context, _ op.Request, in showIn) (result, error) { return result{In: in}, nil },
	})
	op.Add(r, op.Op[showIn, result]{Name: "item.history", CLI: "item <id> history", Effect: op.Read,
		Handler: func(_ context.Context, _ op.Request, in showIn) (result, error) { return result{In: in}, nil },
	})
	return r
}

func TestDefaultCommand(t *testing.T) {
	r := defaults()
	for _, args := range [][]string{
		{"--json", "items"},
		{"--json", "items", "list"},
		{"--limit", "5", "items", "--json"},
	} {
		if code, out, stderr := run(t, r, args...); code != 0 || !strings.Contains(out, `"limit"`) {
			t.Errorf("%v: exit %d %s %s", args, code, out, stderr)
		}
	}
	if _, out, _ := run(t, r, "--limit", "5", "items", "--json"); !strings.Contains(out, `"limit": 5`) {
		t.Errorf("root flags still bind through a default command: %s", out)
	}
	for _, args := range [][]string{
		{"--json", "item", "a1"},
		{"--json", "item", "a1", "show"},
		{"--json", "item", "a1", "--at-version", "3"},
	} {
		if code, out, stderr := run(t, r, args...); code != 0 || !strings.Contains(out, `"id": "a1"`) {
			t.Errorf("%v: exit %d %s %s", args, code, out, stderr)
		}
	}
	if _, out, _ := run(t, r, "--json", "item", "a1", "--at-version", "3"); !strings.Contains(out, `"version": 3`) {
		t.Errorf("a default command takes its own flags: %s", out)
	}
	if _, out, _ := run(t, r, "--json", "item", "a1", "history"); !strings.Contains(out, `"id": "a1"`) {
		t.Errorf("sibling subcommands still run: %s", out)
	}
	_, help, _ := run(t, r, "--help")
	if strings.Contains(help, "items list") || !strings.Contains(help, "items fix") {
		t.Errorf("the default word is hidden from help, its siblings are not:\n%s", help)
	}
}

func TestErrorResultIsPrintedWithTheError(t *testing.T) {
	r := defaults()
	code, out, stderr := run(t, r, "items", "fix", "--apply")
	if code != 10 || out != "done 1\n" || !strings.HasPrefix(stderr, "error: stopped after 1") {
		t.Errorf("human: exit %d stdout %q stderr %q", code, out, stderr)
	}
	code, out, stderr = run(t, r, "--json", "items", "fix", "--apply")
	if code != 10 || strings.TrimSpace(out) != "{\n  \"done\": 1\n}" || !strings.Contains(stderr, `"code":"stopped"`) {
		t.Errorf("json: exit %d stdout %q stderr %q", code, out, stderr)
	}
}

func TestDefaultCommandValidation(t *testing.T) {
	h := func(context.Context, op.Request, listIn) (result, error) { return result{}, nil }
	two := op.New("t", "v")
	op.Add(two, op.Op[listIn, result]{Name: "a.one", CLI: "a one", Effect: op.Read, DefaultCommand: true, Handler: h})
	op.Add(two, op.Op[listIn, result]{Name: "a.two", CLI: "a two", Effect: op.Read, DefaultCommand: true, Handler: h})
	if err := cli.Validate(two, cli.Options{}); err == nil || !strings.Contains(err.Error(), "same parent") {
		t.Errorf("two defaults under one parent: %v", err)
	}
	beside := op.New("t", "v")
	op.Add(beside, op.Op[listIn, result]{Name: "a.list", CLI: "a list", Effect: op.Read, DefaultCommand: true, Handler: h})
	op.Add(beside, op.Op[showIn, result]{Name: "a.show", CLI: "a <id> show", Effect: op.Read,
		Handler: func(context.Context, op.Request, showIn) (result, error) { return result{}, nil }})
	if err := cli.Validate(beside, cli.Options{}); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("a default next to a placeholder: %v", err)
	}
}
