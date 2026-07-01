package harness

import "testing"

// These tests guard for-in statement semantics and the many early (parse-time)
// SyntaxErrors mandated for the for-in head and body. Each was added as a
// failing regression before the corresponding parser/interpreter fix.

// --- Enumeration semantics -------------------------------------------------

func TestForInEnumeration(t *testing.T) {
	Expect(t, `
		// own + inherited enumerable string keys, integer indices first (ascending),
		// then string keys in insertion order; shadowed keys not revisited.
		var proto = { z: 1, a: 2 };
		var obj = Object.create(proto);
		obj.b = 3; obj.a = 4; // shadows proto.a
		var out = [];
		for (var k in obj) out.push(k);
		assert.sameValue(out.join(","), "b,a,z");

		// non-enumerable keys are skipped
		var o2 = {};
		Object.defineProperty(o2, "hidden", { value: 1, enumerable: false });
		o2.shown = 2;
		var seen = [];
		for (var k2 in o2) seen.push(k2);
		assert.sameValue(seen.join(","), "shown");

		// integer index ordering
		var o3 = {}; o3.x = 1; o3[2] = 1; o3[1] = 1; o3.y = 1;
		var ord = [];
		for (var k3 in o3) ord.push(k3);
		assert.sameValue(ord.join(","), "1,2,x,y");
	`)
}

func TestForInSparseArraySkipsHoles(t *testing.T) {
	Expect(t, `
		var out = [];
		for (var i in [10, , 30]) out.push(i);
		assert.sameValue(out.join(","), "0,2");
	`)
}

func TestForInDeleteDuringEnumeration(t *testing.T) {
	// A property deleted before it is visited must not be visited.
	Expect(t, `
		var obj = Object.create(null);
		obj.aa = 1; obj.ba = 2; obj.ca = 3;
		var accum = "";
		for (var key in obj) {
			delete obj.ba;
			accum += key + obj[key];
		}
		assert(accum === "aa1ca3" || accum === "ca3aa1", "got '" + accum + "'");
	`)
}

// --- Completion value ------------------------------------------------------

func TestForInCompletionValue(t *testing.T) {
	Expect(t, `
		assert.sameValue(eval('1; for (var a in { x: 0 }) { }'), undefined);
		assert.sameValue(eval('2; for (var b in { x: 0 }) { 3; }'), 3);
		assert.sameValue(eval('var a; 1; for (a in { x: 0 }) { break; }'), undefined);
		assert.sameValue(eval('var b; 2; for (b in { x: 0 }) { 3; break; }'), 3);
		assert.sameValue(
			eval('var b; 5; outer: do { for (b in { x: 0 }) { 6; continue outer; } } while (false)'),
			6
		);
	`)
}

// --- Early errors: body must be a Statement, not a Declaration -------------

func TestForInBodyDeclarationEarlyErrors(t *testing.T) {
	for _, src := range []string{
		`for (var x in {}) let y;`,
		`for (var x in {}) const y = null;`,
		`for (var x in {}) function f() {}`,
		`for (var x in {}) function* f() {}`,
		`for (var x in {}) async function f() {}`,
		`for (var x in {}) class C {}`,
		// labelled function statement is an early error regardless of mode
		`for (const x in {}) label1: label2: function f() {}`,
		`for (x in {}) label1: label2: function f() {}`,
	} {
		ExpectError(t, src, "SyntaxError")
	}
	// a bare `var` body and a labelled non-function body are legal
	Expect(t, `for (var x in {a:1}) var y = x; for (var z in {a:1}) lbl: z;`)
}

// --- Early errors: for-in head left-hand side ------------------------------

func TestForInHeadLHSEarlyErrors(t *testing.T) {
	for _, src := range []string{
		`for (this in {}) {}`,
		`for ((this) in {}) {}`,
		`for (1 in {}) {}`,
		`for (x + y in {}) {}`,
		`for ([(x, y)] in {}) {}`,
		`for ([[(x, y)]] in {}) {}`,
		`for ({ m() {} } in {}) {}`,
		`for ({ get x() {} } in {}) {}`,
		`for ({ x: { get x() {} } } in {}) {}`,
	} {
		ExpectError(t, src, "SyntaxError")
	}
	// valid targets parse and run
	Expect(t, `var o = {}; for (o.p in {a:1}) {} for (o["q"] in {a:1}) {}`)
}

// --- Early errors: destructuring assignment pattern rest rules -------------

func TestForInHeadRestEarlyErrors(t *testing.T) {
	for _, src := range []string{
		`for ([...x, y] in [[]]) ;`,
		`for ([...x, ...y] in [[]]) ;`,
		`var x; for ([...x = 1] in [[]]) ;`,
		`var rest, b; for ({...rest, b} in [{}]) ;`,
		`for ([...[x](y)] in [[]]) ;`,
	} {
		ExpectError(t, src, "SyntaxError")
	}
	// valid rest patterns run
	Expect(t, `var a, b; for ([a, ...b] in [[1,2,3]]) {} for ({a, ...b} in [{a:1,c:2}]) {}`)
}

// --- Early errors: let/const ForDeclaration bound names --------------------

func TestForInBoundNamesEarlyErrors(t *testing.T) {
	for _, src := range []string{
		`for (let [x, x] in {}) {}`,
		`for (const [x, x] in {}) {}`,
		`for (let {a: x, b: x} in {}) {}`,
		`for (let let in {}) {}`,
		`for (const let in {}) {}`,
		// a var in the body may not redeclare a lexical head binding
		`for (let x in {}) { var x; }`,
		`for (const x in {}) { { var x; } }`,
	} {
		ExpectError(t, src, "SyntaxError")
	}
	// a var in the body that does not collide is fine
	Expect(t, `for (let x in {a:1}) { var y = x; }`)
}

// --- `let` as an identifier in the for-in head (not a declaration) ---------

func TestForInLetAsIdentifier(t *testing.T) {
	Expect(t, `
		var count = 0;
		for (let in { a: 1, b: 2 }) count++;
		assert.sameValue(count, 2);
		// the identifier 'let' received the last enumerated key
		assert.sameValue(let, "b");
	`)
	// [let][1] is an array-literal member expression (a valid assignment
	// target), not a `let` declaration; the head must parse as an expression.
	Expect(t, `var let = 5; for ( [let][1] in {a:1} ) ; assert.sameValue(let, 5);`)
}
