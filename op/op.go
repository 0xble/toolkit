// Package op is the operation registry. A tool declares each operation once,
// with typed input and output structs, an effect and a handler. The CLI, HTTP
// API, OpenAPI document, MCP tools and metadata are all derived from that one
// declaration, and every surface calls the same Entry.Call.
package op

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Effect classifies what an operation does to the outside world.
type Effect string

const (
	// Read operations never change state.
	Read Effect = "read"
	// Write operations change state. They preview unless the caller sets apply.
	Write Effect = "write"
	// Destructive operations change state in a way that is hard to undo. They
	// preview unless the caller sets apply, and apply also requires confirm.
	Destructive Effect = "destructive"
)

// Mutates reports whether the effect can change state.
func (e Effect) Mutates() bool { return e == Write || e == Destructive }

// Surface names the adapter a call arrived through.
type Surface string

const (
	SurfaceCLI  Surface = "cli"
	SurfaceHTTP Surface = "http"
	SurfaceMCP  Surface = "mcp"
)

// Request carries the call-level controls every surface supplies alongside
// the typed input.
type Request struct {
	Surface Surface
	// Apply asks a write or destructive operation to perform the change.
	// Without it the handler must only preview. Always false for reads.
	Apply bool
	// Confirm is the caller's explicit confirmation of a destructive apply.
	Confirm bool
	// HTTP is the inbound request on the HTTP and HTTP-MCP surfaces, for
	// authorizers that read transport identity. Nil on the CLI and stdio MCP.
	HTTP *http.Request
	// Meta is a copy of the MCP tool call's request _meta on the MCP surface
	// (stdio and HTTP), or nil when the call has none. Clients may add
	// protocol keys under io.modelcontextprotocol/, so read only the keys you
	// define. It is always nil on the CLI and the HTTP API, and is never read
	// from the arguments, so a model that fills in arguments cannot set it.
	// It is caller-supplied
	// transport context, trusted only as much as the transport: on stdio,
	// the parent process that spawned the server; on HTTP, whoever can reach
	// the socket.
	Meta map[string]any
}

// Op declares one operation. In is a struct. Out is usually a struct but may
// be any type that encodes to JSON: any and json.RawMessage declare an output
// that may be any JSON value, and map[string]any any JSON object. Input field
// tags carry both the wire shape (json) and the CLI shape (kong: arg, name,
// short, help, default).
//
// An input field tagged toolkit:"cli-only", such as a flag that names a
// local file to read or write, is accepted only on the command line. HTTP
// and MCP input schemas and the metadata input omit it, the metadata lists
// it in cli_only_inputs, and a call on any other surface that sets it is
// refused with the usage error cli_only before the handler runs. The field
// must be optional (json omitempty) and have no default.
type Op[In, Out any] struct {
	// Name is the canonical dotted name, e.g. "note.delete".
	Name string
	// CLI is the command path, e.g. "note <id> delete". A word in angle
	// brackets is a positional argument owned by the parent command and fills
	// the string input field with that json name. Empty means Name with dots
	// replaced by spaces.
	CLI     string
	Summary string
	Effect  Effect
	// MCP exposes the operation as an MCP tool.
	MCP bool
	// DefaultCommand makes the last CLI word the default subcommand of its
	// parent, so "workflow <id> show" also runs as "workflow <id>", and
	// "items list" as "items". The word still works and is hidden from help.
	// At most one operation may be the default under a parent.
	DefaultCommand bool
	// Aliases are extra names of the last CLI word, as kong aliases: with
	// Aliases ["s"], "search <query>" also runs as "s <query>". They are
	// CLI-only and change no HTTP route, MCP name or operation name.
	Aliases []string
	// CLIImmediate makes the CLI apply a write or destructive operation
	// without --apply, for clearly scoped mutations such as "player pause".
	// The CLI gains --dry-run to preview instead and still accepts --apply as
	// a no-op. A destructive operation still needs --yes or a confirmed
	// prompt, unless it sets CLIConfirmed. HTTP and MCP are unchanged: they apply only with "apply": true,
	// and the served default authorizer still refuses applied writes.
	CLIImmediate bool
	// CLIConfirmed makes the command line itself the confirmation for a
	// destructive operation: with CLIImmediate it applies on the CLI with no
	// --yes or prompt. HTTP and MCP still need "apply": true and "confirm": true.
	// Needs Effect Destructive and CLIImmediate.
	CLIConfirmed bool
	// Paged declares that the output is a page: an object whose items array
	// holds the results, next to envelope keys such as next_cursor and
	// has_more. The CLI's --fields then keeps the named keys of each item and
	// every envelope key, rather than the named top-level keys. The output
	// must have an items array.
	Paged bool
	// Handler implements the operation. For write and destructive operations
	// it must not change state unless req.Apply is true.
	Handler func(ctx context.Context, req Request, in In) (Out, error)
	// Render optionally prints a human summary of the result. Without it the
	// CLI prints JSON.
	Render func(w io.Writer, out Out) error
	// RenderWithInput is Render for a summary that depends on the call, such
	// as a footer that repeats the caller's --limit. It receives the decoded
	// input. Set at most one of Render and RenderWithInput.
	RenderWithInput func(w io.Writer, in In, out Out) error
	// Warnings optionally lists warnings carried in the result. In human
	// output the CLI prints each to stderr as "warning: <text>" before the
	// result. With --json or --agent, and on HTTP and MCP, nothing extra is
	// printed: callers read the warnings in the result itself.
	Warnings func(out Out) []string
}

// Entry is the type-erased view of an operation that surfaces use.
type Entry struct {
	Name    string
	CLIPath []string
	Summary string
	Effect  Effect
	MCP     bool
	// DefaultCommand is Op.DefaultCommand.
	DefaultCommand bool
	// Aliases is Op.Aliases.
	Aliases []string
	// CLIImmediate is Op.CLIImmediate.
	CLIImmediate bool
	// CLIConfirmed is Op.CLIConfirmed.
	CLIConfirmed bool
	// Paged is Op.Paged.
	Paged bool
	// CLIOnlyInputs are the json names of the input fields tagged
	// toolkit:"cli-only", sorted.
	CLIOnlyInputs []string
	In, Out       reflect.Type

	call     func(ctx context.Context, req Request, in any) (any, error)
	render   func(w io.Writer, in, out any) error
	warnings func(out any) []string
	inSchema *jsonschema.Schema
	cliOnly  []cliOnlyInput
	// wire is the input schema of HTTP and MCP, without cli-only inputs.
	// resolved validates every surface's input, so it keeps them: Call then
	// refuses a remote caller that sets one with cli_only rather than an
	// unknown property.
	wire     *jsonschema.Schema
	resolved *jsonschema.Resolved
	out      *jsonschema.Schema
}

// Registry is one tool's operations.
type Registry struct {
	Tool    string
	Version string
	entries map[string]*Entry
	paths   map[string]string
}

// New returns an empty registry.
func New(tool, version string) *Registry {
	return &Registry{Tool: tool, Version: version, entries: map[string]*Entry{}, paths: map[string]string{}}
}

var (
	nameRE        = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	wordRE        = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	placeholderRE = regexp.MustCompile(`^<([a-z][a-z0-9_]*)>$`)
)

// Reserved wire fields the registry adds to inputs.
const (
	FieldApply   = "apply"
	FieldConfirm = "confirm"
)

// Add registers an operation. It panics on an invalid declaration, since
// that is a programming error found the first time the tool starts.
func Add[In, Out any](r *Registry, o Op[In, Out]) {
	e, err := newEntry(r, o)
	if err != nil {
		panic(fmt.Sprintf("op %q: %v", o.Name, err))
	}
	r.entries[e.Name] = e
}

func newEntry[In, Out any](r *Registry, o Op[In, Out]) (*Entry, error) {
	if !nameRE.MatchString(o.Name) {
		return nil, fmt.Errorf("name must be lowercase dotted words")
	}
	if _, dup := r.entries[o.Name]; dup {
		return nil, fmt.Errorf("registered twice")
	}
	switch o.Effect {
	case Read, Write, Destructive:
	default:
		return nil, fmt.Errorf("effect must be read, write or destructive")
	}
	if o.Handler == nil {
		return nil, fmt.Errorf("handler is nil")
	}
	if o.CLIImmediate && !o.Effect.Mutates() {
		return nil, fmt.Errorf("CLIImmediate needs a write or destructive effect")
	}
	if o.CLIConfirmed && (o.Effect != Destructive || !o.CLIImmediate) {
		return nil, fmt.Errorf("CLIConfirmed needs a destructive effect and CLIImmediate")
	}
	if o.Render != nil && o.RenderWithInput != nil {
		return nil, fmt.Errorf("set at most one of Render and RenderWithInput")
	}
	e := &Entry{Name: o.Name, Summary: o.Summary, Effect: o.Effect, MCP: o.MCP, DefaultCommand: o.DefaultCommand,
		Aliases: slices.Clone(o.Aliases), CLIImmediate: o.CLIImmediate, CLIConfirmed: o.CLIConfirmed, Paged: o.Paged, In: reflect.TypeFor[In](), Out: reflect.TypeFor[Out]()}
	if e.In.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input must be a struct, got %s", e.In)
	}
	path := o.CLI
	if path == "" {
		path = strings.ReplaceAll(o.Name, ".", " ")
	}
	e.CLIPath = strings.Fields(path)
	if err := checkPath(r, e); err != nil {
		return nil, err
	}
	if e.DefaultCommand && len(e.CLIPath) < 2 {
		return nil, fmt.Errorf("a default command needs a parent: its CLI path must have at least two words")
	}
	fields := jsonFields(e.In)
	for _, reserved := range []string{FieldApply, FieldConfirm} {
		if _, ok := fields[reserved]; ok {
			return nil, fmt.Errorf("input field %q is reserved", reserved)
		}
	}
	for _, w := range e.CLIPath {
		if m := placeholderRE.FindStringSubmatch(w); m != nil {
			f, ok := fields[m[1]]
			if !ok || f.Type.Kind() != reflect.String {
				return nil, fmt.Errorf("placeholder %s needs a string input field with json name %q", w, m[1])
			}
		}
	}
	var err error
	if e.inSchema, err = schemaFor(e.In); err != nil {
		return nil, fmt.Errorf("input schema: %w", err)
	}
	if e.out, err = schemaFor(e.Out); err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}
	if e.Paged && !hasItemsArray(e.out) {
		return nil, fmt.Errorf("a Paged operation needs an output object with an items array")
	}
	if e.cliOnly, err = cliOnlyInputs(e.In, e.inSchema); err != nil {
		return nil, err
	}
	for _, f := range e.cliOnly {
		e.CLIOnlyInputs = append(e.CLIOnlyInputs, f.json)
	}
	full := e.buildWireSchema()
	if e.resolved, err = full.Resolve(nil); err != nil {
		return nil, fmt.Errorf("resolve input schema: %w", err)
	}
	e.wire = cloneSchema(full)
	for _, name := range e.CLIOnlyInputs {
		delete(e.wire.Properties, name)
	}
	if _, err := newInput(e.In); err != nil {
		return nil, err
	}
	e.call = func(ctx context.Context, req Request, in any) (any, error) {
		v, ok := in.(*In)
		if !ok {
			return nil, fmt.Errorf("op %s: input is %T, want *%s", o.Name, in, e.In)
		}
		out, err := o.Handler(ctx, req, *v)
		var oe *Error
		if errors.As(err, &oe) && oe.Result != nil {
			if _, ok := oe.Result.(Out); !ok {
				return nil, fmt.Errorf("op %s: error result is %T, want %s", o.Name, oe.Result, e.Out)
			}
		}
		return out, err
	}
	if o.Render != nil {
		e.render = func(w io.Writer, _, out any) error {
			v, err := typedOut[Out](e, out)
			if err != nil {
				return err
			}
			return o.Render(w, v)
		}
	}
	if o.RenderWithInput != nil {
		e.render = func(w io.Writer, in, out any) error {
			v, err := typedOut[Out](e, out)
			if err != nil {
				return err
			}
			var iv In
			switch x := in.(type) {
			case nil:
			case *In:
				iv = *x
			case In:
				iv = x
			default:
				return fmt.Errorf("op %s: input is %T, want %s", e.Name, in, e.In)
			}
			return o.RenderWithInput(w, iv, v)
		}
	}
	if o.Warnings != nil {
		e.warnings = func(out any) []string {
			// Surfaces only pass outputs the handler returned, so a mismatch
			// is impossible; Render reports it if it ever happens.
			v, err := typedOut[Out](e, out)
			if err != nil {
				return nil
			}
			return o.Warnings(v)
		}
	}
	return e, nil
}

// hasItemsArray reports whether s is an object schema with an items array.
func hasItemsArray(s *jsonschema.Schema) bool {
	if s.Type != "object" {
		return false
	}
	p := s.Properties["items"]
	return p != nil && (p.Type == "array" || slices.Contains(p.Types, "array"))
}

// typedOut converts an output back to Out. A nil output is the zero value of
// an interface Out such as any.
func typedOut[Out any](e *Entry, out any) (Out, error) {
	v, ok := out.(Out)
	if !ok && (out != nil || e.Out.Kind() != reflect.Interface) {
		return v, fmt.Errorf("op %s: output is %T, want %s", e.Name, out, e.Out)
	}
	return v, nil
}

// checkPath validates the CLI words and aliases and rejects paths that would
// collide in the command tree: identical paths, paths equal after
// placeholders are removed, and a path that is a prefix of another (a command
// cannot be both runnable and a group). Each alias claims the path with the
// last word replaced, under the same rules.
func checkPath(r *Registry, e *Entry) error {
	if len(e.CLIPath) == 0 || placeholderRE.MatchString(e.CLIPath[0]) {
		return fmt.Errorf("CLI path must start with a command word")
	}
	last := e.CLIPath[len(e.CLIPath)-1]
	if placeholderRE.MatchString(last) {
		return fmt.Errorf("CLI path must end with a command word; use an arg:\"\" input field for a trailing positional")
	}
	for _, w := range e.CLIPath {
		if !wordRE.MatchString(w) && !placeholderRE.MatchString(w) {
			return fmt.Errorf("invalid CLI word %q", w)
		}
	}
	words := commandWords(e.CLIPath)
	keys := []string{strings.Join(words, " ")}
	for _, a := range e.Aliases {
		if !wordRE.MatchString(a) {
			return fmt.Errorf("invalid alias %q", a)
		}
		keys = append(keys, strings.Join(append(slices.Clone(words[:len(words)-1]), a), " "))
	}
	claimed := map[string]bool{}
	for _, key := range keys {
		if claimed[key] {
			return fmt.Errorf("CLI path %q is declared twice by its word and aliases", key)
		}
		claimed[key] = true
		if other, ok := r.paths[key]; ok {
			return fmt.Errorf("CLI path %q collides with %s", key, other)
		}
		for k, other := range r.paths {
			if strings.HasPrefix(k+" ", key+" ") || strings.HasPrefix(key+" ", k+" ") {
				return fmt.Errorf("CLI path %q and %s's path are prefixes of each other", key, other)
			}
		}
	}
	for _, key := range keys {
		r.paths[key] = e.Name
	}
	return nil
}

func commandWords(p []string) []string {
	var out []string
	for _, w := range p {
		if !placeholderRE.MatchString(w) {
			out = append(out, w)
		}
	}
	return out
}

// CLI is the command path as declared, e.g. "note <id> delete".
func (e *Entry) CLI() string { return strings.Join(e.CLIPath, " ") }

// CommandKey is the CLI path without placeholders, e.g. "note delete".
func (e *Entry) CommandKey() string { return strings.Join(commandWords(e.CLIPath), " ") }

// Entries returns operations sorted by name.
func (r *Registry) Entries() []*Entry {
	out := make([]*Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup finds an operation by canonical name.
func (r *Registry) Lookup(name string) *Entry { return r.entries[name] }

// MCPName is the MCP tool name and OpenAPI operationId. Dots are not valid
// in every model provider's function-name grammar, so they become underscores.
func (e *Entry) MCPName() string { return strings.ReplaceAll(e.Name, ".", "_") }

// InputSchema is the JSON Schema of the wire input that HTTP and MCP accept,
// including the standard apply and confirm controls and omitting cli-only
// inputs. The caller gets a copy.
func (e *Entry) InputSchema() *jsonschema.Schema { return cloneSchema(e.wire) }

// OutputSchema is the JSON Schema of the result. The caller gets a copy.
func (e *Entry) OutputSchema() *jsonschema.Schema { return cloneSchema(e.out) }

// Warnings returns the warnings the operation's Warnings hook finds in out,
// or nil without a hook.
func (e *Entry) Warnings(out any) []string {
	if e.warnings == nil {
		return nil
	}
	return e.warnings(out)
}

// CanRender reports whether the operation has a human render hook, Render
// or RenderWithInput.
func (e *Entry) CanRender() bool { return e.render != nil }

// Render prints the human form of out. A RenderWithInput hook receives the
// zero input. It errors when there is no hook.
func (e *Entry) Render(w io.Writer, out any) error { return e.RenderWithInput(w, nil, out) }

// RenderWithInput prints the human form of out for the call with input in,
// the *In that Decode returns, an In, or nil for the zero input. A Render
// hook ignores in. It errors when there is no hook.
func (e *Entry) RenderWithInput(w io.Writer, in, out any) error {
	if e.render == nil {
		return fmt.Errorf("op %s has no render hook", e.Name)
	}
	return e.render(w, in, out)
}

func (e *Entry) buildWireSchema() *jsonschema.Schema {
	s := cloneSchema(e.inSchema)
	if !e.Effect.Mutates() {
		return s
	}
	if s.Properties == nil {
		s.Properties = map[string]*jsonschema.Schema{}
	}
	s.Properties[FieldApply] = &jsonschema.Schema{Type: "boolean",
		Description: "Perform the change. Without it the operation only previews."}
	if e.Effect == Destructive {
		s.Properties[FieldConfirm] = &jsonschema.Schema{Type: "boolean",
			Description: "Must be true together with apply: this operation is destructive. Previews do not need it."}
	}
	return s
}

// NewInput returns a pointer to a fresh input with kong default tags applied.
func (e *Entry) NewInput() any {
	v, _ := newInput(e.In) // checked at Add
	return v
}

// Decode builds a typed input from wire JSON after validating it against the
// input schema. It returns the input pointer and the apply and confirm
// controls, which are removed from the object before decoding.
func (e *Entry) Decode(raw json.RawMessage) (in any, apply, confirm bool, err error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = json.RawMessage("{}")
	}
	generic, ok := decodeObject(raw)
	if !ok {
		return nil, false, false, Errorf(KindUsage, "invalid_input", "input must be a JSON object")
	}
	if err := e.resolved.Validate(generic); err != nil {
		return nil, false, false, Errorf(KindUsage, "invalid_input", "%s", err.Error())
	}
	apply, _ = generic[FieldApply].(bool)
	confirm, _ = generic[FieldConfirm].(bool)
	delete(generic, FieldApply)
	delete(generic, FieldConfirm)
	clean, err := json.Marshal(generic)
	if err != nil {
		return nil, false, false, err
	}
	in = e.NewInput()
	if err := prefillSections(reflect.ValueOf(in), generic); err != nil {
		return nil, false, false, err
	}
	dec := json.NewDecoder(strings.NewReader(string(clean)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(in); err != nil {
		return nil, false, false, Errorf(KindUsage, "invalid_input", "%s", err.Error())
	}
	return in, apply, confirm, nil
}

// Call runs the handler. The surface, apply and confirm rules are enforced
// here, once, for every surface: a cli-only input set off the CLI is
// refused, reads never apply, and a destructive apply without confirmation
// is refused, all before the handler runs.
func (e *Entry) Call(ctx context.Context, req Request, in any) (any, error) {
	if err := e.checkCLIOnly(req, in); err != nil {
		return nil, err
	}
	if !e.Effect.Mutates() {
		req.Apply, req.Confirm = false, false
	}
	if err := e.checkConfirm(req); err != nil {
		return nil, err
	}
	return e.call(ctx, req, in)
}

// checkCLIOnly refuses a call that sets a cli-only input on any surface but
// the CLI: a served tool must not read or write paths for a remote caller.
func (e *Entry) checkCLIOnly(req Request, in any) error {
	if req.Surface == SurfaceCLI || len(e.cliOnly) == 0 {
		return nil
	}
	v := reflect.ValueOf(in)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Type() != e.In {
		return nil // e.call reports the mismatch
	}
	for _, f := range e.cliOnly {
		if fv, err := v.FieldByIndexErr(f.index); err == nil && !fv.IsZero() {
			return &Error{Kind: KindUsage, Code: "cli_only",
				Message:     f.flag + " names a local path and is accepted only on the command line",
				Suggestions: []string{"run the command line on the host, or call without " + f.json}}
		}
	}
	return nil
}

func (e *Entry) checkConfirm(req Request) error {
	if e.Effect == Destructive && req.Apply && !req.Confirm {
		return &Error{Kind: KindUsage, Code: "confirmation_required",
			Message:     e.Name + " is destructive and needs explicit confirmation to apply",
			Suggestions: []string{"CLI: add --yes or answer the prompt", "API and MCP: send \"confirm\": true with \"apply\": true"}}
	}
	return nil
}

// CallJSON is the remote path used by HTTP and MCP: decode and validate the
// wire input, refuse cli-only inputs, enforce confirmation, authorize, then
// Call. A nil authorizer means DenyWrites.
func (e *Entry) CallJSON(ctx context.Context, req Request, raw json.RawMessage, auth Authorizer) (any, error) {
	in, apply, confirm, err := e.Decode(raw)
	if err != nil {
		return nil, err
	}
	req.Apply, req.Confirm = apply && e.Effect.Mutates(), confirm && e.Effect == Destructive
	if err := e.checkCLIOnly(req, in); err != nil {
		return nil, err
	}
	if err := e.checkConfirm(req); err != nil {
		return nil, err
	}
	if auth == nil {
		auth = DenyWrites
	}
	if err := auth.Authorize(ctx, e, req); err != nil {
		return nil, AsError(err, KindAuth)
	}
	return e.Call(ctx, req, in)
}
