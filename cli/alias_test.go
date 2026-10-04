package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/op"
)

type queryIn struct {
	Query string `json:"query" arg:"" help:"Search text"`
}

func aliases() *op.Registry {
	r := op.New("t", "v1")
	op.Add(r, op.Op[queryIn, result]{Name: "search", Summary: "Search", Effect: op.Read, Aliases: []string{"s", "find"},
		Handler: func(_ context.Context, _ op.Request, in queryIn) (result, error) { return result{In: in}, nil },
	})
	op.Add(r, op.Op[showIn, result]{Name: "item.show", CLI: "item <id> show", Summary: "Show", Effect: op.Read, DefaultCommand: true, Aliases: []string{"get"},
		Handler: func(_ context.Context, _ op.Request, in showIn) (result, error) { return result{In: in}, nil },
	})
	op.Add(r, op.Op[listIn, result]{Name: "items.list", CLI: "items list", Summary: "List", Effect: op.Read, Aliases: []string{"ls"},
		Handler: func(_ context.Context, _ op.Request, in listIn) (result, error) { return result{In: in}, nil },
	})
	return r
}

func TestAliasesRunTheOperation(t *testing.T) {
	r := aliases()
	for args, want := range map[string]string{
		"search cats":        `"query": "cats"`,
		"s cats":             `"query": "cats"`,
		"find cats":          `"query": "cats"`,
		"item a1 get":        `"id": "a1"`,
		"item a1":            `"id": "a1"`,
		"items ls --tag x":   `"tag": "x"`,
		"--limit 5 items ls": `"limit": 5`,
	} {
		code, out, stderr := run(t, r, append([]string{"--json"}, strings.Fields(args)...)...)
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("%s: exit %d %s %s", args, code, out, stderr)
		}
	}
	if code, _, _ := run(t, r, "items", "list", "--json"); code != 0 {
		t.Errorf("the canonical word still runs: exit %d", code)
	}
	if _, help, _ := run(t, r, "--help"); !strings.Contains(help, "search (s,find)") {
		t.Errorf("help lists the aliases:\n%s", help)
	}
	if code, _, _ := run(t, r, "sea", "cats"); code != 2 {
		t.Errorf("an undeclared spelling is a usage error: exit %d", code)
	}
}

func TestAliasesCollideWithHandWrittenCommands(t *testing.T) {
	r := op.New("t", "v")
	op.Add(r, op.Op[queryIn, result]{Name: "search", Effect: op.Read, Aliases: []string{"hello"},
		Handler: func(context.Context, op.Request, queryIn) (result, error) { return result{}, nil }})
	err := cli.Validate(r, cli.Options{Commands: []cli.Command{{Name: "hello", Cmd: &hand{}}}})
	if err == nil || !strings.Contains(err.Error(), `alias "hello"`) {
		t.Errorf("an alias equal to a hand-written command must be rejected: %v", err)
	}
}
