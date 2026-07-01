package ast

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/iceisfun/gojs/token"
)

// Dump returns a human-readable, indented tree rendering of an AST node. It is
// intended for debugging and for the CLI's --ast mode; the format is not
// stable and should not be parsed.
//
// The dumper is reflection-based so it automatically covers every node type
// without a per-type visitor: for each node it prints the concrete type name
// followed by its exported fields, recursing into nested [Node] values, slices,
// and pointers.
func Dump(n Node) string {
	var b strings.Builder
	dumpValue(&b, reflect.ValueOf(n), 0)
	return b.String()
}

// dumpValue renders a reflected value at the given indentation depth.
func dumpValue(b *strings.Builder, v reflect.Value, depth int) {
	// Unwrap interfaces and pointers.
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr {
		if v.IsNil() {
			b.WriteString("nil")
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		dumpStruct(b, v, depth)
	case reflect.Slice:
		if v.Len() == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[")
		for i := 0; i < v.Len(); i++ {
			b.WriteString("\n")
			b.WriteString(indent(depth + 1))
			dumpValue(b, v.Index(i), depth+1)
		}
		b.WriteString("\n")
		b.WriteString(indent(depth))
		b.WriteString("]")
	default:
		fmt.Fprintf(b, "%v", v.Interface())
	}
}

// dumpStruct renders a struct node, printing its type name and exported fields.
// token.Pos values are collapsed to a compact line:col suffix, and empty/zero
// fields are omitted to keep the output readable.
func dumpStruct(b *strings.Builder, v reflect.Value, depth int) {
	t := v.Type()

	// Collapse a token.Pos to "line:col".
	if t == reflect.TypeOf(token.Pos{}) {
		p := v.Interface().(token.Pos)
		fmt.Fprintf(b, "%d:%d", p.Line, p.Column)
		return
	}

	b.WriteString(t.Name())
	b.WriteString(" {")
	wrote := false
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		fv := v.Field(i)
		if isZeroish(fv) {
			continue
		}
		wrote = true
		b.WriteString("\n")
		b.WriteString(indent(depth + 1))
		b.WriteString(f.Name)
		b.WriteString(": ")
		dumpValue(b, fv, depth+1)
	}
	if wrote {
		b.WriteString("\n")
		b.WriteString(indent(depth))
	}
	b.WriteString("}")
}

// isZeroish reports whether a field should be omitted from the dump (nil
// pointers/interfaces, empty slices, empty strings, and zero token.Pos).
func isZeroish(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.Slice:
		return v.Len() == 0
	case reflect.String:
		return v.Len() == 0
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(token.Pos{}) {
			return v.Interface().(token.Pos) == token.Pos{}
		}
	}
	return false
}

func indent(depth int) string { return strings.Repeat("  ", depth) }
