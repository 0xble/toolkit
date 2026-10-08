package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/0xble/toolkit/internal/jsontag"
	"github.com/0xble/toolkit/op"
)

// app is one generated command line. It is built fresh for every Run, so the
// parsed values never leak between invocations.
type app struct {
	root    builtins
	options []kong.Option
	leaves  map[string]*leaf
	globals []*global
	// shorts are the declared short flags, which markNumbers leaves alone.
	shorts map[rune]bool
}

// leaf is the generated kong command of one operation.
type leaf struct {
	entry *op.Entry
	// v is the addressable generated command struct.
	v      reflect.Value
	fields []flatField
	// args are the path placeholders above this command.
	args []placeholder
	// apply is the generated --apply flag; invalid for reads.
	apply reflect.Value
	// dryRun is the generated --dry-run flag; valid only for an operation
	// with CLIImmediate.
	dryRun reflect.Value
	// globals maps the json name of an input field bound to a root flag to
	// that field's index path in the input.
	globals map[string]flatField
}

// flatField is one input field as it appears on the command line. Embedded
// structs without a json name are flattened, as encoding/json does.
type flatField struct {
	json      string
	input     []int
	gen       reflect.StructField
	stringTag bool
}

type explicitField struct {
	input     []int
	stringTag bool
}

type placeholder struct {
	json      string
	input     []int
	v         reflect.Value
	stringTag bool
}

type global struct {
	json  string
	v     reflect.Value
	flag  string
	field reflect.StructField
}

type node struct {
	word     string
	arg      string
	children []*node
	entry    *op.Entry
}

const (
	applyField  = "ToolkitApply"
	dryRunField = "ToolkitDryRun"
)

func build(reg *op.Registry, opts Options) (*app, error) {
	a := &app{leaves: map[string]*leaf{}}
	if err := a.parseGlobals(opts.Globals); err != nil {
		return nil, err
	}
	root := &node{}
	for _, e := range reg.Entries() {
		cur := root
		for _, w := range e.CLIPath {
			var next *node
			for _, c := range cur.children {
				if c.word == w {
					next = c
				}
			}
			if next == nil {
				next = &node{word: w}
				if strings.HasPrefix(w, "<") {
					next.arg = strings.Trim(w, "<>")
					for _, c := range cur.children {
						if c.arg != "" {
							return nil, fmt.Errorf("%s: %s and %s are two placeholders at the same position", e.Name, c.word, w)
						}
					}
				}
				cur.children = append(cur.children, next)
			}
			cur = next
		}
		cur.entry = e
	}
	if err := checkDefaults(root); err != nil {
		return nil, err
	}
	if err := checkRootAliases(root, opts.Commands); err != nil {
		return nil, err
	}
	for _, c := range root.children {
		typ, err := a.buildType(c, nil)
		if err != nil {
			return nil, err
		}
		v := reflect.New(typ)
		if err := a.collect(c, v.Elem(), []string{c.word}, nil); err != nil {
			return nil, err
		}
		a.options = append(a.options, kong.DynamicCommand(c.word, nodeHelp(c), "Operations", v.Interface(), aliasTag(c)))
	}
	for _, cmd := range opts.Commands {
		a.options = append(a.options, kong.DynamicCommand(cmd.Name, cmd.Help, "Commands", cmd.Cmd))
	}
	return a, nil
}

func (a *app) parseGlobals(g any) error {
	if g == nil {
		return nil
	}
	v := reflect.ValueOf(g)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("options.Globals must be a pointer to a struct, got %T", g)
	}
	v = v.Elem()
	for i := range v.NumField() {
		f := v.Type().Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || !f.IsExported() {
			continue
		}
		if name == op.FieldApply || name == op.FieldConfirm {
			return fmt.Errorf("global flag %s: json name %q is reserved", f.Name, name)
		}
		a.globals = append(a.globals, &global{json: name, v: v.Field(i), field: f})
	}
	return nil
}

// bindGlobalFlags checks that each bound global is a root flag.
func (a *app) bindGlobalFlags(k *kong.Kong) error {
	for _, g := range a.globals {
		for _, f := range k.Model.Flags {
			if f.Target.IsValid() && f.Target.CanAddr() && f.Target.UnsafeAddr() == g.v.UnsafeAddr() {
				g.flag = f.Name
			}
		}
		if g.flag == "" {
			return fmt.Errorf("global %s is not a root flag", g.field.Name)
		}
	}
	return nil
}

// nodeHelp is an operation's summary, or for a group with a default
// command, that command's summary.
func nodeHelp(n *node) string {
	if n.entry != nil {
		return n.entry.Summary
	}
	if d := defaultChild(n); d != nil {
		return d.entry.Summary
	}
	return ""
}

func defaultChild(n *node) *node {
	for _, c := range n.children {
		if c.entry != nil && c.entry.DefaultCommand {
			return c
		}
	}
	return nil
}

// checkDefaults allows at most one default command per parent, and none next
// to a placeholder, where kong could not tell a subcommand from an argument.
func checkDefaults(n *node) error {
	var defaults []string
	arg := ""
	for _, c := range n.children {
		if c.entry != nil && c.entry.DefaultCommand {
			defaults = append(defaults, c.entry.Name)
		}
		if c.arg != "" {
			arg = c.word
		}
		if err := checkDefaults(c); err != nil {
			return err
		}
	}
	if len(defaults) > 1 {
		return fmt.Errorf("%s are all default commands of the same parent", strings.Join(defaults, " and "))
	}
	if len(defaults) == 1 && arg != "" {
		return fmt.Errorf("%s is a default command next to the placeholder %s", defaults[0], arg)
	}
	return nil
}

// checkRootAliases rejects an alias of a root command that equals the name
// of a hand-written command, which kong would not report. The registry
// already keeps aliases apart from other operations' words.
func checkRootAliases(root *node, cmds []Command) error {
	for _, c := range root.children {
		if c.entry == nil {
			continue
		}
		for _, alias := range c.entry.Aliases {
			for _, cmd := range cmds {
				if cmd.Name == alias {
					return fmt.Errorf("%s: alias %q is also the name of a command", c.entry.Name, alias)
				}
			}
		}
	}
	return nil
}

func (a *app) globalFor(name string) *global {
	for _, g := range a.globals {
		if g.json == name {
			return g
		}
	}
	return nil
}

// leafFields lists the input fields that become flags or positional
// arguments of the operation's own command: everything except path
// placeholders and fields bound to root flags.
func (a *app) leafFields(e *op.Entry, bound map[string]bool) (own []flatField, globals map[string]flatField, err error) {
	all, err := flatten(e.In, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", e.Name, err)
	}
	globals = map[string]flatField{}
	seen := map[string]bool{}
	for _, f := range all {
		if bound[f.json] {
			continue
		}
		if g := a.globalFor(f.json); g != nil {
			if g.field.Type != f.gen.Type {
				return nil, nil, fmt.Errorf("%s: input field %q is %s but root flag %s is %s", e.Name, f.json, f.gen.Type, g.field.Name, g.field.Type)
			}
			globals[f.json] = f
			continue
		}
		if seen[f.gen.Name] {
			return nil, nil, fmt.Errorf("%s: Go field name %s appears twice after flattening", e.Name, f.gen.Name)
		}
		seen[f.gen.Name] = true
		own = append(own, f)
	}
	return own, globals, nil
}

func flatten(t reflect.Type, prefix []int) ([]flatField, error) {
	var out []flatField
	names := map[string]bool{}
	for i := range t.NumField() {
		f := t.Field(i)
		idx := append(append([]int(nil), prefix...), i)
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if tag == "-" || f.Tag.Get("kong") == "-" {
			continue
		}
		ft := f.Type
		if f.Anonymous && name == "" && ft.Kind() == reflect.Struct {
			inner, err := flatten(ft, idx)
			if err != nil {
				return nil, err
			}
			for _, x := range inner {
				if !names[x.json] {
					names[x.json] = true
					out = append(out, x)
				}
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if f.Anonymous {
			return nil, fmt.Errorf("embedded field %s must be a struct without a json name", f.Name)
		}
		if name == "" {
			name = f.Name
		}
		if names[name] {
			continue
		}
		names[name] = true
		gen := reflect.StructField{Name: f.Name, Type: f.Type, Tag: f.Tag}
		out = append(out, flatField{json: name, input: idx, gen: gen, stringTag: jsontag.Quoted(f)})
	}
	return out, nil
}

// buildType returns the kong command struct for n. A leaf holds the
// operation's own fields and --apply. A placeholder child becomes a branching
// positional argument: a struct field and its first inner field share one
// name, which is how kong recognises a branching argument.
func (a *app) buildType(n *node, bound map[string]bool) (reflect.Type, error) {
	var fields []reflect.StructField
	if n.entry != nil {
		own, _, err := a.leafFields(n.entry, bound)
		if err != nil {
			return nil, err
		}
		for _, f := range own {
			fields = append(fields, f.gen)
		}
		fields = append(fields, applyFlags(n.entry)...)
	}
	for i, c := range n.children {
		if c.arg != "" {
			nb := map[string]bool{c.arg: true}
			for k := range bound {
				nb[k] = true
			}
			inner, err := a.buildType(c, nb)
			if err != nil {
				return nil, err
			}
			name := argFieldName(c.arg)
			argTag := reflect.StructTag(fmt.Sprintf(`arg:"" name:%q help:%q json:"-"`, c.arg, placeholderHelp(c)))
			wrapped := append([]reflect.StructField{{Name: name, Type: reflect.TypeFor[string](), Tag: argTag}}, fieldsOf(inner)...)
			fields = append(fields, reflect.StructField{Name: name, Type: reflect.StructOf(wrapped), Tag: argTag})
			continue
		}
		t, err := a.buildType(c, bound)
		if err != nil {
			return nil, err
		}
		tag := fmt.Sprintf(`cmd:"" name:%q help:%q json:"-"`, c.word, nodeHelp(c))
		if c.entry != nil && c.entry.DefaultCommand {
			tag += ` default:"withargs"`
			// kong's help lists leaf commands, so hiding the only child of a
			// group would hide the group too. Keep it listed then.
			if len(n.children) > 1 {
				tag += ` hidden:""`
			}
		}
		tag += aliasTag(c)
		fields = append(fields, reflect.StructField{Name: fmt.Sprintf("C%d", i), Type: t, Tag: reflect.StructTag(tag)})
	}
	return reflect.StructOf(fields), nil
}

// applyFlags are the generated --apply and --dry-run flags of a write or
// destructive operation. By default --apply performs the change. With
// CLIImmediate the command applies on its own, --dry-run previews, and
// --apply is a hidden no-op kept for callers that already pass it. With
// CLIConfirmed a destructive command also applies without --yes.
func applyFlags(e *op.Entry) []reflect.StructField {
	if !e.Effect.Mutates() {
		return nil
	}
	flag := func(field, name, help, extra string) reflect.StructField {
		return reflect.StructField{Name: field, Type: reflect.TypeFor[bool](),
			Tag: reflect.StructTag(fmt.Sprintf(`name:%q help:%q json:"-"%s`, name, help, extra))}
	}
	destructive := e.Effect == op.Destructive
	if !e.CLIImmediate {
		help := "Perform the change. Without it the command only previews."
		if destructive {
			help += " Destructive: also needs --yes or a confirmed prompt."
		}
		return []reflect.StructField{flag(applyField, "apply", help, "")}
	}
	help := "Preview the change without applying it."
	if destructive && !e.CLIConfirmed {
		help += " Destructive: applying needs --yes or a confirmed prompt."
	}
	const xor = ` xor:"toolkit-apply"`
	return []reflect.StructField{
		flag(applyField, "apply", "Accepted for compatibility: this command applies without it.", xor+` hidden:""`),
		flag(dryRunField, "dry-run", help, xor),
	}
}

// aliasTag is the kong tag of an operation's aliases, or "".
func aliasTag(n *node) string {
	if n.entry == nil || len(n.entry.Aliases) == 0 {
		return ""
	}
	return fmt.Sprintf(` aliases:%q`, strings.Join(n.entry.Aliases, ","))
}

// collect records where each leaf's generated values live, so a parsed
// command can be turned back into a typed input.
func (a *app) collect(n *node, v reflect.Value, words []string, args []placeholder) error {
	if n.entry != nil {
		own, globals, err := a.leafFields(n.entry, placeholderSet(args))
		if err != nil {
			return err
		}
		all, err := flatten(n.entry.In, nil)
		if err != nil {
			return err
		}
		l := &leaf{entry: n.entry, v: v, fields: own, globals: globals, args: make([]placeholder, len(args))}
		for i, p := range args {
			// The tag is per leaf: sibling operations under one <id> may
			// tag their id field differently.
			for _, f := range all {
				if f.json == p.json {
					p.input = f.input
					p.stringTag = f.stringTag
				}
			}
			l.args[i] = p
		}
		if n.entry.Effect.Mutates() {
			l.apply = v.FieldByName(applyField)
		}
		if n.entry.CLIImmediate {
			l.dryRun = v.FieldByName(dryRunField)
		}
		a.leaves[strings.Join(words, " ")] = l
	}
	for i, c := range n.children {
		if c.arg != "" {
			name := argFieldName(c.arg)
			w := v.FieldByName(name)
			next := append(append([]placeholder(nil), args...), placeholder{json: c.arg, v: w.Field(0)})
			if err := a.collect(c, w, words, next); err != nil {
				return err
			}
			continue
		}
		if err := a.collect(c, v.FieldByName(fmt.Sprintf("C%d", i)), append(append([]string(nil), words...), c.word), args); err != nil {
			return err
		}
	}
	return nil
}

func placeholderSet(args []placeholder) map[string]bool {
	m := map[string]bool{}
	for _, p := range args {
		m[p.json] = true
	}
	return m
}

// placeholderHelp reuses the help text of the input field the placeholder
// fills, from the first operation below it.
func placeholderHelp(n *node) string {
	if n.entry != nil {
		all, _ := flatten(n.entry.In, nil)
		for _, f := range all {
			if f.json == n.arg {
				return f.gen.Tag.Get("help")
			}
		}
	}
	for _, c := range n.children {
		if h := placeholderHelp(&node{arg: n.arg, entry: c.entry, children: c.children}); h != "" {
			return h
		}
	}
	return ""
}

func fieldsOf(t reflect.Type) []reflect.StructField {
	out := make([]reflect.StructField, t.NumField())
	for i := range out {
		out[i] = t.Field(i)
	}
	return out
}

// argFieldName turns a placeholder like "id" or "event_id" into "PId" or
// "PEventId".
func argFieldName(s string) string {
	var b strings.Builder
	b.WriteString("P")
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' }) {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// selected returns the operation leaf kong selected, or nil for a
// hand-written command.
func (a *app) selected(kctx *kong.Context) *leaf {
	var words []string
	for n := kctx.Selected(); n != nil && n.Type != kong.ApplicationNode; n = n.Parent {
		if n.Type == kong.CommandNode {
			words = append([]string{n.Name}, words...)
		}
	}
	return a.leaves[strings.Join(words, " ")]
}

// input builds the typed operation input from the parsed command line and
// validates it through the same schema as HTTP and MCP.
func (a *app) input(kctx *kong.Context, l *leaf) (any, error) {
	in := l.entry.NewInput()
	iv := reflect.ValueOf(in).Elem()
	given := givenValues(kctx)
	// explicit maps the json name of each input field the caller gave to its
	// index in the input.
	explicit := map[string]explicitField{}
	for i, f := range l.fields {
		fv := l.v.Field(i)
		iv.FieldByIndex(f.input).Set(fv)
		if given[fv.UnsafeAddr()] {
			explicit[f.json] = explicitField{input: f.input, stringTag: f.stringTag}
		}
	}
	for _, p := range l.args {
		iv.FieldByIndex(p.input).Set(p.v)
		explicit[p.json] = explicitField{input: p.input, stringTag: p.stringTag}
	}
	for _, g := range a.globals {
		f, ok := l.globals[g.json]
		if ok && given[g.v.UnsafeAddr()] {
			iv.FieldByIndex(f.input).Set(g.v)
			explicit[g.json] = explicitField{input: f.input, stringTag: f.stringTag}
		}
	}
	raw, err := wireInput(iv, explicit)
	if err != nil {
		return nil, err
	}
	decoded, _, _, err := l.entry.Decode(raw)
	return decoded, err
}

// givenValues returns the addresses of the flags and positional arguments
// the caller gave, on the command line or through a flag's env var.
func givenValues(kctx *kong.Context) map[uintptr]bool {
	given := map[uintptr]bool{}
	add := func(v *kong.Value) {
		if v != nil && v.Target.CanAddr() {
			given[v.Target.UnsafeAddr()] = true
		}
	}
	for _, p := range kctx.Path {
		if p.Flag != nil {
			add(p.Flag.Value)
		}
		add(p.Positional)
	}
	for _, f := range kctx.Flags() {
		if envSet(f.Tag.Envs) {
			add(f.Value)
		}
	}
	return given
}

// wireInput encodes the input for Decode. A field the caller gave keeps its
// value even when it is zero and omitempty drops it, so that Decode does not
// replace an explicit 0, false or "" with the field's default.
func wireInput(iv reflect.Value, explicit map[string]explicitField) ([]byte, error) {
	raw, err := json.Marshal(iv.Interface())
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	missing := false
	for name, field := range explicit {
		if _, ok := obj[name]; ok {
			continue
		}
		if obj[name], err = marshalExplicit(iv.FieldByIndex(field.input), field.stringTag); err != nil {
			return nil, err
		}
		missing = true
	}
	if !missing {
		return raw, nil
	}
	return json.Marshal(obj)
}

func marshalExplicit(v reflect.Value, stringTag bool) ([]byte, error) {
	if !stringTag {
		return json.Marshal(v.Interface())
	}
	t := reflect.StructOf([]reflect.StructField{{
		Name: "Value",
		Type: v.Type(),
		Tag:  reflect.StructTag(`json:",string"`),
	}})
	h := reflect.New(t).Elem()
	h.Field(0).Set(v)
	b, err := json.Marshal(h.Interface())
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, err
	}
	return obj["Value"], nil
}

func envSet(names []string) bool {
	for _, n := range names {
		if _, ok := os.LookupEnv(n); ok {
			return true
		}
	}
	return false
}
