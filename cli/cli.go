// Package cli generates a kong command line from an operation registry.
//
// Each operation's CLI path ("note <id> delete") becomes nested kong commands
// built with reflect.StructOf. Input fields keep their own kong tags, so a
// field is declared once for the CLI and the wire. A path placeholder (<id>)
// becomes a branching positional argument and fills the input field with that
// json name. Root flags declared in Options.Globals with a json tag fill the
// input field with the same json name, so `tool --limit 5 notes list` and
// `tool notes list --limit 5` both work.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-isatty"

	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
)

// Options configures the generated command line.
type Options struct {
	// Description is the root help text.
	Description string
	// Globals is an optional pointer to a struct of root flags. A field with a
	// json tag fills the operation input field with the same json name and
	// type whenever the flag is given on the command line; otherwise the
	// input's own default applies, as it does over HTTP and MCP. Fields
	// without a json tag are root flags the tool reads itself.
	Globals any
	// Commands are hand-written commands mounted next to the operations, for
	// the few actions that do not fit the registry.
	Commands []Command
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
}

// Command is a hand-written kong command. Cmd is a pointer to a kong command
// struct whose Run method takes a *Context.
type Command struct {
	Name string
	Help string
	Cmd  any
}

// Context is what a hand-written command's Run method receives.
type Context struct {
	context.Context
	Registry *op.Registry
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	// JSON reports whether the caller asked for machine output (--json or
	// --agent).
	JSON bool
}

// Builtin root flags. Tools must not redeclare these names in Globals.
type builtins struct {
	JSON    bool             `short:"j" help:"Output as JSON"`
	Agent   bool             `help:"Agent surface: JSON output, no colour, no prompts"`
	Fields  string           `help:"Comma-separated fields to include in JSON output"`
	Yes     bool             `short:"y" help:"Confirm a destructive --apply without prompting"`
	Version kong.VersionFlag `help:"Show version"`
}

type exitSignal struct{ code int }

// Run parses args, runs the selected operation or command and returns the
// process exit code. It never calls os.Exit.
func Run(ctx context.Context, reg *op.Registry, opts Options, args []string) (code int) {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	defer func() {
		if r := recover(); r != nil {
			s, ok := r.(exitSignal)
			if !ok {
				panic(r)
			}
			code = s.code
		}
	}()
	app, k, err := newKong(reg, opts, kong.Exit(func(c int) {
		if c != 0 {
			c = output.ExitUsage
		}
		panic(exitSignal{c})
	}))
	if err != nil {
		_, _ = fmt.Fprintln(opts.Stderr, "error: invalid command tree:", err)
		return output.ExitError
	}
	kctx, err := k.Parse(args)
	if err != nil {
		format := output.FormatTable
		if wantsJSON(args) {
			format = output.FormatJSON
		}
		e := &output.CLIError{Code: "usage", Message: err.Error(), ExitCode: output.ExitUsage,
			Suggestions: []string{"run " + reg.Tool + " --help"}}
		output.WriteError(opts.Stderr, format, e)
		return output.ExitUsage
	}
	output.SetAgentMode(app.root.Agent)
	defer output.SetAgentMode(false)
	format := output.ResolveFormat("", app.root.JSON)
	if l := app.selected(kctx); l != nil {
		return app.runOp(ctx, kctx, l, opts, format)
	}
	c := &Context{Context: ctx, Registry: reg, Stdin: opts.Stdin, Stdout: opts.Stdout, Stderr: opts.Stderr,
		JSON: format == output.FormatJSON}
	if err := kctx.Run(c); err != nil {
		return writeErr(opts.Stderr, format, err)
	}
	return output.ExitOK
}

// Validate builds the command tree without running it, so a tool's tests
// catch a declaration kong rejects, such as a duplicate flag.
func Validate(reg *op.Registry, opts Options) error {
	_, _, err := newKong(reg, opts)
	return err
}

func newKong(reg *op.Registry, opts Options, extra ...kong.Option) (*app, *kong.Kong, error) {
	a, err := build(reg, opts)
	if err != nil {
		return nil, nil, err
	}
	kopts := []kong.Option{
		kong.Name(reg.Tool),
		kong.Description(opts.Description),
		kong.Writers(opts.Stdout, opts.Stderr),
		kong.Vars{"version": reg.Version},
	}
	if opts.Globals != nil {
		kopts = append(kopts, kong.Embed(opts.Globals))
	}
	kopts = append(kopts, a.options...)
	kopts = append(kopts, extra...)
	k, err := kong.New(&a.root, kopts...)
	if err != nil {
		return nil, nil, err
	}
	if err := a.bindGlobalFlags(k); err != nil {
		return nil, nil, err
	}
	return a, k, nil
}

func (a *app) runOp(ctx context.Context, kctx *kong.Context, l *leaf, opts Options, format output.Format) int {
	e := l.entry
	in, err := a.input(kctx, l)
	if err != nil {
		return writeErr(opts.Stderr, format, err)
	}
	req := op.Request{Surface: op.SurfaceCLI, Confirm: a.root.Yes}
	if l.dryRun.IsValid() {
		req.Apply = !l.dryRun.Bool()
	} else if l.apply.IsValid() {
		req.Apply = l.apply.Bool()
	}
	if e.Effect == op.Destructive && req.Apply && !req.Confirm && !a.root.Agent && isTerminal(opts.Stdin) {
		req.Confirm = prompt(opts.Stdin, opts.Stderr, fmt.Sprintf("Apply %s? This is destructive.", e.Name))
	}
	out, err := e.Call(ctx, req, in)
	if err != nil {
		var oe *op.Error
		if errors.As(err, &oe) && oe.Result != nil {
			if perr := a.print(e, in, oe.Result, opts, format); perr != nil {
				return writeErr(opts.Stderr, format, perr)
			}
		}
		return writeErr(opts.Stderr, format, err)
	}
	if err := a.print(e, in, out, opts, format); err != nil {
		return writeErr(opts.Stderr, format, err)
	}
	return output.ExitOK
}

// print writes an operation's output to stdout: the render hook, which also
// sees the decoded input, for human output, otherwise JSON filtered by
// --fields. Human output first prints the result's warnings to stderr.
func (a *app) print(e *op.Entry, in, out any, opts Options, format output.Format) error {
	if format != output.FormatJSON {
		for _, w := range e.Warnings(out) {
			if _, err := fmt.Fprintf(opts.Stderr, "warning: %s\n", w); err != nil {
				return err
			}
		}
	}
	if format != output.FormatJSON && e.CanRender() {
		return e.RenderWithInput(opts.Stdout, in, out)
	}
	v := out
	if strings.TrimSpace(a.root.Fields) != "" {
		var err error
		if v, err = output.FilterFields(v, strings.Split(a.root.Fields, ",")); err != nil {
			return op.Errorf(op.KindUsage, "invalid_fields", "%s", err.Error())
		}
	}
	return output.EncodeJSON(opts.Stdout, v)
}

func writeErr(w io.Writer, format output.Format, err error) int {
	var ce *output.CLIError
	if !errors.As(err, &ce) {
		ce = op.AsError(err, op.KindError).CLIError()
	}
	output.WriteError(w, format, ce)
	return ce.ExitCode
}

func wantsJSON(args []string) bool {
	for _, a := range args {
		if a == "--" {
			break
		}
		switch a {
		case "--json", "-j", "--agent", "--json=true", "--agent=true":
			return true
		}
	}
	return false
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && (isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd()))
}

func prompt(in io.Reader, out io.Writer, msg string) bool {
	_, _ = fmt.Fprint(out, msg+" [y/N] ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
