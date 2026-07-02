package harness

import "testing"

// drain wraps a body so that a synchronous log array can be inspected after all
// microtasks settle. The body pushes to `log`; the trailing then() keeps the
// event loop pumping until the promise graph settles, and console.log emits the
// final log so the Go test can assert on it.

func TestPromiseSpeciesGetter(t *testing.T) {
	Expect(t, `
		assert.sameValue(Promise[Symbol.species], Promise, "species is Promise");
		var desc = Object.getOwnPropertyDescriptor(Promise, Symbol.species);
		assert.sameValue(typeof desc.get, "function", "species has a getter");
		assert.sameValue(desc.set, undefined, "species has no setter");
		assert.sameValue(desc.enumerable, false);
		assert.sameValue(desc.configurable, true);
	`)
}

func TestPromiseWithResolvers(t *testing.T) {
	Expect(t, `
		assert.sameValue(typeof Promise.withResolvers, "function");
		var d = Promise.withResolvers();
		assert.sameValue(d.promise instanceof Promise, true);
		assert.sameValue(typeof d.resolve, "function");
		assert.sameValue(typeof d.reject, "function");
	`)
	out := Expect(t, `
		var log = [];
		var d = Promise.withResolvers();
		d.promise.then(function(v){ log.push("f:"+v); console.log(log.join(",")); });
		d.resolve(42);
	`)
	if len(out) != 1 || out[0] != "f:42" {
		t.Errorf("withResolvers resolve = %v, want [f:42]", out)
	}
}

func TestPromiseTry(t *testing.T) {
	Expect(t, `assert.sameValue(typeof Promise.try, "function");`)
	out := Expect(t, `
		var log = [];
		Promise.try(function(a,b){ return a+b; }, 2, 3).then(function(v){
			log.push("v:"+v); console.log(log.join(","));
		});
	`)
	if len(out) != 1 || out[0] != "v:5" {
		t.Errorf("Promise.try = %v, want [v:5]", out)
	}
	out = Expect(t, `
		var log = [];
		Promise.try(function(){ throw new TypeError("x"); }).then(
			function(){ log.push("f"); },
			function(e){ log.push("r:"+(e instanceof TypeError)); console.log(log.join(",")); });
	`)
	if len(out) != 1 || out[0] != "r:true" {
		t.Errorf("Promise.try throw = %v, want [r:true]", out)
	}
}

func TestPromiseResolveThisSemantics(t *testing.T) {
	// Promise.resolve uses `this` as the constructor.
	ExpectError(t, `Promise.resolve.call(undefined, 1);`, "TypeError")
	ExpectError(t, `Promise.resolve.call(1, 1);`, "TypeError")
	// Promise.resolve on Promise returns the same object when arg is a Promise.
	Expect(t, `
		var p = Promise.resolve(1);
		assert.sameValue(Promise.resolve(p), p, "same promise returned");
	`)
	// Custom constructor: uses NewPromiseCapability(this).
	Expect(t, `
		var calls = 0;
		function Custom(executor){ calls++; executor(function(){}, function(){}); }
		Custom.resolve = Promise.resolve;
		var r = Promise.resolve.call(Custom, 5);
		assert.sameValue(calls, 1, "custom constructor invoked once");
		assert.sameValue(r instanceof Custom, true);
	`)
}

func TestPromiseRejectThisSemantics(t *testing.T) {
	ExpectError(t, `Promise.reject.call(undefined, 1);`, "TypeError")
	Expect(t, `
		var calls = 0;
		function Custom(executor){ calls++; executor(function(){}, function(){}); }
		var r = Promise.reject.call(Custom, 5);
		assert.sameValue(calls, 1, "custom constructor invoked once");
		assert.sameValue(r instanceof Custom, true);
	`)
}

func TestPromiseAllResolveElementFunction(t *testing.T) {
	// The resolve element function passed to a thenable's `then` is an anonymous
	// length-1 function per §27.2.4.1.2.
	Expect(t, `
		var resolveElementFunction;
		var thenable = { then: function(fulfill){ resolveElementFunction = fulfill; } };
		function NotPromise(executor){ executor(function(){}, function(){}); }
		NotPromise.resolve = function(v){ return v; };
		Promise.all.call(NotPromise, [thenable]);
		assert.sameValue(typeof resolveElementFunction, "function");
		assert.sameValue(resolveElementFunction.name, "");
		assert.sameValue(resolveElementFunction.length, 1);
		assert.sameValue(resolveElementFunction.hasOwnProperty("prototype"), false);
	`)
}

func TestPromiseAllIteratorCloseOnResolveThrow(t *testing.T) {
	// When C.resolve throws, the iterator must be closed exactly once.
	Expect(t, `
		var callCount = 0;
		var iter = {};
		iter[Symbol.iterator] = function(){
			return {
				next: function(){ return { value: null, done: false }; },
				return: function(){ callCount += 1; return {}; }
			};
		};
		var saved = Promise.resolve;
		Promise.resolve = function(){ throw new Test262Error(); };
		try { Promise.all(iter); } finally { Promise.resolve = saved; }
		assert.sameValue(callCount, 1, "iterator closed once");
	`)
}

func TestPromiseAllBasic(t *testing.T) {
	out := Expect(t, `
		var log = [];
		Promise.all([1, Promise.resolve(2), 3]).then(function(v){
			log.push(v.join(",")); console.log(log.join(";"));
		});
	`)
	if len(out) != 1 || out[0] != "1,2,3" {
		t.Errorf("Promise.all = %v, want [1,2,3]", out)
	}
}

func TestPromiseAnyAggregateError(t *testing.T) {
	Expect(t, `assert.sameValue(typeof AggregateError, "function", "AggregateError global");`)
	out := Expect(t, `
		var log = [];
		Promise.any([Promise.reject(1), Promise.reject(2)]).then(
			function(){ log.push("f"); },
			function(e){
				log.push("agg:"+(e instanceof AggregateError));
				log.push("errs:"+e.errors.join(","));
				console.log(log.join(";"));
			});
	`)
	if len(out) != 1 || out[0] != "agg:true;errs:1,2" {
		t.Errorf("Promise.any = %v, want [agg:true;errs:1,2]", out)
	}
}

func TestPromiseAllSettled(t *testing.T) {
	out := Expect(t, `
		var log = [];
		Promise.allSettled([Promise.resolve(1), Promise.reject(2)]).then(function(r){
			log.push(r[0].status+":"+r[0].value);
			log.push(r[1].status+":"+r[1].reason);
			console.log(log.join(";"));
		});
	`)
	if len(out) != 1 || out[0] != "fulfilled:1;rejected:2" {
		t.Errorf("Promise.allSettled = %v, want [fulfilled:1;rejected:2]", out)
	}
}

func TestPromiseThenSpecies(t *testing.T) {
	// then uses SpeciesConstructor: a custom @@species constructor is invoked.
	Expect(t, `
		var callCount = 0;
		var p1 = new Promise(function(){});
		function Ctor(){}
		Ctor[Symbol.species] = function(executor){
			callCount++;
			executor(function(){}, function(){});
		};
		p1.constructor = Ctor;
		p1.then();
		assert.sameValue(callCount, 1, "species constructor invoked once");
	`)
}

func TestPromiseThenBrandCheck(t *testing.T) {
	// then on Promise.prototype (no [[PromiseState]]) throws TypeError.
	ExpectError(t, `Promise.prototype.then.call(Promise.prototype, function(){});`, "TypeError")
	ExpectError(t, `Promise.prototype.then.call({}, function(){});`, "TypeError")
}

func TestPromiseToStringTag(t *testing.T) {
	Expect(t, `
		assert.sameValue(Promise.prototype[Symbol.toStringTag], "Promise");
		assert.sameValue(Object.prototype.toString.call(Promise.resolve(1)), "[object Promise]");
	`)
}

func TestPromiseCatchGenericThis(t *testing.T) {
	// catch is generic: it invokes this.then even when this is not a native Promise.
	Expect(t, `
		var count = 0;
		var thenable = { then: function(onF, onR){ count++; assert.sameValue(onF, undefined); } };
		Promise.prototype.catch.call(thenable, function(){});
		assert.sameValue(count, 1);
	`)
	// catch on an object-coercible primitive boxes it (§7.3.18 Invoke).
	Expect(t, `
		var booleanCount = 0;
		Boolean.prototype.then = function(){ booleanCount += 1; };
		Promise.prototype.catch.call(true);
		assert.sameValue(booleanCount, 1, "boolean this");
	`)
}

func TestPromiseFinallyGenericThis(t *testing.T) {
	// finally is generic over `this` (only requires an object) and dispatches
	// through this.then.
	Expect(t, `
		var count = 0;
		var thenable = { then: function(onF, onR){ count++; } };
		Promise.prototype.finally.call(thenable, function(){});
		assert.sameValue(count, 1);
	`)
	ExpectError(t, `Promise.prototype.finally.call(1, function(){});`, "TypeError")
}

func TestPromiseResolveFunctionsAnonymous(t *testing.T) {
	Expect(t, `
		var resolveFn, rejectFn;
		new Promise(function(res, rej){ resolveFn = res; rejectFn = rej; });
		assert.sameValue(resolveFn.name, "", "resolve is anonymous");
		assert.sameValue(rejectFn.name, "", "reject is anonymous");
	`)
}

func TestPromiseRace(t *testing.T) {
	out := Expect(t, `
		var log = [];
		Promise.race([Promise.resolve("a"), Promise.resolve("b")]).then(function(v){
			log.push(v); console.log(log.join(";"));
		});
	`)
	if len(out) != 1 || out[0] != "a" {
		t.Errorf("Promise.race = %v, want [a]", out)
	}
}
