package interp

import "github.com/iceisfun/gojs/ast"

// This file plans slot-based locals for the bytecode VM. The name-based VM
// (bc_vm.go) resolves every variable by walking a map[string]*binding chain; the
// single biggest speedup available is to give a function's locals fixed frame
// slots (an array index) instead. This is the scope-resolution pass the design
// doc calls the "real prize".
//
// It is applied conservatively so the escape hatch stays correct: a slot local is
// invisible to an opEvalNode/opEvalStmt subtree (those resolve by name via env),
// so slots are used ONLY when the whole body compiles with no fallback, no
// let/const, no nested function, and no `arguments`. The compiler enforces exactly
// that by aborting slot-mode compilation the moment it would emit a fallback (see
// bc_compiler.go); this file only assigns the slot indices.

// slotPlan maps a slot-eligible function's parameter and function-scope var names
// to frame slot indices. Parameters occupy the first slots (by first appearance);
// paramSlot[pos] is the slot for the parameter at position pos, so a duplicated
// simple parameter name (legal in sloppy mode) resolves to one shared slot with
// last-write-wins semantics.
type slotPlan struct {
	byName    map[string]int
	slotName  []string
	paramSlot []int
	numParams int
}

// planSlots assigns slots for def's parameters and function-scope var names, or
// returns nil when the parameter list is not all simple identifiers (defaults,
// rest, and destructuring keep the function on the name-based path). A nil plan
// just means "don't try slot mode"; the compiler still decides eligibility by
// whether the body compiles without a fallback.
func planSlots(def *ast.FuncDef) *slotPlan {
	if def.Body == nil {
		return nil
	}
	for _, p := range def.Params {
		if _, ok := p.(*ast.Ident); !ok {
			return nil
		}
	}
	sp := &slotPlan{byName: make(map[string]int)}
	add := func(name string) int {
		if s, ok := sp.byName[name]; ok {
			return s
		}
		s := len(sp.slotName)
		sp.byName[name] = s
		sp.slotName = append(sp.slotName, name)
		return s
	}
	for _, p := range def.Params {
		sp.paramSlot = append(sp.paramSlot, add(p.(*ast.Ident).Name))
	}
	sp.numParams = len(def.Params)
	// Function-scope var names (recursive through blocks, not into nested
	// functions — which slot mode rejects anyway).
	varNames := map[string]bool{}
	collectVarNames(def.Body.Body, varNames)
	// A `var arguments` (when not also a parameter) is initialized to the
	// activation's arguments object, not undefined — slot mode would wrongly
	// hoist it to undefined. Decline so name mode binds the real arguments object.
	if varNames["arguments"] && !paramHasName(def.Params, "arguments") {
		return nil
	}
	for n := range varNames {
		add(n)
	}
	return sp
}

// paramHasName reports whether a simple parameter list binds name.
func paramHasName(params []ast.Expr, name string) bool {
	for _, p := range params {
		if id, ok := p.(*ast.Ident); ok && id.Name == name {
			return true
		}
	}
	return false
}
