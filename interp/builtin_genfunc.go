package interp

import "context"

// This file installs the generator/async function-family intrinsics defined in
// ECMA-262 §27.3 (%GeneratorFunction%), §27.4 (%AsyncGeneratorFunction%) and
// §27.7 (%AsyncFunction%), together with their prototype objects
// (%GeneratorFunction.prototype% aka %Generator%, %AsyncGeneratorFunction.prototype%
// aka %AsyncGenerator%, and %AsyncFunction.prototype%).
//
// None of these constructors is a global binding; each is reachable only through
// the prototype chain of a corresponding function, e.g.
//
//	Object.getPrototypeOf(function*(){}).constructor  === %GeneratorFunction%
//	(async function(){}).constructor                  === %AsyncFunction%
//	(async function*(){}).constructor                 === %AsyncGeneratorFunction%
//
// The chains are wired by makeFunction, which sets a generator/async function
// object's [[Prototype]] to the matching %…Function.prototype% intrinsic.

// initGenAsyncFunctions builds the three function-family constructor/prototype
// pairs. It must run after initFunction (which creates %Function% and
// %Function.prototype%) and after the iterator/async-iterator prototypes exist.
func (i *Interpreter) initGenAsyncFunctions() {
	// %GeneratorFunction.prototype% (%Generator%): an ordinary, non-callable
	// object whose [[Prototype]] is %Function.prototype%.
	i.genFuncProto = NewObject(i.functionProto)
	i.genFuncCtor = i.makeFamilyCtor("GeneratorFunction", i.genFuncProto, dynGenerator)
	// GeneratorFunction.prototype.prototype is %GeneratorPrototype%.
	i.genFuncProto.defineOwn(StrKey("prototype"), &Property{Value: i.generatorProto, Writable: false, Enumerable: false, Configurable: true})
	i.tagFamilyProto(i.genFuncProto, i.genFuncCtor, "GeneratorFunction")

	// %AsyncGeneratorFunction.prototype% (%AsyncGenerator%).
	i.asyncGenFuncProto = NewObject(i.functionProto)
	i.asyncGenFuncCtor = i.makeFamilyCtor("AsyncGeneratorFunction", i.asyncGenFuncProto, dynAsyncGenerator)
	i.asyncGenFuncProto.defineOwn(StrKey("prototype"), &Property{Value: i.asyncGeneratorProto, Writable: false, Enumerable: false, Configurable: true})
	i.tagFamilyProto(i.asyncGenFuncProto, i.asyncGenFuncCtor, "AsyncGeneratorFunction")

	// %AsyncFunction.prototype% has no "prototype" property (async functions are
	// not constructors and expose no .prototype).
	i.asyncFuncProto = NewObject(i.functionProto)
	i.asyncFuncCtor = i.makeFamilyCtor("AsyncFunction", i.asyncFuncProto, dynAsync)
	i.tagFamilyProto(i.asyncFuncProto, i.asyncFuncCtor, "AsyncFunction")
}

// makeFamilyCtor builds one of the family constructors: a callable object that
// creates a function of the given dynamic kind from source strings. Its
// [[Prototype]] is %Function% (the Function constructor), its "prototype" own
// property is proto, its name is name, and its length is 1.
func (i *Interpreter) makeFamilyCtor(name string, proto *Object, kind dynFuncKind) *Object {
	ctor := i.newNativeCtor(name, 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		return i.createDynamicFunctionKind(ctx, kind, args)
	}, nil)
	// A subclass of %Function%: Object.getPrototypeOf(ctor) === Function.
	ctor.SetProto(i.functionCtor)
	// ctor.prototype = proto, { writable:false, enumerable:false, configurable:false }.
	ctor.defineOwn(StrKey("prototype"), &Property{Value: proto, Writable: false, Enumerable: false, Configurable: false})
	return ctor
}

// tagFamilyProto installs the shared own properties of a %…Function.prototype%
// object: a non-writable "constructor" back-reference and a @@toStringTag.
func (i *Interpreter) tagFamilyProto(proto, ctor *Object, tag string) {
	proto.defineOwn(StrKey("constructor"), &Property{Value: ctor, Writable: false, Enumerable: false, Configurable: true})
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String(tag), Writable: false, Enumerable: false, Configurable: true})
}
