// Package jsontag holds the encoding/json struct tag rules that op and cli
// both need, so the schema and the command line read a tag the same way.
package jsontag

import (
	"reflect"
	"strings"
)

// HasOption reports whether a json struct tag lists opt after its name.
func HasOption(tag, opt string) bool {
	parts := strings.Split(tag, ",")
	for _, part := range parts[1:] {
		if part == opt {
			return true
		}
	}
	return false
}

// Quoted reports whether encoding/json encodes f as a JSON string because of
// a ",string" option. Like encoding/json, it follows one unnamed pointer and
// honours the option only on string, boolean, integer and floating-point
// fields, ignoring it everywhere else.
func Quoted(f reflect.StructField) bool {
	if !HasOption(f.Tag.Get("json"), "string") {
		return false
	}
	t := f.Type
	if t.Name() == "" && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	}
	return false
}
