package op

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/jsonschema-go/jsonschema"
)

// anyJSON returns the schema of an output that may be any JSON value. It
// lists every JSON type rather than being the empty schema, which
// jsonschema-go encodes as the boolean true and toolkit.metadata.v1 rejects.
func anyJSON() *jsonschema.Schema {
	return &jsonschema.Schema{Types: []string{"object", "array", "string", "number", "boolean", "null"}}
}

// schemaFor infers the JSON Schema of t, copies kong help tags into property
// descriptions and kong default tags into defaults, and drops fields with a
// default from required (a missing value takes the default on every surface).
// An interface type such as any, and json.RawMessage at any depth, get
// anyJSON.
func schemaFor(t reflect.Type) (*jsonschema.Schema, error) {
	if t.Kind() == reflect.Interface {
		return anyJSON(), nil
	}
	s, err := jsonschema.ForType(t, &jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[json.RawMessage](): anyJSON()},
	})
	if err != nil {
		return nil, err
	}
	if err := annotate(s, t); err != nil {
		return nil, err
	}
	return s, nil
}

func annotate(s *jsonschema.Schema, t reflect.Type) error {
	if s == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		for name, f := range jsonFields(t) {
			p, ok := s.Properties[name]
			if !ok {
				continue
			}
			if err := annotate(p, f.Type); err != nil {
				return err
			}
			if p.Description == "" {
				p.Description = f.Tag.Get("help")
			}
			stringTag := hasJSONOption(f.Tag.Get("json"), "string") && jsonStringType(f.Type)
			if stringTag {
				p.Type = "string"
				p.Types = nil
			}
			if def, ok := f.Tag.Lookup("default"); ok {
				v, err := parseDefault(f.Type, def)
				if err != nil {
					return fmt.Errorf("field %s: %w", f.Name, err)
				}
				b, err := marshalDefault(v, stringTag)
				if err != nil {
					return fmt.Errorf("field %s: %w", f.Name, err)
				}
				p.Default = b
				s.Required = slices.DeleteFunc(s.Required, func(r string) bool { return r == name })
			}
		}
	case reflect.Array, reflect.Slice:
		if s.Items != nil {
			return annotate(s.Items, t.Elem())
		}
		for _, item := range s.ItemsArray {
			if err := annotate(item, t.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		if s.AdditionalProperties != nil {
			return annotate(s.AdditionalProperties, t.Elem())
		}
	}
	return nil
}

func marshalDefault(v reflect.Value, stringTag bool) ([]byte, error) {
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

func hasJSONOption(tag, want string) bool {
	parts := strings.Split(tag, ",")
	for _, part := range parts[1:] {
		if part == want {
			return true
		}
	}
	return false
}

func jsonStringType(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// jsonFields maps wire names to struct fields, following encoding/json:
// exported fields, json:"-" skipped, anonymous structs without a json name
// flattened. Each field's Index is its full path from t.
func jsonFields(t reflect.Type) map[string]reflect.StructField {
	out := map[string]reflect.StructField{}
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if f.Anonymous && name == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k, v := range jsonFields(ft) {
					if _, ok := out[k]; !ok {
						v.Index = append([]int{i}, v.Index...)
						out[k] = v
					}
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f
	}
	return out
}

// cliOnlyInput is an input field tagged toolkit:"cli-only".
type cliOnlyInput struct {
	json  string
	flag  string
	index []int
}

// tagCLIOnly is the value of the toolkit struct tag that marks an input field
// as accepted only on the command line: toolkit:"cli-only".
const tagCLIOnly = "cli-only"

// cliOnlyInputs finds the fields tagged toolkit:"cli-only" in t, sorted by
// json name. Such a field must be optional and have no default: a remote
// caller can never set it, so a required one could never be satisfied, and
// a default would be a path every remote call carries.
func cliOnlyInputs(t reflect.Type, s *jsonschema.Schema) ([]cliOnlyInput, error) {
	var out []cliOnlyInput
	for name, f := range jsonFields(t) {
		tag, ok := f.Tag.Lookup("toolkit")
		if !ok {
			continue
		}
		if tag != tagCLIOnly {
			return nil, fmt.Errorf("field %s: unknown toolkit tag %q, want %q", f.Name, tag, tagCLIOnly)
		}
		if _, ok := s.Properties[name]; !ok {
			return nil, fmt.Errorf("field %s: a cli-only input must be a wire property", f.Name)
		}
		if slices.Contains(s.Required, name) {
			return nil, fmt.Errorf("field %s: a cli-only input must be optional (json omitempty), since HTTP and MCP callers can never set it", f.Name)
		}
		if _, ok := f.Tag.Lookup("default"); ok {
			return nil, fmt.Errorf("field %s: a cli-only input must not have a default, since every HTTP and MCP call would carry it", f.Name)
		}
		out = append(out, cliOnlyInput{json: name, flag: cliSpelling(f), index: f.Index})
	}
	slices.SortFunc(out, func(a, b cliOnlyInput) int { return strings.Compare(a.json, b.json) })
	return out, nil
}

// cliSpelling is how kong spells the field on the command line: --name for
// a flag, <name> for a positional argument.
func cliSpelling(f reflect.StructField) string {
	name := f.Tag.Get("name")
	if name == "" {
		name = strings.ToLower(strings.Join(camelCase(f.Name), "-"))
	}
	if _, ok := f.Tag.Lookup("arg"); ok {
		return "<" + name + ">"
	}
	return "--" + name
}

// camelCase splits a Go identifier into words as kong does for flag names,
// so "IDsFrom" is "I", "Ds", "From" and "HTTPServer" is "HTTP", "Server".
func camelCase(s string) []string {
	var runs [][]rune
	last := 0
	for _, r := range s {
		class := 4
		switch {
		case unicode.IsLower(r):
			class = 1
		case unicode.IsUpper(r):
			class = 2
		case unicode.IsDigit(r):
			class = 3
		}
		if class == last {
			runs[len(runs)-1] = append(runs[len(runs)-1], r)
		} else {
			runs = append(runs, []rune{r})
		}
		last = class
	}
	for i := 0; i < len(runs)-1; i++ {
		if unicode.IsUpper(runs[i][0]) && unicode.IsLower(runs[i+1][0]) {
			runs[i+1] = append([]rune{runs[i][len(runs[i])-1]}, runs[i+1]...)
			runs[i] = runs[i][:len(runs[i])-1]
		}
	}
	var out []string
	for _, r := range runs {
		if len(r) > 0 {
			out = append(out, string(r))
		}
	}
	return out
}

// newInput returns a pointer to a zero T with kong default tags applied, so
// HTTP and MCP callers get the same defaults as the CLI.
func newInput(t reflect.Type) (any, error) {
	v := reflect.New(t)
	if err := applyDefaults(v.Elem()); err != nil {
		return nil, err
	}
	return v.Interface(), nil
}

func applyDefaults(v reflect.Value) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			if !typeHasDefaults(v.Type().Elem(), map[reflect.Type]bool{}) {
				return nil
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		fv := v.Field(i)
		// An embedded struct's exported fields are promoted even when the
		// struct type itself is unexported, as in encoding/json.
		if f.Type.Kind() == reflect.Struct && (f.Anonymous || f.Tag.Get("embed") != "") {
			if err := applyDefaults(fv); err != nil {
				return err
			}
			continue
		}
		if !f.IsExported() || !fv.CanSet() {
			continue
		}
		if f.Type.Kind() == reflect.Struct {
			if err := applyDefaults(fv); err != nil {
				return err
			}
			continue
		}
		if f.Type.Kind() == reflect.Pointer {
			if typeHasDefaults(f.Type, map[reflect.Type]bool{}) {
				if err := applyDefaults(fv); err != nil {
					return err
				}
			}
			continue
		}
		def, ok := f.Tag.Lookup("default")
		if !ok {
			continue
		}
		d, err := parseDefault(f.Type, def)
		if err != nil {
			return fmt.Errorf("field %s: %w", f.Name, err)
		}
		fv.Set(d)
	}
	return nil
}

func typeHasDefaults(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return false
	}
	seen[t] = true
	defer delete(seen, t)
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if _, ok := f.Tag.Lookup("default"); ok {
			return true
		}
		if f.Type.Kind() == reflect.Struct || f.Type.Kind() == reflect.Pointer {
			if typeHasDefaults(f.Type, seen) {
				return true
			}
		}
	}
	return false
}

func parseDefault(t reflect.Type, s string) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return v, err
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, t.Bits())
		if err != nil {
			return v, err
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(s, 10, t.Bits())
		if err != nil {
			return v, err
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, t.Bits())
		if err != nil {
			return v, err
		}
		v.SetFloat(n)
	default:
		return v, fmt.Errorf("default tag is supported only on string, bool and number fields, not %s", t)
	}
	return v, nil
}

// decodeObject decodes a JSON object as json.Unmarshal into map[string]any
// does, except that an integer that fits int64 or uint64 keeps that type
// instead of float64. The value then reaches the typed input exactly, even
// above 2^53, and the schema validator still sees a number.
func decodeObject(raw []byte) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false // trailing data, which json.Unmarshal also rejects
	}
	if _, ok := exactNumbers(m); !ok {
		return nil, false
	}
	return m, true
}

// exactNumbers replaces each json.Number in v, in place, with the int64,
// uint64 or float64 it parses as. It fails when a number overflows float64.
func exactNumbers(v any) (any, bool) {
	var ok bool
	switch v := v.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, true
		}
		if u, err := strconv.ParseUint(v.String(), 10, 64); err == nil {
			return u, true
		}
		f, err := v.Float64()
		return f, err == nil
	case map[string]any:
		for k, x := range v {
			if v[k], ok = exactNumbers(x); !ok {
				return nil, false
			}
		}
	case []any:
		for i, x := range v {
			if v[i], ok = exactNumbers(x); !ok {
				return nil, false
			}
		}
	}
	return v, true
}

func cloneSchema(s *jsonschema.Schema) *jsonschema.Schema {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	var c jsonschema.Schema
	if err := json.Unmarshal(b, &c); err != nil {
		panic(err)
	}
	return &c
}
