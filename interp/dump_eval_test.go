package interp

import (
	"strings"
	"testing"
)

// TestWithDumpEval covers the eval/Function compilation observer: it fires for
// indirect eval, direct eval, and the Function constructor (including the
// `fn.constructor(...)` obfuscation idiom), delivering the source and a parsed
// AST, and reports parse failures with Err set and Program nil.
func TestWithDumpEval(t *testing.T) {
	var got []EvalInfo
	vm := New(WithDumpEval(func(info EvalInfo) { got = append(got, info) }))
	defer vm.Close()

	src := `
		var ev = eval; ev("1 + 2");                 // indirect eval (aliased callee)
		eval("var q = 3;");                          // direct eval (callee is 'eval')
		Function("a", "b", "return a + b")(2, 3);   // Function constructor
		(function(){}).constructor("return 41")();  // the obfuscator idiom
		try { (0, eval)("("); } catch (e) {}        // an indirect parse error
	`
	if _, err := vm.RunString("t.js", src); err != nil {
		t.Fatal(err)
	}

	// Bucket by kind.
	byKind := map[EvalKind][]EvalInfo{}
	for _, g := range got {
		byKind[g.Kind] = append(byKind[g.Kind], g)
	}

	// Indirect evals: the "1 + 2" success and the "(" parse failure.
	ind := byKind[EvalIndirect]
	if len(ind) != 2 {
		t.Fatalf("indirect eval count = %d, want 2 (%+v)", len(ind), ind)
	}
	if ind[0].Source != "1 + 2" || ind[0].Program == nil || ind[0].Err != nil {
		t.Errorf("first indirect eval = %+v, want source %q with a Program and no Err", ind[0], "1 + 2")
	}
	if ind[1].Err == nil || ind[1].Program != nil {
		t.Errorf("parse-failing eval = %+v, want Err set and Program nil", ind[1])
	}

	// Direct eval.
	if d := byKind[EvalDirect]; len(d) != 1 || d[0].Source != "var q = 3;" || d[0].Program == nil {
		t.Errorf("direct eval = %+v, want one with source %q and a Program", d, "var q = 3;")
	}

	// Function constructor: the explicit Function(...) and the constructor idiom.
	fn := byKind[EvalFunction]
	if len(fn) != 2 {
		t.Fatalf("Function count = %d, want 2 (%+v)", len(fn), fn)
	}
	for _, f := range fn {
		if f.Program == nil || f.Err != nil {
			t.Errorf("Function compile = %+v, want a Program and no Err", f)
		}
	}
	if !strings.Contains(fn[0].Source, "return a + b") {
		t.Errorf("first Function source = %q, want it to contain the body", fn[0].Source)
	}
	if !strings.Contains(fn[1].Source, "return 41") {
		t.Errorf("constructor-idiom source = %q, want it to contain the body", fn[1].Source)
	}
}

// TestWithDumpEvalDisabledByDefault confirms the observer is inert when not
// installed (no panic, normal execution).
func TestWithDumpEvalDisabledByDefault(t *testing.T) {
	vm := New()
	defer vm.Close()
	v, err := vm.RunString("t.js", `eval("6 * 7")`)
	if err != nil {
		t.Fatal(err)
	}
	if v != Number(42) {
		t.Fatalf("got %v, want 42", v)
	}
}
