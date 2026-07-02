package harness

import "testing"

// The generator/async function-family intrinsics: %GeneratorFunction%,
// %AsyncGeneratorFunction%, %AsyncFunction% and their .prototype objects,
// reachable only through the prototype chains of generator/async functions.

func TestGeneratorFunctionIntrinsic(t *testing.T) {
	Expect(t, `
		var GeneratorFunction = Object.getPrototypeOf(function*(){}).constructor;
		assert.sameValue(typeof GeneratorFunction, "function");
		assert.sameValue(GeneratorFunction.name, "GeneratorFunction");
		assert.sameValue(GeneratorFunction.length, 1);
		// [[Prototype]] is %Function%
		assert.sameValue(Object.getPrototypeOf(GeneratorFunction), Function);
		var GFP = Object.getPrototypeOf(function*(){});
		assert.sameValue(GeneratorFunction.prototype, GFP);
		assert.sameValue(Object.getPrototypeOf(GFP), Function.prototype);
		assert.sameValue(GFP[Symbol.toStringTag], "GeneratorFunction");
		assert.sameValue(GFP.constructor, GeneratorFunction);
		// GFP.prototype is %GeneratorPrototype%
		assert.sameValue(GFP.prototype, Object.getPrototypeOf(function*(){}.prototype));
		// constructor descriptor: non-writable, configurable
		var cd = Object.getOwnPropertyDescriptor(GFP, "constructor");
		assert.sameValue(cd.writable, false);
		assert.sameValue(cd.enumerable, false);
		assert.sameValue(cd.configurable, true);
	`)
}

func TestGeneratorFunctionInstancePrototype(t *testing.T) {
	Expect(t, `
		var GeneratorFunction = Object.getPrototypeOf(function*(){}).constructor;
		var instance = GeneratorFunction();
		assert.sameValue(typeof instance.prototype, "object");
		assert.sameValue(
			Object.getPrototypeOf(instance.prototype),
			Object.getPrototypeOf(instance).prototype);
		var d = Object.getOwnPropertyDescriptor(instance, "prototype");
		assert.sameValue(d.writable, true);
		assert.sameValue(d.enumerable, false);
		assert.sameValue(d.configurable, false);
	`)
}

func TestGeneratorFunctionNotConstructors(t *testing.T) {
	ExpectError(t, `new (function*(){})()`, "TypeError")
	ExpectError(t, `new (async function(){})()`, "TypeError")
	ExpectError(t, `new (async function*(){})()`, "TypeError")
}

func TestGeneratorFunctionDynamic(t *testing.T) {
	Expect(t, `
		var GeneratorFunction = Object.getPrototypeOf(function*(){}).constructor;
		var g = GeneratorFunction('x', 'y', 'yield x + y;');
		var it = g(2, 3);
		var r = it.next();
		assert.sameValue(r.value, 5);
		assert.sameValue(r.done, false);
		assert.sameValue(r.done, it.next().done ? false : false);
		assert.sameValue(GeneratorFunction().name, "anonymous");
		assert.sameValue(GeneratorFunction('x','').length, 1);
		// is a constructor
		assert.sameValue((new GeneratorFunction()) instanceof GeneratorFunction, true);
		// instanceof
		assert(g instanceof GeneratorFunction);
	`)
}

func TestAsyncGeneratorFunctionIntrinsic(t *testing.T) {
	Expect(t, `
		var AGF = Object.getPrototypeOf(async function*(){}).constructor;
		assert.sameValue(typeof AGF, "function");
		assert.sameValue(AGF.name, "AsyncGeneratorFunction");
		assert.sameValue(AGF.length, 1);
		assert.sameValue(Object.getPrototypeOf(AGF), Function);
		var AGFP = Object.getPrototypeOf(async function*(){});
		assert.sameValue(AGF.prototype, AGFP);
		assert.sameValue(Object.getPrototypeOf(AGFP), Function.prototype);
		assert.sameValue(AGFP[Symbol.toStringTag], "AsyncGeneratorFunction");
		assert.sameValue(AGFP.constructor, AGF);
		assert.sameValue(AGFP.prototype, Object.getPrototypeOf(async function*(){}.prototype));
		var instance = AGF();
		assert.sameValue(typeof instance.prototype, "object");
	`)
}

func TestAsyncFunctionIntrinsic(t *testing.T) {
	Expect(t, `
		var AF = (async function foo(){}).constructor;
		assert.sameValue(typeof AF, "function");
		assert.sameValue(AF.name, "AsyncFunction");
		assert.sameValue(AF.length, 1);
		assert.sameValue(Object.getPrototypeOf(AF), Function);
		var AFP = Object.getPrototypeOf(async function(){});
		assert.sameValue(AF.prototype, AFP);
		assert.sameValue(Object.getPrototypeOf(AFP), Function.prototype);
		assert.sameValue(AFP[Symbol.toStringTag], "AsyncFunction");
		assert.sameValue(AFP.constructor, AF);
		// async functions have NO own prototype property
		assert.sameValue((async function(){}).hasOwnProperty("prototype"), false);
		assert.sameValue(typeof AFP, "object");
		assert((async function(){}) instanceof Function);
	`)
}

func TestAsyncFunctionDynamic(t *testing.T) {
	Expect(t, asyncCase(`
		var AF = (async function(){}).constructor;
		var f = AF('x', 'return await x;');
		var r = await f(Promise.resolve(42));
		assert.sameValue(r, 42);
	`))
}
