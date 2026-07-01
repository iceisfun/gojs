package harness

import "testing"

// class_inheritance_test.go is a broad regression suite for JavaScript class,
// inheritance, and prototype semantics. It complements class_test.go, exercising
// construction, extends/super, the prototype chain, static and private members,
// accessors, field initialization ordering, class expressions, hoisting/TDZ,
// new.target, built-in subclassing, and their many error conditions.
//
// Each function drives self-asserting JS through the test262-style `assert`
// prologue injected by harness.go, so a broken invariant throws and fails the
// Go test. Behaviors that already work are kept here as regression guards.

// ---------------------------------------------------------------------------
// 1. Basic class construction
// ---------------------------------------------------------------------------

func TestClassBasicConstruction(t *testing.T) {
	Expect(t, `
		class Empty {}
		var e = new Empty();
		assert.sameValue(typeof Empty, "function", "class is callable object");
		assert.sameValue(e instanceof Empty, true, "instance of its class");
		assert.sameValue(Object.getPrototypeOf(e) === Empty.prototype, true, "proto link");
		assert.sameValue(e.constructor === Empty, true, "constructor identity");
		assert.sameValue(Empty.name, "Empty", "class name");

		class Point {
			constructor(x, y) { this.x = x; this.y = y; }
		}
		var p = new Point(3, 4);
		assert.sameValue(p.x, 3, "field x");
		assert.sameValue(p.y, 4, "field y");
		assert.sameValue(p instanceof Point, true, "instanceof Point");
		assert.sameValue(p.constructor === Point, true, "p.constructor");

		// Two instances are independent and share the same prototype.
		var q = new Point(1, 2);
		assert.notSameValue(p, q, "distinct instances");
		assert.sameValue(Object.getPrototypeOf(p) === Object.getPrototypeOf(q), true,
			"shared prototype");
	`)
}

// ---------------------------------------------------------------------------
// 2. Constructor semantics
// ---------------------------------------------------------------------------

func TestClassConstructorSemantics(t *testing.T) {
	Expect(t, `
		// Constructor runs exactly once per instantiation.
		var runs = 0;
		class Once { constructor() { runs++; } }
		new Once(); new Once();
		assert.sameValue(runs, 2, "constructor runs once per new");

		// arguments are received.
		class Sum { constructor(a, b, c) { this.total = a + b + c; } }
		assert.sameValue(new Sum(1, 2, 3).total, 6, "constructor args");

		// Explicit return of a primitive is ignored; 'this' is returned.
		class RetPrim { constructor() { this.v = 5; return 42; } }
		var rp = new RetPrim();
		assert.sameValue(rp.v, 5, "primitive return ignored");
		assert.sameValue(rp instanceof RetPrim, true, "still an instance");

		// Explicit return of an object replaces 'this'.
		var replacement = { tag: "custom" };
		class RetObj { constructor() { this.v = 1; return replacement; } }
		var ro = new RetObj();
		assert.sameValue(ro === replacement, true, "object return replaces this");
		assert.sameValue(ro.tag, "custom", "returned object's shape");
		assert.sameValue(ro.v, undefined, "this fields discarded");

		// Omitted constructor: implicit empty constructor.
		class NoCtor { greet() { return "hi"; } }
		var nc = new NoCtor();
		assert.sameValue(nc.greet(), "hi", "implicit constructor still constructs");
	`)
}

// ---------------------------------------------------------------------------
// 3. extends: chaining and inherited methods
// ---------------------------------------------------------------------------

func TestClassExtendsChain(t *testing.T) {
	Expect(t, `
		class A {
			constructor(a) { this.a = a; }
			who() { return "A"; }
			shared() { return "from A"; }
		}
		class B extends A {
			constructor(a, b) { super(a); this.b = b; }
			who() { return "B"; }
		}
		class C extends B {
			constructor(a, b, c) { super(a, b); this.c = c; }
		}
		var obj = new C(1, 2, 3);
		assert.sameValue(obj.a, 1, "A field set through two supers");
		assert.sameValue(obj.b, 2, "B field");
		assert.sameValue(obj.c, 3, "C field");
		assert.sameValue(obj.who(), "B", "most-derived override wins (C inherits B.who)");
		assert.sameValue(obj.shared(), "from A", "inherited method from grandparent");
		assert.sameValue(obj instanceof A, true, "instanceof A");
		assert.sameValue(obj instanceof B, true, "instanceof B");
		assert.sameValue(obj instanceof C, true, "instanceof C");
	`)
}

// ---------------------------------------------------------------------------
// 4. super(): mandatory before this; ordering; implicit; single-call
// ---------------------------------------------------------------------------

func TestClassSuperCallRules(t *testing.T) {
	Expect(t, `
		// Accessing this before super() throws ReferenceError.
		class Base { constructor() { this.ok = true; } }
		class EarlyThis extends Base {
			constructor() { this.x = 1; super(); }
		}
		assert.throws(ReferenceError, function() { new EarlyThis(); },
			"this before super() throws");

		// A derived constructor that never calls super() cannot use this.
		class NoSuper extends Base {
			constructor() { this.x = 1; }
		}
		assert.throws(ReferenceError, function() { new NoSuper(); },
			"accessing this without super() throws");

		// Calling super() twice throws ReferenceError.
		class TwiceSuper extends Base {
			constructor() { super(); super(); }
		}
		assert.throws(ReferenceError, function() { new TwiceSuper(); },
			"second super() throws");

		// Argument forwarding and post-super access.
		class Args extends Base {
			constructor(v) { super(); this.v = v; }
		}
		var a = new Args(7);
		assert.sameValue(a.ok, true, "super() ran the base constructor");
		assert.sameValue(a.v, 7, "this usable after super()");

		// Ordering: pre-super code runs, then base ctor, then post-super code.
		var log = [];
		class Ordered extends class { constructor() { log.push("base"); } } {
			constructor() { log.push("pre"); super(); log.push("post"); }
		}
		new Ordered();
		assert.sameValue(log.join(","), "pre,base,post", "super() ordering");
	`)
}

func TestClassImplicitDerivedConstructor(t *testing.T) {
	Expect(t, `
		// A derived class with no constructor implicitly forwards all args.
		class Base { constructor(x, y) { this.x = x; this.y = y; } }
		class Derived extends Base {}
		var d = new Derived(4, 9);
		assert.sameValue(d.x, 4, "implicit super forwards x");
		assert.sameValue(d.y, 9, "implicit super forwards y");
		assert.sameValue(d instanceof Base, true, "instanceof base");
	`)
}

// ---------------------------------------------------------------------------
// 5. super property access
// ---------------------------------------------------------------------------

func TestClassSuperPropertyAccess(t *testing.T) {
	Expect(t, `
		class Base {
			constructor() { this.n = "base"; }
			hello() { return "hello " + this.n; }
			get tag() { return "T:" + this.n; }
		}
		class Sub extends Base {
			constructor() { super(); this.n = "sub"; }
			callHello() { return super.hello(); }         // super.foo()
			readTag() { return super.tag; }               // super.bar (accessor)
			computed(k) { return super[k](); }            // super["x"]
			bindsThis() { return super.hello(); }
		}
		var s = new Sub();
		// super method/accessor run with the derived 'this'.
		assert.sameValue(s.callHello(), "hello sub", "super.method() uses derived this");
		assert.sameValue(s.readTag(), "T:sub", "super accessor uses derived this");
		assert.sameValue(s.computed("hello"), "hello sub", "computed super access");

		// super.x = v assigns through the chain (runs an inherited setter with this).
		class WithSetter {
			set value(v) { this._stored = v * 2; }
			get value() { return this._stored; }
		}
		class UsesSetter extends WithSetter {
			bump() { super.value = 21; }
		}
		var u = new UsesSetter();
		u.bump();
		assert.sameValue(u._stored, 42, "super.x = v runs inherited setter with this");
	`)
}

// ---------------------------------------------------------------------------
// 6. this semantics
// ---------------------------------------------------------------------------

func TestClassThisSemantics(t *testing.T) {
	Expect(t, `
		// Nested closures capture the instance this via a saved reference.
		class Counter {
			constructor() {
				this.count = 0;
				var self = this;
				this.inc = function() { self.count++; };
			}
		}
		var c = new Counter();
		c.inc(); c.inc();
		assert.sameValue(c.count, 2, "closure over this");

		// Arrow functions capture lexical this in a method.
		class Adder {
			constructor(base) { this.base = base; }
			makeAdder() { return (x) => this.base + x; }
		}
		var add = new Adder(10).makeAdder();
		assert.sameValue(add(5), 15, "arrow method captures this");

		// A method invoked with an explicit receiver sees that receiver.
		class Named { name() { return this.label; } }
		var n = new Named();
		assert.sameValue(n.name.call({ label: "X" }), "X", "call rebinds this");
	`)
}

// ---------------------------------------------------------------------------
// 7. Prototype chain
// ---------------------------------------------------------------------------

func TestClassPrototypeChain(t *testing.T) {
	Expect(t, `
		class Base { baseM() { return "base"; } }
		class Mid extends Base { midM() { return "mid"; } }
		class Leaf extends Mid { leafM() { return "leaf"; } }
		var leaf = new Leaf();

		// instance -> Leaf.prototype -> Mid.prototype -> Base.prototype -> Object.prototype
		assert.sameValue(Object.getPrototypeOf(leaf) === Leaf.prototype, true, "1");
		assert.sameValue(Object.getPrototypeOf(Leaf.prototype) === Mid.prototype, true, "2");
		assert.sameValue(Object.getPrototypeOf(Mid.prototype) === Base.prototype, true, "3");
		assert.sameValue(Object.getPrototypeOf(Base.prototype) === Object.prototype, true, "4");

		// Lookup walks the whole chain.
		assert.sameValue(leaf.baseM(), "base", "inherited grandparent method");
		assert.sameValue(leaf.midM(), "mid", "inherited parent method");
		assert.sameValue(leaf.leafM(), "leaf", "own method");

		// Methods live on the prototype, not the instance.
		assert.sameValue(leaf.hasOwnProperty("leafM"), false, "method not own");
		assert.sameValue(Leaf.prototype.hasOwnProperty("leafM"), true, "method on prototype");
	`)
}

// ---------------------------------------------------------------------------
// 8. instanceof
// ---------------------------------------------------------------------------

func TestClassInstanceofRelations(t *testing.T) {
	Expect(t, `
		class A {}
		class B extends A {}
		var b = new B();
		assert.sameValue(b instanceof B, true, "direct");
		assert.sameValue(b instanceof A, true, "indirect");
		assert.sameValue(b instanceof Object, true, "Object");
		assert.sameValue(B instanceof Function, true, "class is a Function");
		assert.sameValue(new A() instanceof B, false, "base not instanceof derived");
		assert.sameValue((function(){}) instanceof A, false, "unrelated object");
	`)
}

// ---------------------------------------------------------------------------
// 9. Method dispatch
// ---------------------------------------------------------------------------

func TestClassMethodDispatch(t *testing.T) {
	Expect(t, `
		class Shape {
			area() { return 0; }
			describe() { return "area=" + this.area(); }   // dynamic dispatch
		}
		class Square extends Shape {
			constructor(s) { super(); this.s = s; }
			area() { return this.s * this.s; }
		}
		var sq = new Square(3);
		assert.sameValue(sq.area(), 9, "override");
		// describe() is inherited but calls the overridden area() via this.
		assert.sameValue(sq.describe(), "area=9", "dynamic dispatch through this");

		class LoggingSquare extends Square {
			area() { return super.area() + 1; }            // super.method()
		}
		assert.sameValue(new LoggingSquare(2).area(), 5, "super.method in override");
		assert.sameValue(new LoggingSquare(2).describe(), "area=5", "dispatch reaches override");
	`)
}

// ---------------------------------------------------------------------------
// 10. Static members
// ---------------------------------------------------------------------------

func TestClassStaticMembers(t *testing.T) {
	Expect(t, `
		class Base {
			static tag = "base";
			static make() { return "made-" + this.tag; }
			static who() { return "Base"; }
		}
		class Derived extends Base {
			static who() { return "Derived:" + super.who(); }  // static super
		}
		// Static method inheritance.
		assert.sameValue(Derived.make === Base.make, true, "static method inherited");
		assert.sameValue(typeof Derived.make, "function", "static callable on derived");
		// Static field is not inherited as an own property, but is reachable.
		assert.sameValue(Base.tag, "base", "own static field");
		assert.sameValue(Derived.tag, "base", "static field reachable via proto chain");
		// this in a static method is the class it was called on.
		assert.sameValue(Base.make(), "made-base", "static this is the class");
		// static super.
		assert.sameValue(Derived.who(), "Derived:Base", "static super.method()");
		// The static side chain: Derived.__proto__ === Base.
		assert.sameValue(Object.getPrototypeOf(Derived) === Base, true, "static proto link");
		// Statics are not on instances.
		assert.sameValue(typeof (new Base()).make, "undefined", "static not on instance");
	`)
}

// ---------------------------------------------------------------------------
// 11. Private fields
// ---------------------------------------------------------------------------

func TestClassPrivateFieldsIsolation(t *testing.T) {
	Expect(t, `
		class Box {
			#value = 0;
			constructor(v) { this.#value = v; }
			get() { return this.#value; }
			set(v) { this.#value = v; return this; }
		}
		var a = new Box(1);
		var b = new Box(2);
		assert.sameValue(a.get(), 1, "a private");
		assert.sameValue(b.get(), 2, "b private (per-instance)");
		a.set(99);
		assert.sameValue(b.get(), 2, "b unaffected by a");

		// Private fields are invisible to reflection.
		assert.sameValue(Object.keys(a).length, 0, "no enumerable keys");
		assert.sameValue(a.hasOwnProperty("#value"), false, "not an own string property");
		assert.sameValue(a.hasOwnProperty("value"), false, "not exposed without #");
		assert.sameValue(JSON.stringify(a), "{}", "private field not serialized");

		// #field in obj brand check.
		class Branded {
			#brand = true;
			static has(o) { return #brand in o; }
		}
		assert.sameValue(Branded.has(new Branded()), true, "brand present");
		assert.sameValue(Branded.has({}), false, "brand absent on plain object");
		assert.sameValue(Branded.has(a), false, "brand absent on other class");
	`)
}

func TestClassPrivateFieldErrors(t *testing.T) {
	// Accessing a private field on an object that lacks the brand throws TypeError.
	Expect(t, `
		class Vault {
			#secret = 42;
			static peek(o) { return o.#secret; }
		}
		assert.sameValue(Vault.peek(new Vault()), 42, "own instance reads secret");
		assert.throws(TypeError, function() { Vault.peek({}); },
			"private access on foreign object throws TypeError");
	`)
	// Accessing a private field outside the class body is a SyntaxError.
	ExpectError(t, `class C { #x = 1; } var c = new C(); c.#x;`, "SyntaxError")
	// Duplicate private names are a SyntaxError.
	ExpectError(t, `class C { #x = 1; #x = 2; }`, "SyntaxError")
	// Deleting a private field is a SyntaxError.
	ExpectError(t, `class C { #x = 1; m() { delete this.#x; } }`, "SyntaxError")
}

// ---------------------------------------------------------------------------
// 12. Private methods
// ---------------------------------------------------------------------------

func TestClassPrivateMethods(t *testing.T) {
	Expect(t, `
		class Service {
			#count = 0;
			#tick() { this.#count++; return this.#count; }
			run() { return this.#tick() + this.#tick(); }
		}
		var s = new Service();
		assert.sameValue(s.run(), 3, "private method invoked twice (1+2)");

		// Private methods are not visible externally.
		assert.sameValue(s.hasOwnProperty("#tick"), false, "private method not own string prop");
		assert.sameValue(typeof s.tick, "undefined", "no public leak");

		// Assigning to a private method throws TypeError.
		class Locked {
			#m() { return 1; }
			bad() { this.#m = 2; }
		}
		assert.throws(TypeError, function() { new Locked().bad(); },
			"cannot assign to a private method");

		// Private accessor pair.
		class Temp {
			#c = 0;
			get #celsius() { return this.#c; }
			set #celsius(v) { this.#c = v; }
			set(v) { this.#celsius = v; return this; }
			get() { return this.#celsius; }
		}
		assert.sameValue(new Temp().set(25).get(), 25, "private get/set accessor");
	`)
}

// ---------------------------------------------------------------------------
// 13. Getters / setters
// ---------------------------------------------------------------------------

func TestClassAccessors(t *testing.T) {
	Expect(t, `
		class Base {
			get kind() { return "base"; }
			set kind(v) { this._k = v; }
		}
		class Derived extends Base {
			get kind() { return "derived:" + (this._k || "?"); }   // override getter
		}
		var d = new Derived();
		assert.sameValue(d.kind, "derived:?", "overridden getter");

		// Inherited accessor (Derived has no own setter, inherits Base's).
		var bd = new (class extends Base {})();
		bd.kind = "x";
		assert.sameValue(bd._k, "x", "inherited setter runs");
		assert.sameValue(bd.kind, "base", "inherited getter");

		// Static accessor.
		class Config {
			static _v = 1;
			static get v() { return Config._v; }
			static set v(x) { Config._v = x; }
		}
		Config.v = 8;
		assert.sameValue(Config.v, 8, "static accessor");

		// Accessor descriptor is non-enumerable and lives on the prototype.
		var desc = Object.getOwnPropertyDescriptor(Base.prototype, "kind");
		assert.sameValue(desc.enumerable, false, "accessor non-enumerable");
		assert.sameValue(typeof desc.get, "function", "has getter");
		assert.sameValue(typeof desc.set, "function", "has setter");
	`)
}

// ---------------------------------------------------------------------------
// 14. Field initialization ordering
// ---------------------------------------------------------------------------

func TestClassFieldInitOrder(t *testing.T) {
	Expect(t, `
		var log = [];
		class Base {
			bf = log.push("base-field") && "bf";
			constructor() { log.push("base-ctor"); }
		}
		class Derived extends Base {
			df = log.push("derived-field") && "df";
			constructor() {
				super();                 // base fields + base ctor run here
				log.push("derived-ctor");
			}
		}
		new Derived();
		// Base field init runs before base ctor body; derived fields run right
		// after super() returns, before the rest of the derived ctor body.
		assert.sameValue(log.join(","),
			"base-field,base-ctor,derived-field,derived-ctor",
			"field/constructor ordering across super()");

		// Fields initialize top-to-bottom; a later field can read an earlier one,
		// but an earlier field sees a not-yet-initialized later field as undefined.
		class Order {
			a = 1;
			b = this.a + 1;
			c = this.d;      // d not yet initialized
			d = 4;
		}
		var o = new Order();
		assert.sameValue(o.b, 2, "later field reads earlier");
		assert.sameValue(o.c, undefined, "earlier field sees undefined for later");
		assert.sameValue(o.d, 4, "later field value");
	`)
}

// ---------------------------------------------------------------------------
// 15. Computed method / field names
// ---------------------------------------------------------------------------

func TestClassComputedNames(t *testing.T) {
	Expect(t, `
		var m = "run";
		var f = "field";
		var s = "smethod";
		class C {
			[m]() { return "ran"; }
			[f] = 7;
			static [s]() { return "static"; }
			[1 + 1]() { return "two"; }
		}
		var c = new C();
		assert.sameValue(c.run(), "ran", "computed method name");
		assert.sameValue(c.field, 7, "computed field name");
		assert.sameValue(C.smethod(), "static", "computed static method name");
		assert.sameValue(c[2](), "two", "computed numeric method name");
	`)
}

// ---------------------------------------------------------------------------
// 16. Class expressions
// ---------------------------------------------------------------------------

func TestClassExpressionForms(t *testing.T) {
	Expect(t, `
		// Anonymous, name inferred from binding.
		var Anon = class { m() { return 1; } };
		assert.sameValue(Anon.name, "Anon", "inferred name");
		assert.sameValue(new Anon().m(), 1, "anonymous class works");

		// Named class expression: inner name is in scope inside the body only.
		var Ref = class Inner {
			self() { return Inner; }             // inner name visible internally
		};
		assert.sameValue(Ref.name, "Inner", "declared name wins");
		assert.sameValue(new Ref().self() === Ref, true, "inner name refers to the class");
		assert.throws(ReferenceError, function() { return Inner; },
			"inner name not visible outside");

		// Immediately-invoked and used as a value.
		var inst = new (class { constructor() { this.v = 9; } })();
		assert.sameValue(inst.v, 9, "IIFE-style class expression");
	`)
}

// ---------------------------------------------------------------------------
// 17. Class hoisting / TDZ
// ---------------------------------------------------------------------------

func TestClassHoistingTDZ(t *testing.T) {
	// A class declaration is not hoisted like a function: use before the
	// declaration is a ReferenceError (TDZ), like let/const.
	ExpectError(t, `new C(); class C {}`, "ReferenceError")
	ExpectError(t, `typeof C; let C = class {};`, "ReferenceError")

	Expect(t, `
		// After declaration, everything is fine.
		class C { v() { return 5; } }
		assert.sameValue(new C().v(), 5, "usable after declaration");

		// The class binding is mutable (class declarations bind like let, not const).
		var Original = C;
		C = 123;
		assert.sameValue(C, 123, "class binding is reassignable");
		assert.sameValue(new Original().v(), 5, "old reference still works");
	`)
}

// ---------------------------------------------------------------------------
// 18. Prototype mutation
// ---------------------------------------------------------------------------

func TestClassPrototypeMutation(t *testing.T) {
	Expect(t, `
		class C { constructor(v) { this.v = v; } }
		var a = new C(1);
		// Add a method after instances exist; existing instances see it.
		C.prototype.double = function() { return this.v * 2; };
		assert.sameValue(a.double(), 2, "method added post-construction is visible");
		var b = new C(3);
		assert.sameValue(b.double(), 6, "new instance sees it too");

		// Changing an inherited method changes behavior for all instances.
		class Base { greet() { return "hi"; } }
		class Sub extends Base {}
		var s = new Sub();
		assert.sameValue(s.greet(), "hi", "before mutation");
		Base.prototype.greet = function() { return "hello"; };
		assert.sameValue(s.greet(), "hello", "inherited behavior changes");
	`)
}

// ---------------------------------------------------------------------------
// 19. Object.getPrototypeOf relationships
// ---------------------------------------------------------------------------

func TestClassGetPrototypeOf(t *testing.T) {
	Expect(t, `
		class Base {}
		class Derived extends Base {}
		var d = new Derived();
		// Instance side.
		assert.sameValue(Object.getPrototypeOf(d) === Derived.prototype, true, "instance proto");
		assert.sameValue(Object.getPrototypeOf(Derived.prototype) === Base.prototype, true,
			"prototype proto");
		// Constructor side (static inheritance).
		assert.sameValue(Object.getPrototypeOf(Derived) === Base, true, "ctor proto is base");
		assert.sameValue(Object.getPrototypeOf(Base) === Function.prototype, true,
			"base ctor proto is Function.prototype");
	`)
}

// ---------------------------------------------------------------------------
// 20. constructor property through inheritance and mutation
// ---------------------------------------------------------------------------

func TestClassConstructorPropertyChain(t *testing.T) {
	Expect(t, `
		class A {}
		class B extends A {}
		var b = new B();
		assert.sameValue(b.constructor === B, true, "constructor is most-derived");
		assert.sameValue(b.hasOwnProperty("constructor"), false, "not own on instance");
		assert.sameValue(B.prototype.hasOwnProperty("constructor"), true, "own on prototype");
		assert.sameValue(b.constructor.prototype === B.prototype, true, "round trip");

		// After replacing the prototype's constructor link.
		function F() {}
		var inst = new F();
		assert.sameValue(inst.constructor === F, true, "before replace");
	`)
}

// ---------------------------------------------------------------------------
// 21. Method home object survives extraction / rebinding
// ---------------------------------------------------------------------------

func TestClassMethodHomeObject(t *testing.T) {
	Expect(t, `
		class Base { greet() { return "base"; } }
		class Derived extends Base {
			greet() { return super.greet() + "-derived"; }
		}
		var d = new Derived();
		// Extract the method; super must still resolve against Derived.prototype.
		var extracted = d.greet;
		assert.sameValue(extracted.call(d), "base-derived", "extracted method keeps super home");

		// Rebind onto another instance of the same class.
		var d2 = new Derived();
		assert.sameValue(d.greet.call(d2), "base-derived", "rebound method keeps home object");
	`)
}

// ---------------------------------------------------------------------------
// 22. Arrow functions and lexical this
// ---------------------------------------------------------------------------

func TestClassArrowLexicalThis(t *testing.T) {
	Expect(t, `
		class Base {
			constructor() { this.tag = "base"; }
			make() { return () => this.tag; }
		}
		class Derived extends Base {
			constructor() { super(); this.tag = "derived"; }
		}
		// Arrow captures the instance this, even through an inherited method.
		assert.sameValue(new Derived().make()(), "derived", "arrow via inherited method");

		// Arrow field captures the constructing instance's this.
		class Field {
			value = 10;
			getter = () => this.value;
		}
		var f = new Field();
		var g = f.getter;
		assert.sameValue(g(), 10, "arrow field is bound to the instance");
	`)
}

// ---------------------------------------------------------------------------
// 23. new.target
// ---------------------------------------------------------------------------

func TestClassNewTarget(t *testing.T) {
	Expect(t, `
		// Base class: new.target is the invoked constructor.
		var seen;
		class Base { constructor() { seen = new.target; } }
		new Base();
		assert.sameValue(seen === Base, true, "base new.target is Base");

		// Derived: new.target is the most-derived class through the super chain.
		class Derived extends Base {}
		new Derived();
		assert.sameValue(seen === Derived, true, "derived new.target propagates to base");

		// Ordinary function: undefined on a plain call, the function on new.
		function f() { return new.target; }
		assert.sameValue(f(), undefined, "plain call: undefined");
		var captured;
		function g() { captured = new.target; }
		new g();
		assert.sameValue(captured === g, true, "new: the function");

		// Inside a method (not a constructor), new.target is undefined.
		class M { test() { return new.target; } }
		assert.sameValue(new M().test(), undefined, "method new.target is undefined");
	`)
}

// ---------------------------------------------------------------------------
// 24. Built-in inheritance
// ---------------------------------------------------------------------------

func TestClassExtendsBuiltins(t *testing.T) {
	Expect(t, `
		// Error subclass.
		class MyError extends Error {
			constructor(msg) { super(msg); this.name = "MyError"; }
		}
		var e = new MyError("boom");
		assert.sameValue(e instanceof MyError, true, "instanceof MyError");
		assert.sameValue(e instanceof Error, true, "instanceof Error");
		assert.sameValue(e.message, "boom", "message via super");
		assert.sameValue(e.name, "MyError", "name override");

		// Array subclass: real array behavior on the instance.
		class Stack extends Array {
			top() { return this[this.length - 1]; }
		}
		var s = new Stack();
		s.push(1); s.push(2); s.push(3);
		assert.sameValue(s.length, 3, "length tracks pushes");
		assert.sameValue(s.top(), 3, "index access works");
		assert.sameValue(Array.isArray(s), true, "Array.isArray");
		assert.sameValue(s instanceof Stack, true, "instanceof Stack");
		assert.sameValue(s instanceof Array, true, "instanceof Array");

		// Map subclass: backing storage carried onto the instance.
		class Counter extends Map {
			bump(k) { this.set(k, (this.get(k) || 0) + 1); return this; }
		}
		var m = new Counter();
		m.bump("a").bump("a").bump("b");
		assert.sameValue(m.get("a"), 2, "map get");
		assert.sameValue(m.size, 2, "map size");
		assert.sameValue(m instanceof Map, true, "instanceof Map");

		// Set subclass.
		class Tags extends Set {}
		var tset = new Tags();
		tset.add("x"); tset.add("x"); tset.add("y");
		assert.sameValue(tset.size, 2, "set dedupes");
		assert.sameValue(tset.has("x"), true, "set has");
	`)
}

// ---------------------------------------------------------------------------
// 25. Error conditions
// ---------------------------------------------------------------------------

func TestClassErrorConditions(t *testing.T) {
	// Calling a class without new.
	ExpectError(t, `class C {} C();`, "TypeError")
	// Extending a non-constructor.
	ExpectError(t, `class C extends 5 {}`, "TypeError")
	ExpectError(t, `var x = {}; class C extends x {}`, "TypeError")
	// Duplicate constructors.
	ExpectError(t, `class C { constructor() {} constructor() {} }`, "SyntaxError")
	// new on a non-constructor.
	ExpectError(t, `var f = () => {}; new f();`, "TypeError")

	Expect(t, `
		class Vehicle { constructor(b) { this.b = b; } }
		assert.throws(TypeError, function() { Vehicle("x"); },
			"class invoked without new throws TypeError");
		// null is a permitted (special) extends value.
		var Nullish = class extends null {};
		assert.sameValue(typeof Nullish, "function", "extends null is allowed");
	`)
}

// ---------------------------------------------------------------------------
// 26. Class fields (instance, static, computed, inherited)
// ---------------------------------------------------------------------------

func TestClassFieldsVariety(t *testing.T) {
	Expect(t, `
		class Base {
			baseField = "b";
			static baseStatic = "bs";
		}
		class Derived extends Base {
			derivedField = "d";
			static derivedStatic = "ds";
		}
		var d = new Derived();
		assert.sameValue(d.baseField, "b", "inherited instance field initialized");
		assert.sameValue(d.derivedField, "d", "own instance field");
		assert.sameValue(d.hasOwnProperty("baseField"), true, "base field is own on instance");
		assert.sameValue(Derived.derivedStatic, "ds", "own static field");
		assert.sameValue(Derived.baseStatic, "bs", "static field via chain");

		// Uninitialized field is an own property with value undefined.
		class Bare { x; }
		var bare = new Bare();
		assert.sameValue(bare.hasOwnProperty("x"), true, "declared field is own");
		assert.sameValue(bare.x, undefined, "uninitialized is undefined");
	`)
}

// ---------------------------------------------------------------------------
// 27. Circular inheritance is rejected
// ---------------------------------------------------------------------------

func TestClassCircularInheritance(t *testing.T) {
	// class A extends A {} reads A while it is still in the TDZ.
	ExpectError(t, `class A extends A {}`, "ReferenceError")
}

// ---------------------------------------------------------------------------
// 28. Shadowing interactions
// ---------------------------------------------------------------------------

func TestClassShadowing(t *testing.T) {
	Expect(t, `
		// A derived instance field shadows an inherited method of the same name.
		class Base { x() { return "method"; } }
		class Derived extends Base { x = 5; }
		var d = new Derived();
		assert.sameValue(typeof d.x, "number", "field shadows inherited method");
		assert.sameValue(d.x, 5, "field value");
		// The base method is still reachable on the prototype.
		assert.sameValue(Base.prototype.x.call(d), "method", "base method reachable");

		// Derived accessor shadows a base data field via the prototype.
		class B2 { get v() { return "getter"; } }
		class D2 extends B2 { get v() { return "override"; } }
		assert.sameValue(new D2().v, "override", "accessor override");

		// Static shadowing.
		class SB { static tag() { return "SB"; } }
		class SD extends SB { static tag() { return "SD"; } }
		assert.sameValue(SD.tag(), "SD", "static override");
		assert.sameValue(SB.tag(), "SB", "base static unchanged");
	`)
}

// ---------------------------------------------------------------------------
// 29. Prototype identity: shared methods, per-instance fields
// ---------------------------------------------------------------------------

func TestClassPrototypeIdentity(t *testing.T) {
	Expect(t, `
		class C {
			constructor() { this.data = []; }   // per-instance
			method() { return 1; }              // shared
		}
		var a = new C();
		var b = new C();
		// All instances share one prototype and one method object.
		assert.sameValue(Object.getPrototypeOf(a) === Object.getPrototypeOf(b), true,
			"single shared prototype");
		assert.sameValue(a.method === b.method, true, "method object is shared");
		// Fields are per-instance.
		assert.notSameValue(a.data, b.data, "distinct field objects");
		a.data.push(1);
		assert.sameValue(b.data.length, 0, "field mutation isolated");
	`)
}

// ---------------------------------------------------------------------------
// 30. Cross-feature integration
// ---------------------------------------------------------------------------

func TestClassIntegration(t *testing.T) {
	Expect(t, `
		class Base {
			#id;
			constructor(id) { this.#id = id; }
			get id() { return this.#id; }
		}
		class Node extends Base {
			static #counter = 0;
			static nextId() { return ++Node.#counter; }
			// destructuring + defaults + rest in the constructor.
			constructor({ label = "node", ...meta } = {}) {
				super(Node.nextId());
				this.label = label;
				this.meta = meta;
				this.children = [];
			}
			add(child) { this.children.push(child); return this; }
			// optional chaining + closures over this.
			firstChildLabel() { return this.children?.[0]?.label; }
			labels() { return this.children.map((c) => c.label); }
		}
		var root = new Node({ label: "root", kind: "container" });
		var a = new Node({ label: "a" });
		var b = new Node();                       // uses default label
		root.add(a).add(b);

		assert.sameValue(root.label, "root", "destructured label");
		assert.sameValue(root.meta.kind, "container", "rest captured extra props");
		assert.sameValue(root.id, 1, "private id via super + static counter");
		assert.sameValue(a.id, 2, "second id");
		assert.sameValue(b.id, 3, "third id");
		assert.sameValue(b.label, "node", "default label applied");
		assert.sameValue(root.firstChildLabel(), "a", "optional chaining into children");
		assert.sameValue(root.labels().join(","), "a,node", "closure + map over children");
		assert.sameValue(root.children.length, 2, "chained add()");
	`)

	// Async method integration through the microtask queue.
	Expect(t, asyncCase(`
		class Repo {
			#items = new Map();
			async put(k, v) { this.#items.set(k, v); return await Promise.resolve(k); }
			async get(k) { return this.#items.get(k); }
		}
		class Cached extends Repo {
			async get(k) {
				var v = await super.get(k);
				return v === undefined ? "miss" : v;
			}
		}
		var r = new Cached();
		var key = await r.put("a", 1);
		assert.sameValue(key, "a", "async put returns key");
		assert.sameValue(await r.get("a"), 1, "async get hit via super");
		assert.sameValue(await r.get("z"), "miss", "async get miss via override");
	`))
}
