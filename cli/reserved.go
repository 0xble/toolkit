package cli

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
)

// checkFlags rejects a command flag that reuses the name, an alias or the
// short form of a root flag: the toolkit's builtins, --help and the tool's
// Globals. kong parses such a flag as the root flag, so the command would
// silently never see it. An operation's input also may not declare --apply
// or --dry-run, which the toolkit generates for writes.
func (a *app) checkFlags(k *kong.Kong) error {
	toolkit := map[uintptr]bool{}
	rv := reflect.ValueOf(&a.root).Elem()
	for i := range rv.NumField() {
		toolkit[rv.Field(i).UnsafeAddr()] = true
	}
	names, shorts := map[string]string{}, map[rune]string{}
	rootFlags := slices.Clone(k.Model.Flags)
	if h := k.Model.HelpFlag; h != nil && !slices.Contains(rootFlags, h) {
		rootFlags = append(rootFlags, h)
	}
	for _, f := range rootFlags {
		owner := "the tool's root flag --" + f.Name
		if f == k.Model.HelpFlag || (f.Target.CanAddr() && toolkit[f.Target.UnsafeAddr()]) {
			owner = "the toolkit's root flag --" + f.Name
		}
		for _, n := range append([]string{f.Name}, f.Aliases...) {
			names[n] = owner
		}
		if f.Short != 0 {
			shorts[f.Short] = owner
		}
	}
	var walk func(n *kong.Node, words []string) error
	walk = func(n *kong.Node, words []string) error {
		if n.Type == kong.CommandNode {
			words = append(words, n.Name)
		}
		l := a.leaves[strings.Join(words, " ")]
		if n.Type != kong.CommandNode {
			l = nil
		}
		what := "command " + strings.Join(words, " ")
		if l != nil {
			what = "operation " + l.entry.Name
		}
		for _, f := range n.Flags {
			if l != nil && (sameField(f.Target, l.apply) || sameField(f.Target, l.dryRun)) {
				continue
			}
			for _, name := range append([]string{f.Name}, f.Aliases...) {
				owner, ok := names[name]
				if !ok && l != nil && (name == "apply" || name == "dry-run") {
					owner, ok = "the toolkit's reserved --"+name, true
				}
				if ok {
					return fmt.Errorf("%s: flag --%s collides with %s, which kong would parse instead; rename it with a name tag", what, name, owner)
				}
			}
			if owner, ok := shorts[f.Short]; ok && f.Short != 0 {
				return fmt.Errorf("%s: short flag -%c of --%s collides with %s, which kong would parse instead; choose another short tag", what, f.Short, f.Name, owner)
			}
		}
		for _, c := range n.Children {
			if err := walk(c, words); err != nil {
				return err
			}
		}
		return nil
	}
	for _, c := range k.Model.Children {
		if err := walk(c, nil); err != nil {
			return err
		}
	}
	return nil
}

func sameField(a, b reflect.Value) bool {
	return a.IsValid() && b.IsValid() && a.CanAddr() && b.CanAddr() && a.UnsafeAddr() == b.UnsafeAddr()
}
