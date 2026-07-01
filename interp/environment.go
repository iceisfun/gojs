package interp

// Environment is a lexical scope: a set of variable bindings plus a link to the
// enclosing scope. Environments form a chain from the innermost block out to
// the global environment.
//
// Binding kinds:
//   - var / function declarations are hoisted to the nearest function (or
//     global) scope; these use mutable, pre-initialized bindings.
//   - let / const are block-scoped and start uninitialized, giving the
//     Temporal Dead Zone: reading before initialization is a ReferenceError.
type Environment struct {
	parent  *Environment
	vars    map[string]*binding
	fnScope bool            // true for function bodies and the global scope
	thisVal Value           // `this` binding for this scope (nil = inherit from parent)
	hasThis bool            // whether thisVal is set at this scope
	newTgt  Value           // new.target for this scope
	homeObj *Object         // [[HomeObject]] for super resolution in methods
	gen     *generatorState // active generator channels (set in generator bodies)

	// superInit tracks whether super() has run in a derived constructor scope.
	// It is non-nil only on the scope that establishes a derived constructor's
	// `this`; reading `this` before super() (superInit.called == false) is a
	// ReferenceError, and a second super() call is likewise rejected.
	superInit *superInitState

	// privNames is the class-body private environment: the PrivateName identity
	// for each private element (#x) declared in the class whose scope this is.
	// It is set only on a class's scope; a reference resolves by walking the
	// enclosing scopes, so nested classes see their outer classes' privates.
	privNames map[string]*PrivateName
}

// resolvePrivate returns the PrivateName identity that a textual private name
// (#x) refers to in this scope, walking enclosing class scopes, or nil when no
// enclosing class declares it (a case the parser already rejects).
func (e *Environment) resolvePrivate(name string) *PrivateName {
	for env := e; env != nil; env = env.parent {
		if env.privNames != nil {
			if pn, ok := env.privNames[name]; ok {
				return pn
			}
		}
	}
	return nil
}

// privateNamesInScope returns every private name (#x) visible from this scope,
// so a direct eval can be parsed with the same private environment.
func (e *Environment) privateNamesInScope() []string {
	var names []string
	seen := map[string]bool{}
	for env := e; env != nil; env = env.parent {
		for n := range env.privNames {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	return names
}

// inDerivedConstructor reports whether a derived constructor's `this`-binding
// scope is in effect, so a direct eval here may contain a SuperCall.
func (e *Environment) inDerivedConstructor() bool {
	for env := e; env != nil; env = env.parent {
		if env.superInit != nil {
			return true
		}
	}
	return false
}

// superInitState records whether a derived constructor has invoked super().
type superInitState struct{ called bool }

// generator returns the generator state for the nearest enclosing generator
// body, or nil when the current scope is not inside one.
func (e *Environment) generator() *generatorState {
	for env := e; env != nil; env = env.parent {
		if env.gen != nil {
			return env.gen
		}
		// An ordinary (non-generator) function body stops the search: a yield
		// there belongs to no generator.
		if env.fnScope {
			return nil
		}
	}
	return nil
}

// binding is a single variable slot.
type binding struct {
	value       Value
	mutable     bool // false for const
	initialized bool // false while in the Temporal Dead Zone
}

// NewEnvironment creates a child environment of parent. If fnScope is true, the
// environment acts as a var/function hoisting target.
func NewEnvironment(parent *Environment, fnScope bool) *Environment {
	return &Environment{
		parent:  parent,
		vars:    make(map[string]*binding),
		fnScope: fnScope,
	}
}

// declare creates a lexical binding (let/const/class) in this environment,
// initially in the TDZ. It overwrites any existing binding of the same name in
// this scope.
func (e *Environment) declareLexical(name string, mutable bool) *binding {
	b := &binding{mutable: mutable, initialized: false}
	e.vars[name] = b
	return b
}

// declareVar creates (or reuses) a hoisted, pre-initialized var binding in the
// nearest function/global scope.
func (e *Environment) declareVar(name string, v Value) {
	target := e.functionScope()
	if b, ok := target.vars[name]; ok {
		if v != nil {
			b.value = v
		}
		b.initialized = true
		return
	}
	init := Value(Undef)
	if v != nil {
		init = v
	}
	target.vars[name] = &binding{value: init, mutable: true, initialized: true}
}

// functionScope returns the nearest enclosing function or global environment.
func (e *Environment) functionScope() *Environment {
	for env := e; env != nil; env = env.parent {
		if env.fnScope {
			return env
		}
	}
	return e
}

// lookup returns the binding for name, searching outward, or nil.
func (e *Environment) lookup(name string) *binding {
	for env := e; env != nil; env = env.parent {
		if b, ok := env.vars[name]; ok {
			return b
		}
	}
	return nil
}

// has reports whether name is bound anywhere in the chain.
func (e *Environment) has(name string) bool { return e.lookup(name) != nil }

// thisBinding returns the effective `this` value, searching outward to the
// nearest scope that established one.
func (e *Environment) thisBinding() (Value, bool) {
	for env := e; env != nil; env = env.parent {
		if env.hasThis {
			return env.thisVal, true
		}
	}
	return Undef, false
}

// setThis establishes the `this` binding for this environment.
func (e *Environment) setThis(v Value) {
	e.thisVal = v
	e.hasThis = true
}

// thisScope returns the nearest environment that establishes a `this` binding,
// or nil. It is used to locate a derived constructor's super-init state so that
// reading `this` before super() can be rejected.
func (e *Environment) thisScope() *Environment {
	for env := e; env != nil; env = env.parent {
		if env.hasThis {
			return env
		}
	}
	return nil
}

// homeObject returns the nearest [[HomeObject]] for super resolution.
func (e *Environment) homeObject() *Object {
	for env := e; env != nil; env = env.parent {
		if env.homeObj != nil {
			return env.homeObj
		}
	}
	return nil
}

// newTarget returns the nearest new.target value.
func (e *Environment) newTarget() Value {
	for env := e; env != nil; env = env.parent {
		if env.newTgt != nil {
			return env.newTgt
		}
	}
	return Undef
}
