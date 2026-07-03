package parser

import "testing"

// TestDynamicImportValid checks that the well-formed dynamic import forms parse
// without error: a required specifier, an optional trailing comma, an optional
// second (options/attributes) argument, and the import.meta meta-property.
func TestDynamicImportValid(t *testing.T) {
	valid := []string{
		`import('./mod.js');`,
		`import('./mod.js',);`,
		`import('./mod.js', {});`,
		`import('./mod.js', {},);`,
		`import(x);`,
		`import(x + y);`,
		`import.meta;`,
		`import.meta.url;`,
		`x = import('./mod.js');`,
	}
	for _, src := range valid {
		if _, err := Parse("test", src); err != nil {
			t.Errorf("Parse(%q) = %v, want success", src, err)
		}
	}
}

// TestDynamicImportInvalid checks the early errors around ImportCall: a missing
// specifier, spread/rest arguments, too many arguments, `new` applied to an
// ImportCall, a bare `import`, and import.<non-meta> member access (including the
// unsupported import.source / import.defer proposal forms).
func TestDynamicImportInvalid(t *testing.T) {
	invalid := []string{
		`import();`,                     // AssignmentExpression is not optional
		`import(...['x']);`,             // ImportCall is not extensible: no rest
		`import('a', 'b', 'c');`,        // ImportCall takes at most two arguments
		`new import('');`,               // ImportCall cannot be a `new` callee
		`new import('').prop;`,          // ...even with a trailing member access
		`typeof import;`,                // bare `import` is not an expression
		`import.UNKNOWN('x');`,          // import.<non-meta> is a SyntaxError
		`typeof import.source;`,         // import.source is not supported
		`typeof import.source.UNKNOWN;`, // ...nor any property of it
		`import.defer('x');`,            // import.defer is not supported
		`import.source('x');`,           // import.source is not supported
		`new import.defer('');`,         // new import.defer(...) is a SyntaxError
		`new import.source('');`,        // new import.source(...) is a SyntaxError
	}
	for _, src := range invalid {
		if _, err := Parse("test", src); err == nil {
			t.Errorf("Parse(%q) = nil error, want SyntaxError", src)
		}
	}
}
