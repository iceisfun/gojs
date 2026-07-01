package interp

import "context"

// bootstrap builds the realm: the intrinsic prototype objects, the built-in
// constructors, and the global object/environment. It is called once from New.
//
// Ordering matters. objectProto and functionProto must exist before any native
// function is created (newNativeFunc sets functionProto as the function's
// prototype), so they are created as bare objects first and wired afterward.
func (i *Interpreter) bootstrap() {
	// 1. Root prototypes.
	i.objectProto = &Object{props: map[PropertyKey]*Property{}, extensible: true, class: "Object"}
	i.functionProto = &Object{proto: i.objectProto, props: map[PropertyKey]*Property{}, extensible: true, class: "Function"}
	// functionProto is itself callable (a no-op returning undefined).
	i.functionProto.fn = &functionData{
		call:   func(context.Context, Value, []Value) (Value, error) { return Undef, nil },
		name:   "",
		length: 0,
	}

	// 2. Remaining intrinsic prototypes (bare; methods added by initializers).
	i.arrayProto = NewObject(i.objectProto)
	i.arrayProto.isArray = true
	i.arrayProto.elems = []Value{}
	i.stringProto = NewObject(i.objectProto)
	i.numberProto = NewObject(i.objectProto)
	i.booleanProto = NewObject(i.objectProto)
	i.symbolProto = NewObject(i.objectProto)
	i.bigintProto = NewObject(i.objectProto)
	i.errorProto = NewObject(i.objectProto)
	i.regexpProto = NewObject(i.objectProto)
	i.mapProto = NewObject(i.objectProto)
	i.setProto = NewObject(i.objectProto)
	i.promiseProto = NewObject(i.objectProto)
	i.iteratorProto = NewObject(i.objectProto)
	i.generatorProto = NewObject(i.iteratorProto)
	i.dateProto = NewObject(i.objectProto)
	i.nativeErrorProtos = make(map[string]*Object)
	i.nativeErrorCtors = make(map[string]*Object)

	// 3. Global object and environment.
	i.global = NewObject(i.objectProto)
	i.global.class = "global"
	i.globalEnv = NewEnvironment(nil, true)
	i.globalEnv.setThis(i.global)

	// 4. Populate intrinsics and globals.
	i.initObject()
	i.initFunction()
	i.initError()
	i.initArray()
	i.initString()
	i.initNumber()
	i.initBoolean()
	i.initSymbol()
	i.initMath()
	i.initJSON()
	i.initRegExp()
	i.initCollections()
	i.initConsole()
	i.initGlobals()
	i.initTimers()
}

// setGlobal defines an enumerable global binding both on the global object and
// (for lexical resolution) reachable through the global environment. Global
// var/function bindings live on the global object; the environment falls back
// to the global object during lookup (see resolveIdent).
func (i *Interpreter) setGlobal(name string, v Value) {
	i.global.SetData(name, v)
}

// setGlobalHidden defines a non-enumerable global (used for constructors and
// namespaces like Math/JSON, matching real engines).
func (i *Interpreter) setGlobalHidden(name string, v Value) {
	i.global.SetHidden(name, v)
}
