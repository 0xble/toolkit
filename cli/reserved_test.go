package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/op"
)

// withInput registers one operation x.y with input In.
func withInput[In any](eff op.Effect, immediate bool) *op.Registry {
	r := op.New("t", "v1")
	op.Add(r, op.Op[In, result]{Name: "x.y", Effect: eff, CLIImmediate: immediate,
		Handler: func(context.Context, op.Request, In) (result, error) { return result{}, nil }})
	return r
}

type (
	versionIn struct {
		Version string `json:"version"`
	}
	jsonIn struct {
		JSON bool `json:"json"`
	}
	shortJIn struct {
		Join bool `json:"join" short:"j"`
	}
	helpIn struct {
		Help bool `json:"help"`
	}
	shortHIn struct {
		Host string `json:"host" short:"h"`
	}
	aliasIn struct {
		Only string `json:"only" aliases:"fields"`
	}
	applyIn struct {
		Now bool `json:"now" name:"apply"`
	}
	dryRunIn struct {
		Plan bool `json:"plan" name:"dry-run"`
	}
	verboseIn struct {
		Verbose bool `json:"verbose"`
	}
	renamedIn struct {
		Version int    `json:"version,omitempty" name:"at-version"`
		Account string `json:"account,omitempty"`
	}
)

type agentCmd struct {
	Agent bool `help:"Clashes with the root --agent"`
}

func (c *agentCmd) Run(*cli.Context) error { return nil }

func TestValidateRejectsSwallowedFlags(t *testing.T) {
	opts := cli.Options{Globals: &globals{}}
	for _, c := range []struct {
		name string
		reg  *op.Registry
		opts cli.Options
		want string
	}{
		{"version", withInput[versionIn](op.Read, false), opts, "operation x.y: flag --version collides with the toolkit's root flag --version"},
		{"json", withInput[jsonIn](op.Read, false), opts, "flag --json collides with the toolkit's root flag --json"},
		{"short j", withInput[shortJIn](op.Read, false), opts, "short flag -j of --join collides with the toolkit's root flag --json"},
		{"help", withInput[helpIn](op.Read, false), opts, "flag --help collides with the toolkit's root flag --help"},
		{"short h", withInput[shortHIn](op.Read, false), opts, "short flag -h of --host collides with the toolkit's root flag --help"},
		{"alias", withInput[aliasIn](op.Read, false), opts, "flag --fields collides with the toolkit's root flag --fields"},
		{"apply on a read", withInput[applyIn](op.Read, false), opts, "flag --apply collides with the toolkit's reserved --apply"},
		{"apply on a write", withInput[applyIn](op.Write, false), opts, "duplicate flag --apply"},
		{"dry-run on a write", withInput[dryRunIn](op.Write, false), opts, "flag --dry-run collides with the toolkit's reserved --dry-run"},
		{"tool root flag", withInput[verboseIn](op.Read, false), opts, "flag --verbose collides with the tool's root flag --verbose"},
		{"hand-written command", op.New("t", "v1"), cli.Options{Commands: []cli.Command{{Name: "hand", Cmd: &agentCmd{}}}},
			"command hand: flag --agent collides with the toolkit's root flag --agent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := cli.Validate(c.reg, c.opts)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want %q", err, c.want)
			}
		})
	}
	for _, c := range []struct {
		name string
		reg  *op.Registry
		opts cli.Options
	}{
		{"renamed, and bound to a root flag", withInput[renamedIn](op.Read, false), opts},
		{"generated --apply", withInput[verboseIn](op.Destructive, false), cli.Options{}},
		{"generated --dry-run", withInput[renamedIn](op.Write, true), cli.Options{}},
	} {
		if err := cli.Validate(c.reg, c.opts); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}
