package parser

import "testing"

// Static-semantic rules on class element names (ECMA-262 15.7.1):
//   - a field may not be named "constructor" (static or instance);
//   - the constructor must be a plain method, not an accessor/generator/async;
//   - a static element may not be named "prototype";
//   - a private element may not be named "#constructor".
//
// Computed names are exempt from all of these.
func TestClassReservedElementNames(t *testing.T) {
	bad := []string{
		`class C { constructor; }`,
		`class C { static constructor; }`,
		`class C { 'constructor'; }`,
		`class C { get constructor() {} }`,
		`class C { set constructor(v) {} }`,
		`class C { *constructor() {} }`,
		`class C { async constructor() {} }`,
		`class C { async *constructor() {} }`,
		`class C { static prototype() {} }`,
		`class C { static prototype; }`,
		`class C { static 'prototype'; }`,
		`class C { static async *prototype() {} }`,
		`class C { #constructor() {} }`,
		`class C { #constructor = 1; }`,
		`class C { constructor() {} constructor() {} }`,
	}
	for _, src := range bad {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("expected SyntaxError: %s", src)
		}
	}
	good := []string{
		`class C { constructor() {} }`,
		`class C { static constructor() {} }`, // a static method named constructor is fine
		`class C { prototype() {} }`,          // a non-static prototype is fine
		`class C { prototype = 1; }`,
		`class C { ['constructor']() {} }`, // computed names are exempt
		`class C { ['constructor'] = 1; }`,
		`class C { static ['prototype']() {} }`,
		`class C { constructor() {} static constructor() {} }`,
	}
	for _, src := range good {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("valid class wrongly rejected: %s -> %v", src, err)
		}
	}
}
