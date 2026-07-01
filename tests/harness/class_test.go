package harness

import "testing"

// class_test.go exercises JavaScript class semantics end-to-end through the
// test harness. Each function covers one aspect of the class specification.
// Assertions use the test262-style helpers injected by harness.go.
//
// Some tests are expected to expose engine limitations:
//   - TestClassPrivateAccessOutsideClass: engine does not enforce private-access
//     SyntaxError; c.#x outside the class silently reads property "x" instead.
//   - TestClassPrivateBrandCheck: #field in obj evaluates the PrivateIdent as a
//     primary expression, hitting the default "unsupported expression" path.
//   - TestClassExtendsArray: derived instances are plain objects, not real arrays;
//     length, indexed access, and Array.isArray fail.

// ---------------------------------------------------------------------------
// Basic construction and method dispatch
// ---------------------------------------------------------------------------

func TestClassBasic(t *testing.T) {
	Expect(t, `
		class Point {
			constructor(x, y) {
				this.x = x;
				this.y = y;
			}
			toString() { return "(" + this.x + "," + this.y + ")"; }
			distanceTo(other) {
				var dx = this.x - other.x;
				var dy = this.y - other.y;
				return Math.sqrt(dx * dx + dy * dy);
			}
		}

		var p = new Point(3, 4);
		assert.sameValue(p.x, 3, "x field");
		assert.sameValue(p.y, 4, "y field");
		assert.sameValue(p.toString(), "(3,4)", "custom toString");

		var origin = new Point(0, 0);
		assert.sameValue(p.distanceTo(origin), 5, "Pythagorean distance");

		assert.sameValue(p instanceof Point, true, "instanceof");

		// Two instances are independent
		var p2 = new Point(1, 2);
		assert.sameValue(p2.x, 1, "p2.x");
		assert.sameValue(p.x, 3, "p.x unchanged");

		// Fields are own; methods are on the prototype
		assert.sameValue(p.hasOwnProperty("x"), true, "x is own");
		assert.sameValue(p.hasOwnProperty("y"), true, "y is own");
		assert.sameValue(p.hasOwnProperty("toString"), false, "toString not own");
		assert.sameValue(typeof p.toString, "function", "toString is callable");

		// constructor is accessible via the prototype chain
		assert.sameValue(p.constructor === Point, true, "constructor link");
		assert.sameValue(Point.name, "Point", "class name");
	`)
}

// ---------------------------------------------------------------------------
// Calling a class without new must throw TypeError
// ---------------------------------------------------------------------------

func TestClassCallWithoutNew(t *testing.T) {
	Expect(t, `
		class Vehicle {
			constructor(brand) { this.brand = brand; }
			describe() { return "I am a " + this.brand; }
		}

		assert.throws(TypeError, function() { Vehicle(); },
			"calling class without new throws TypeError");
		assert.throws(TypeError, function() { Vehicle("Toyota"); },
			"calling class without new with args throws TypeError");

		// Verify it works fine when new is used
		var v = new Vehicle("Honda");
		assert.sameValue(v.brand, "Honda", "new works");
		assert.sameValue(v.describe(), "I am a Honda", "method works");
	`)
}

// ---------------------------------------------------------------------------
// Public instance fields
// ---------------------------------------------------------------------------

func TestClassInstanceFields(t *testing.T) {
	Expect(t, `
		class Counter {
			count = 0;
			step = 1;
			doubled = this.step * 2;
		}
		var c1 = new Counter();
		assert.sameValue(c1.count, 0, "count initial value");
		assert.sameValue(c1.step, 1, "step initial value");
		assert.sameValue(c1.doubled, 2, "doubled = step * 2");

		// Fields are own properties
		assert.sameValue(c1.hasOwnProperty("count"), true, "count is own");
		assert.sameValue(c1.hasOwnProperty("step"), true, "step is own");
		assert.sameValue(c1.hasOwnProperty("doubled"), true, "doubled is own");

		// Each instance gets its own copy
		var c2 = new Counter();
		c1.count = 99;
		assert.sameValue(c2.count, 0, "c2.count unaffected by mutation of c1");

		// Field referencing an earlier field via this
		class Box {
			width = 10;
			height = 20;
			area = this.width * this.height;
		}
		var b = new Box();
		assert.sameValue(b.area, 200, "area = width * height");
		assert.sameValue(b.hasOwnProperty("area"), true, "area is own");

		// Field declared without initializer is undefined own property
		class Bare { value; }
		var bare = new Bare();
		assert.sameValue(bare.hasOwnProperty("value"), true, "value is own");
		assert.sameValue(bare.value, undefined, "uninitialized field is undefined");

		// Constructor and field initializers both run
		class Tagged {
			tag = "default";
			constructor(t) { this.tag = t; }
		}
		var tg = new Tagged("custom");
		assert.sameValue(tg.tag, "custom", "constructor overrides field default");
	`)
}

// ---------------------------------------------------------------------------
// Private fields and private methods
// ---------------------------------------------------------------------------

func TestClassPrivateFields(t *testing.T) {
	Expect(t, `
		class BankAccount {
			#balance = 0;
			constructor(initial) { this.#balance = initial; }
			deposit(amount) { this.#balance += amount; return this; }
			withdraw(amount) {
				if (amount > this.#balance) throw new RangeError("Insufficient funds");
				this.#balance -= amount;
				return this;
			}
			get balance() { return this.#balance; }
		}

		var acc = new BankAccount(100);
		assert.sameValue(acc.balance, 100, "initial balance via getter");
		acc.deposit(50);
		assert.sameValue(acc.balance, 150, "after deposit");
		acc.withdraw(30);
		assert.sameValue(acc.balance, 120, "after withdraw");
		assert.throws(RangeError, function() { acc.withdraw(200); },
			"overdraft throws RangeError");

		// The public getter is not an own enumerable property of the instance
		assert.sameValue(Object.keys(acc).indexOf("balance"), -1,
			"getter not in Object.keys");

		// Private method accessed from within the class
		class Formatter {
			#prefix = "[INFO]";
			#format(msg) { return this.#prefix + " " + msg; }
			log(msg) { return this.#format(msg); }
		}
		var fmt = new Formatter();
		assert.sameValue(fmt.log("hello"), "[INFO] hello", "private method via public wrapper");

		// Private field in derived class
		class Named {
			#name;
			constructor(n) { this.#name = n; }
			getName() { return this.#name; }
		}
		class NamedWithTitle extends Named {
			constructor(name, title) {
				super(name);
				this.title = title;
			}
			getTitle() { return this.title + " " + this.getName(); }
		}
		var person = new NamedWithTitle("Alice", "Dr.");
		assert.sameValue(person.getName(), "Alice", "private field via inherited method");
		assert.sameValue(person.getTitle(), "Dr. Alice", "derived class uses parent method");
	`)
}

// ---------------------------------------------------------------------------
// Accessing a private field from outside the class should throw SyntaxError.
// NOTE: the gojs engine does not enforce this restriction — it treats #x as a
// regular property named "x" and returns 1 without throwing. This test is
// expected to FAIL on the current engine, revealing the bug.
// ---------------------------------------------------------------------------

func TestClassPrivateAccessOutsideClass(t *testing.T) {
	ExpectError(t, `class C { #x = 1; } var c = new C(); c.#x;`, "SyntaxError")
}

// ---------------------------------------------------------------------------
// Private brand check: #field in obj
// NOTE: the gojs engine evaluates a bare PrivateIdent as a primary expression,
// which hits the "unsupported expression" fallthrough and throws a SyntaxError
// for the wrong reason. This test is expected to FAIL on the current engine.
// ---------------------------------------------------------------------------

func TestClassPrivateBrandCheck(t *testing.T) {
	Expect(t, `
		class Tagged {
			#tag = true;
			static isTagged(obj) { return #tag in obj; }
		}
		var inst = new Tagged();
		assert.sameValue(Tagged.isTagged(inst), true, "instance has the brand");
		assert.sameValue(Tagged.isTagged({}), false, "plain object lacks the brand");
	`)
}

// ---------------------------------------------------------------------------
// Public getters and setters
// ---------------------------------------------------------------------------

func TestClassGetterSetter(t *testing.T) {
	Expect(t, `
		class Circle {
			constructor(r) { this._r = r; }
			get radius() { return this._r; }
			set radius(v) {
				if (v < 0) throw new RangeError("radius must be non-negative");
				this._r = v;
			}
			get area() { return Math.PI * this._r * this._r; }
			get diameter() { return this._r * 2; }
		}

		var c = new Circle(5);
		assert.sameValue(c.radius, 5, "getter reads backing field");
		assert.sameValue(c.diameter, 10, "derived getter");
		c.radius = 10;
		assert.sameValue(c.radius, 10, "setter updates value");
		assert.throws(RangeError, function() { c.radius = -1; },
			"setter validates input");

		// Getter without a setter: write is silently ignored (non-strict)
		var areaBefore = c.area;
		c.area = 9999;
		assert.sameValue(c.area, areaBefore, "getter-only property ignores write");

		// Getters live on the prototype, not on the instance
		assert.sameValue(c.hasOwnProperty("radius"), false, "getter is not own");
		assert.sameValue(c.hasOwnProperty("_r"), true, "backing field is own");

		// Property descriptor confirms non-enumerable accessor
		var desc = Object.getOwnPropertyDescriptor(Circle.prototype, "radius");
		assert.sameValue(desc.enumerable, false, "accessor is non-enumerable");
		assert.sameValue(typeof desc.get, "function", "descriptor has get");
		assert.sameValue(typeof desc.set, "function", "descriptor has set");

		// area has getter but no setter
		var areaDesc = Object.getOwnPropertyDescriptor(Circle.prototype, "area");
		assert.sameValue(typeof areaDesc.get, "function", "area has getter");
		assert.sameValue(areaDesc.set, undefined, "area has no setter");
	`)
}

// ---------------------------------------------------------------------------
// Class methods are non-enumerable; instance fields are enumerable
// ---------------------------------------------------------------------------

func TestClassMethodEnumerability(t *testing.T) {
	Expect(t, `
		class Dog {
			constructor(name, breed) {
				this.name = name;
				this.breed = breed;
			}
			bark() { return "woof"; }
			sit() { return "sits"; }
			toString() { return this.name + " the " + this.breed; }
		}

		var d = new Dog("Rex", "Lab");

		// Object.keys returns only own enumerable keys
		var keys = Object.keys(d);
		assert.sameValue(keys.indexOf("name") >= 0, true, "name is enumerable");
		assert.sameValue(keys.indexOf("breed") >= 0, true, "breed is enumerable");
		assert.sameValue(keys.indexOf("bark"), -1, "bark is not own enumerable");
		assert.sameValue(keys.indexOf("sit"), -1, "sit is not own enumerable");
		assert.sameValue(keys.indexOf("toString"), -1, "toString is not own enumerable");
		assert.sameValue(keys.length, 2, "exactly two enumerable own keys");

		// Methods are accessible but live on the prototype
		assert.sameValue(typeof d.bark, "function", "bark is callable");
		assert.sameValue(d.bark(), "woof", "bark returns expected string");
		assert.sameValue(d.hasOwnProperty("bark"), false, "bark is not own");
		assert.sameValue(Dog.prototype.hasOwnProperty("bark"), true, "bark is on prototype");

		// The prototype's own enumerable keys are empty (class methods are non-enumerable)
		var protoKeys = Object.keys(Dog.prototype);
		assert.sameValue(protoKeys.length, 0, "prototype has no own enumerable keys");
	`)
}

// ---------------------------------------------------------------------------
// Static methods and static fields
// ---------------------------------------------------------------------------

func TestClassStaticMembersAndFields(t *testing.T) {
	Expect(t, `
		class Config {
			static version = "2.0";
			static author = "test-suite";
			static defaultTimeout = 5000;
		}
		assert.sameValue(Config.version, "2.0", "static field version");
		assert.sameValue(Config.author, "test-suite", "static field author");
		assert.sameValue(Config.defaultTimeout, 5000, "static field defaultTimeout");
		// Static fields are not on the prototype
		assert.sameValue(Config.prototype.version, undefined, "static field not on prototype");

		// Static factory method
		class Point {
			constructor(x, y) { this.x = x; this.y = y; }
			static origin() { return new Point(0, 0); }
			static fromArray(arr) { return new Point(arr[0], arr[1]); }
			distSq() { return this.x * this.x + this.y * this.y; }
		}
		var o = Point.origin();
		assert.sameValue(o.x, 0, "origin x");
		assert.sameValue(o.y, 0, "origin y");
		var p = Point.fromArray([3, 4]);
		assert.sameValue(p.distSq(), 25, "fromArray + distSq");

		// Static methods are not on instances
		assert.sameValue(typeof Point.origin, "function", "static method on class");
		assert.sameValue(typeof o.origin, "undefined", "static method not on instance");

		// Static counter pattern
		class IDGen {
			static #next = 1;
			static create() { return new IDGen(); }
			static reset() { IDGen.#next = 1; }
			constructor() { this.id = IDGen.#next++; }
		}
		var a = IDGen.create();
		var b = IDGen.create();
		var cc = new IDGen();
		assert.sameValue(a.id, 1, "first id");
		assert.sameValue(b.id, 2, "second id");
		assert.sameValue(cc.id, 3, "third id");
		IDGen.reset();
		var d = IDGen.create();
		assert.sameValue(d.id, 1, "id after reset");
	`)
}

// ---------------------------------------------------------------------------
// Inheritance: extends, super(), super.method(), instanceof, prototype chain
// ---------------------------------------------------------------------------

func TestClassInheritance(t *testing.T) {
	Expect(t, `
		class Animal {
			constructor(name) { this.name = name; }
			speak() { return this.name + " makes noise"; }
			describe() { return "I am " + this.name; }
		}

		class Dog extends Animal {
			constructor(name, breed) {
				super(name);
				this.breed = breed;
			}
			speak() { return this.name + " barks"; }
			info() { return super.describe() + ", breed: " + this.breed; }
		}

		class GoldenRetriever extends Dog {
			constructor(name) { super(name, "Golden Retriever"); }
			speak() { return super.speak() + " (happily)"; }
		}

		var a = new Animal("Cat");
		var d = new Dog("Rex", "Lab");
		var g = new GoldenRetriever("Buddy");

		// Method overriding and dispatch
		assert.sameValue(a.speak(), "Cat makes noise", "Animal.speak");
		assert.sameValue(d.speak(), "Rex barks", "Dog.speak overrides Animal.speak");
		assert.sameValue(g.speak(), "Buddy barks (happily)", "GoldenRetriever chains super.speak");

		// super.method() accesses the parent's implementation
		assert.sameValue(d.info(), "I am Rex, breed: Lab", "super.describe() in Dog");

		// instanceof checks across the chain
		assert.sameValue(d instanceof Dog, true, "d instanceof Dog");
		assert.sameValue(d instanceof Animal, true, "d instanceof Animal");
		assert.sameValue(g instanceof GoldenRetriever, true, "g instanceof GoldenRetriever");
		assert.sameValue(g instanceof Dog, true, "g instanceof Dog");
		assert.sameValue(g instanceof Animal, true, "g instanceof Animal");
		assert.sameValue(a instanceof Dog, false, "a not instanceof Dog");

		// constructor.name
		assert.sameValue(d.constructor.name, "Dog", "d.constructor.name");
		assert.sameValue(g.constructor.name, "GoldenRetriever", "g.constructor.name");

		// Prototype chain: Derived.prototype.__proto__ === Base.prototype
		assert.sameValue(
			Object.getPrototypeOf(Dog.prototype) === Animal.prototype,
			true, "Dog.prototype.__proto__ === Animal.prototype");
		assert.sameValue(
			Object.getPrototypeOf(GoldenRetriever.prototype) === Dog.prototype,
			true, "GoldenRetriever.prototype.__proto__ === Dog.prototype");

		// Own properties set by each constructor
		assert.sameValue(d.name, "Rex", "d.name from super()");
		assert.sameValue(d.breed, "Lab", "d.breed from Dog constructor");
		assert.sameValue(g.breed, "Golden Retriever", "g.breed from GoldenRetriever constructor");
	`)
}

// ---------------------------------------------------------------------------
// Default derived constructor forwards all arguments to super
// ---------------------------------------------------------------------------

func TestClassDefaultDerivedConstructor(t *testing.T) {
	Expect(t, `
		class Base {
			constructor(x, y) {
				this.x = x;
				this.y = y;
			}
			sum() { return this.x + this.y; }
		}

		// Derived class with no explicit constructor
		class Derived extends Base {}

		var d = new Derived(3, 7);
		assert.sameValue(d.x, 3, "x forwarded");
		assert.sameValue(d.y, 7, "y forwarded");
		assert.sameValue(d.sum(), 10, "inherited method works");
		assert.sameValue(d instanceof Derived, true, "instanceof Derived");
		assert.sameValue(d instanceof Base, true, "instanceof Base");
		assert.sameValue(d.constructor, Derived, "constructor is Derived");
		assert.sameValue(d.constructor.name, "Derived", "constructor.name");

		// Three levels deep, all with default constructors
		class Level3 extends Derived {}
		var l = new Level3(5, 6);
		assert.sameValue(l.x, 5, "x through three levels");
		assert.sameValue(l.sum(), 11, "inherited sum");
		assert.sameValue(l instanceof Level3, true, "instanceof Level3");
		assert.sameValue(l instanceof Derived, true, "instanceof Derived");
		assert.sameValue(l instanceof Base, true, "instanceof Base");
	`)
}

// ---------------------------------------------------------------------------
// Computed method and field names
// ---------------------------------------------------------------------------

func TestClassComputedMethodNames(t *testing.T) {
	Expect(t, `
		var prefix = "get";
		class Accessor {
			constructor(v) { this._v = v; }
			[prefix + "Value"]() { return this._v; }
			["m" + "1"]() { return 1; }
			["m" + "2"]() { return 2; }
		}

		var a = new Accessor(42);
		assert.sameValue(a.getValue(), 42, "computed name 'getValue'");
		assert.sameValue(a.m1(), 1, "computed name 'm1'");
		assert.sameValue(a.m2(), 2, "computed name 'm2'");

		// Computed name from a number expression
		class NumKey {
			[1 + 1]() { return "two"; }
		}
		var nk = new NumKey();
		assert.sameValue(nk[2](), "two", "computed numeric key");

		// Computed name in class expression
		var method = "hello";
		var C = class {
			[method]() { return "world"; }
		};
		var cc = new C();
		assert.sameValue(cc.hello(), "world", "computed name in class expression");
	`)
}

// ---------------------------------------------------------------------------
// Class expressions (anonymous and named)
// ---------------------------------------------------------------------------

func TestClassExpressions(t *testing.T) {
	Expect(t, `
		// Anonymous class expression — name is inferred from the variable binding
		var MyPoint = class {
			constructor(x, y) { this.x = x; this.y = y; }
			toString() { return "(" + this.x + "," + this.y + ")"; }
		};
		var p = new MyPoint(1, 2);
		assert.sameValue(p.toString(), "(1,2)", "anonymous class expression works");
		assert.sameValue(p instanceof MyPoint, true, "instanceof with class expression");
		assert.sameValue(MyPoint.name, "MyPoint", "name inferred from variable");

		// Named class expression — the declared name is the canonical name
		var Outer = class Inner {
			test() { return "inner"; }
		};
		var o = new Outer();
		assert.sameValue(o.test(), "inner", "named class expression method");
		assert.sameValue(Outer.name, "Inner", "class expression name is the declared name");

		// The internal name (Inner) is not accessible in the outer scope
		assert.throws(ReferenceError, function() { return Inner; },
			"inner class name not in outer scope");

		// typeof a class is "function"
		assert.sameValue(typeof MyPoint, "function", "typeof class is function");
		assert.sameValue(typeof Outer, "function", "typeof named class expr is function");

		// Class expression inheriting
		var Animal = class { constructor(n) { this.name = n; } };
		var Dog = class extends Animal {
			speak() { return this.name + " barks"; }
		};
		var d = new Dog("Rex");
		assert.sameValue(d.speak(), "Rex barks", "class expression with extends");
		assert.sameValue(d instanceof Dog, true, "instanceof with class expr extends");
		assert.sameValue(d instanceof Animal, true, "instanceof base with class expr");
	`)
}

// ---------------------------------------------------------------------------
// Method chaining (methods return this)
// ---------------------------------------------------------------------------

func TestClassMethodChaining(t *testing.T) {
	Expect(t, `
		class Builder {
			constructor() { this.parts = []; }
			add(part) { this.parts.push(part); return this; }
			addAll(arr) { for (var i = 0; i < arr.length; i++) this.parts.push(arr[i]); return this; }
			build() { return this.parts.join("-"); }
		}

		var result = new Builder().add("a").add("b").add("c").build();
		assert.sameValue(result, "a-b-c", "basic chaining");

		var result2 = new Builder().addAll(["x", "y"]).add("z").build();
		assert.sameValue(result2, "x-y-z", "addAll then add");

		// Chaining on a derived class
		class ExtBuilder extends Builder {
			addPrefix(p) { this.parts.unshift(p); return this; }
		}
		var result3 = new ExtBuilder().add("b").add("c").addPrefix("a").build();
		assert.sameValue(result3, "a-b-c", "chaining on derived class");

		// Stack with chaining
		class Stack {
			constructor() { this.data = []; }
			push(v) { this.data.push(v); return this; }
			pop() { this.data.pop(); return this; }
			peek() { return this.data[this.data.length - 1]; }
			size() { return this.data.length; }
		}
		var s = new Stack().push(1).push(2).push(3).pop();
		assert.sameValue(s.size(), 2, "size after push x3 then pop");
		assert.sameValue(s.peek(), 2, "peek after pop");
	`)
}

// ---------------------------------------------------------------------------
// constructor property on instances and prototypes
// ---------------------------------------------------------------------------

func TestClassConstructorProperty(t *testing.T) {
	Expect(t, `
		class Foo {}
		class Bar extends Foo {}

		var foo = new Foo();
		var bar = new Bar();

		// instance.constructor points to the class
		assert.sameValue(foo.constructor === Foo, true, "foo.constructor === Foo");
		assert.sameValue(bar.constructor === Bar, true, "bar.constructor === Bar");

		// constructor is on the prototype, not the instance itself
		assert.sameValue(foo.hasOwnProperty("constructor"), false,
			"constructor is not own on instance");
		assert.sameValue(Foo.prototype.hasOwnProperty("constructor"), true,
			"constructor is own on prototype");

		// constructor.prototype circles back to the prototype
		assert.sameValue(foo.constructor.prototype === Foo.prototype, true,
			"constructor.prototype === Foo.prototype");
		assert.sameValue(bar.constructor.prototype === Bar.prototype, true,
			"bar constructor.prototype === Bar.prototype");

		// class name property
		assert.sameValue(Foo.name, "Foo", "Foo.name");
		assert.sameValue(Bar.name, "Bar", "Bar.name");

		// instanceof consistency with constructor
		assert.sameValue(bar instanceof Bar, true, "instanceof Bar");
		assert.sameValue(bar.constructor === Bar, true, "constructor is Bar");
		assert.sameValue(bar.constructor === Foo, false, "constructor is not Foo");
	`)
}

// ---------------------------------------------------------------------------
// Custom toString and valueOf; String() coercion uses toString
// ---------------------------------------------------------------------------

func TestClassToString(t *testing.T) {
	Expect(t, `
		class Vector {
			constructor(x, y) { this.x = x; this.y = y; }
			toString() { return "Vector(" + this.x + ", " + this.y + ")"; }
			valueOf() { return Math.sqrt(this.x * this.x + this.y * this.y); }
		}
		var v = new Vector(3, 4);
		assert.sameValue(v.toString(), "Vector(3, 4)", "toString result");
		assert.sameValue(String(v), "Vector(3, 4)", "String() uses toString");
		// Arithmetic coercion prefers valueOf
		assert.sameValue(v + 0, 5, "arithmetic coercion uses valueOf");
		assert.sameValue(v > 4, true, "comparison uses valueOf");

		// Class with toString only (no valueOf)
		class Color {
			constructor(r, g, b) { this.r = r; this.g = g; this.b = b; }
			toString() { return "rgb(" + this.r + "," + this.g + "," + this.b + ")"; }
		}
		var c = new Color(255, 0, 128);
		assert.sameValue(c.toString(), "rgb(255,0,128)", "Color.toString");
		assert.sameValue(String(c), "rgb(255,0,128)", "String(color)");
		assert.sameValue("" + c, "rgb(255,0,128)", "string concat uses toString");

		// Inherited toString from a parent class
		class NamedColor extends Color {
			constructor(name, r, g, b) {
				super(r, g, b);
				this.colorName = name;
			}
			toString() { return this.colorName + ":" + super.toString(); }
		}
		var nc = new NamedColor("red", 255, 0, 0);
		assert.sameValue(nc.toString(), "red:rgb(255,0,0)", "derived toString via super");
		assert.sameValue(String(nc), "red:rgb(255,0,0)", "String(derived)");
	`)
}

// ---------------------------------------------------------------------------
// Extending Error: instanceof Error, .message, .name override
// ---------------------------------------------------------------------------

func TestClassExtendsError(t *testing.T) {
	Expect(t, `
		class AppError extends Error {
			constructor(message, code) {
				super(message);
				this.name = "AppError";
				this.code = code;
			}
		}

		class NetworkError extends AppError {
			constructor(url, code) {
				super("Network error fetching " + url, code);
				this.name = "NetworkError";
				this.url = url;
			}
		}

		var ae = new AppError("something failed", 500);
		assert.sameValue(ae instanceof AppError, true, "ae instanceof AppError");
		assert.sameValue(ae instanceof Error, true, "ae instanceof Error");
		assert.sameValue(ae.message, "something failed", "ae.message");
		assert.sameValue(ae.name, "AppError", "ae.name override");
		assert.sameValue(ae.code, 500, "ae.code");

		var ne = new NetworkError("example.com", 503);
		assert.sameValue(ne instanceof NetworkError, true, "ne instanceof NetworkError");
		assert.sameValue(ne instanceof AppError, true, "ne instanceof AppError");
		assert.sameValue(ne instanceof Error, true, "ne instanceof Error");
		assert.sameValue(ne.message, "Network error fetching example.com", "ne.message");
		assert.sameValue(ne.name, "NetworkError", "ne.name override");
		assert.sameValue(ne.code, 503, "ne.code");
		assert.sameValue(ne.url, "example.com", "ne.url");

		// Can be caught and re-thrown as plain Error
		var caught;
		try { throw ne; } catch (e) { caught = e; }
		assert.sameValue(caught instanceof Error, true, "caught instanceof Error");
		assert.sameValue(caught.message, "Network error fetching example.com", "caught.message");

		// assert.throws recognizes custom error class
		assert.throws(AppError, function() { throw new AppError("x", 0); },
			"assert.throws catches AppError");
	`)
}

// ---------------------------------------------------------------------------
// Extending Array (may fail — engine uses plain objects for derived instances)
// NOTE: length, indexed access, Array.isArray, and top() are expected to fail
// on the current engine because derived instances are not real array objects.
// ---------------------------------------------------------------------------

func TestClassExtendsArray(t *testing.T) {
	Expect(t, `
		class Stack extends Array {
			top() { return this[this.length - 1]; }
			isEmpty() { return this.length === 0; }
		}
		var s = new Stack();
		assert.sameValue(s instanceof Stack, true, "instanceof Stack");
		assert.sameValue(s instanceof Array, true, "instanceof Array");
		s.push(1);
		s.push(2);
		s.push(3);
		assert.sameValue(s.length, 3, "length after three pushes");
		assert.sameValue(s[0], 1, "s[0] === 1");
		assert.sameValue(s[2], 3, "s[2] === 3");
		assert.sameValue(s.top(), 3, "top() returns last element");
		assert.sameValue(Array.isArray(s), true, "Array.isArray(s)");
		assert.sameValue(s.pop(), 3, "pop returns 3");
		assert.sameValue(s.length, 2, "length after pop");
	`)
}

// ---------------------------------------------------------------------------
// Symbol.iterator as a computed class method name enables for-of and spread
// ---------------------------------------------------------------------------

func TestClassSymbolIterator(t *testing.T) {
	Expect(t, `
		class Range {
			constructor(from, to) {
				this.from = from;
				this.to = to;
			}
			[Symbol.iterator]() {
				var n = this.from;
				var last = this.to;
				return {
					next: function() {
						if (n <= last) {
							return { value: n++, done: false };
						}
						return { value: undefined, done: true };
					}
				};
			}
		}

		// for-of
		var r = new Range(1, 5);
		var out = [];
		for (var x of r) { out.push(x); }
		assert.sameValue(out.join(","), "1,2,3,4,5", "for-of over Range(1,5)");

		// spread
		var arr = [].concat.apply([], [].slice.call({ length: 0 }));
		var spread = [...new Range(3, 6)];
		assert.sameValue(spread.length, 4, "spread length");
		assert.sameValue(spread[0], 3, "spread[0]");
		assert.sameValue(spread[3], 6, "spread[3]");

		// Can iterate the same instance multiple times
		var sum = 0;
		for (var y of r) { sum += y; }
		assert.sameValue(sum, 15, "1+2+3+4+5 = 15");

		// Symbol.iterator is a method on the prototype, not an own property of instances
		assert.sameValue(typeof r[Symbol.iterator], "function",
			"Symbol.iterator method is callable");

		// Derived class inherits the iterator
		class SteppedRange extends Range {
			constructor(from, to, step) {
				super(from, to);
				this.step = step;
			}
			[Symbol.iterator]() {
				var n = this.from;
				var last = this.to;
				var step = this.step;
				return {
					next: function() {
						if (n <= last) {
							var v = n;
							n += step;
							return { value: v, done: false };
						}
						return { value: undefined, done: true };
					}
				};
			}
		}
		var sr = new SteppedRange(0, 10, 3);
		var stepped = [...sr];
		assert.sameValue(stepped.join(","), "0,3,6,9", "stepped range via spread");
	`)
}
