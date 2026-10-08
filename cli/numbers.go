package cli

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/kong"
)

// kong reads every argument that starts with "-" as a flag, so a negative
// number fails both as a flag value (--min -5) and as a positional argument
// (temp -40). kong.WithHyphenPrefixedParameters fixes only the first, and
// makes a value flag swallow whatever follows it, such as --tag -j. Instead,
// Run marks each argument shaped like a number before parsing, so kong sees a
// value, and every value's mapper removes the mark before decoding. kong
// still decides whether the number belongs to a flag or a positional.

// numberMark prefixes a marked argument. Process arguments never contain NUL.
const numberMark = "\x00"

// numberRE matches a negative decimal number: -40, -0.5, -.5, -1e3.
var numberRE = regexp.MustCompile(`^-(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?$`)

// markNumbers marks each argument before "--" that is a negative number and
// does not start with a declared short flag, such as a tool's -1. shorts
// covers the whole command tree, since kong has not yet chosen a command.
func markNumbers(args []string, shorts map[rune]bool) []string {
	out := slices.Clone(args)
	for i, arg := range out {
		if arg == "--" {
			break
		}
		if numberRE.MatchString(arg) && !shorts[rune(arg[1])] {
			out[i] = numberMark + arg
		}
	}
	return out
}

// unmarkNumbers replaces every marked argument left in scan with the number
// as a positional token. That is how kong already treats an unmarked
// argument that does not start with "-", and every mapper accepts it as a
// value, while a bool flag, which consumes only --flag=value, leaves it alone.
func unmarkNumbers(scan *kong.Scanner) {
	if !slices.ContainsFunc(scan.PeekAll(), marked) {
		return
	}
	var tokens []kong.Token
	for !scan.Peek().IsEOL() {
		tokens = append(tokens, scan.Pop())
	}
	for _, t := range slices.Backward(tokens) {
		if marked(t) {
			t = kong.Token{Value: strings.TrimPrefix(t.Value.(string), numberMark), Type: kong.PositionalArgumentToken}
		}
		scan.PushToken(t)
	}
}

func marked(t kong.Token) bool {
	s, ok := t.Value.(string)
	return ok && strings.HasPrefix(s, numberMark)
}

type numberMapper struct{ kong.Mapper }

func (m numberMapper) Decode(ctx *kong.DecodeContext, target reflect.Value) error {
	unmarkNumbers(ctx.Scan)
	return m.Mapper.Decode(ctx, target)
}

// numberPlaceHolderMapper keeps a mapper's help placeholder.
type numberPlaceHolderMapper struct {
	numberMapper
	kong.PlaceHolderProvider
}

// prepareNumbers wraps the mapper of every flag and positional argument in k
// so that it unmarks numbers, and returns the declared short flags. Bool
// mappers are left alone: kong recognises a bool flag by its mapper's type,
// and a bool flag never consumes a marked argument.
func prepareNumbers(k *kong.Kong) map[rune]bool {
	shorts := map[rune]bool{}
	wrap := func(v *kong.Value) {
		switch v.Mapper.(type) {
		case nil, numberMapper, numberPlaceHolderMapper:
			return
		}
		if v.IsBool() {
			return
		}
		if p, ok := v.Mapper.(kong.PlaceHolderProvider); ok {
			v.Mapper = numberPlaceHolderMapper{numberMapper{v.Mapper}, p}
		} else {
			v.Mapper = numberMapper{v.Mapper}
		}
	}
	flag := func(f *kong.Flag) {
		wrap(f.Value)
		if f.Short != 0 {
			shorts[f.Short] = true
		}
		for _, alias := range f.Aliases {
			if utf8.RuneCountInString(alias) == 1 {
				r, _ := utf8.DecodeRuneInString(alias)
				shorts[r] = true
			}
		}
	}
	if k.Model.HelpFlag != nil {
		flag(k.Model.HelpFlag)
	}
	var walk func(n *kong.Node)
	walk = func(n *kong.Node) {
		for _, f := range n.Flags {
			flag(f)
		}
		for _, p := range n.Positional {
			wrap(p)
		}
		if n.Argument != nil {
			wrap(n.Argument)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(k.Model.Node)
	return shorts
}
