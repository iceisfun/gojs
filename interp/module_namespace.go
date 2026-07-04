package interp

import (
	"context"
	"sort"
	"unicode/utf16"
)

// This file implements the Module Namespace exotic object (ECMA-262 §10.4.6):
// the object returned as the resolution of a dynamic import() and reached
// through `import * as ns`. It is a highly constrained object — null prototype,
// non-extensible, read-only bindings, code-unit-sorted keys, and a fixed
// @@toStringTag of "Module" — whose string keys expose live module exports.
//
// The exotic behavior is spread across the essential-internal-method choke
// points (getOwn, getWithReceiver, setStatus, ordinarySet, Delete via getOwn,
// defineOwnFromDescriptor, OwnKeys, ownPropertyKeys) which each branch on a
// non-nil Object.namespace; the state and helpers those branches use live here.

// nsExotic holds the state of a Module Namespace exotic object: the sorted set
// of exported string names and, for each, a reader that yields the live binding
// value from the module scope (returning a ReferenceError for a still-uninitialized
// export, i.e. a TDZ access through the namespace).
type nsExotic struct {
	names []string // exported names, sorted by UTF-16 code unit (§10.4.6.11)
	read  map[string]func(context.Context) (Value, error)
}

// newModuleNamespace builds the Module Namespace exotic object for module id.
// Its keys are the module's unambiguous exported names (GetExportedNames filtered
// by ResolveExport); each key's reader resolves the export to the module and
// local binding that actually declares it and reads that live value — a
// namespace binding (`export * as ns`) reads the source module's namespace,
// yielding a nested namespace object.
func (i *Interpreter) newModuleNamespace(ctx context.Context, id string) *Object {
	ns := NewObject(nil) // [[Prototype]] is null (§10.4.6.2)
	ns.extensible = false // [[IsExtensible]] is always false (§10.4.6.4)
	ns.immutableProto = true // [[SetPrototypeOf]] succeeds only for null (§10.4.6.3)

	nx := &nsExotic{read: make(map[string]func(context.Context) (Value, error))}
	for _, name := range i.getExportedNames(ctx, id, map[string]bool{}) {
		res, ambiguous, err := i.resolveExport(ctx, id, name, nil)
		if err != nil || ambiguous || res == nil {
			continue // an ambiguous or unresolvable name is not a namespace key
		}
		if _, dup := nx.read[name]; dup {
			continue
		}
		binding := *res // capture the resolved (module, local binding)
		nx.read[name] = func(ctx context.Context) (Value, error) {
			return i.readModuleBinding(ctx, binding)
		}
		nx.names = append(nx.names, name)
	}
	sortCodeUnits(nx.names)
	ns.namespace = nx

	// @@toStringTag is a non-writable, non-enumerable, non-configurable "Module"
	// data property (§10.4.6.1), so Object.prototype.toString reports
	// "[object Module]" and the tag cannot be redefined or deleted.
	ns.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("Module")})
	return ns
}

// readModuleBinding reads the live value of a resolved export binding. A
// namespace binding (`export * as ns`) yields the source module's namespace
// object (a nested namespace); an ordinary binding evaluates the declaring
// module (once, cached) and reads its local, throwing a ReferenceError for a
// still-uninitialized (TDZ) binding.
func (i *Interpreter) readModuleBinding(ctx context.Context, b resolvedBinding) (Value, error) {
	if b.bindingName == starNamespace {
		return i.importByID(ctx, b.moduleID)
	}
	if _, err := i.importByID(ctx, b.moduleID); err != nil {
		return nil, err
	}
	env := i.moduleEnvs[b.moduleID]
	if env == nil {
		return Undef, nil
	}
	bind := env.lookup(b.bindingName)
	if bind == nil {
		return Undef, nil
	}
	if !bind.initialized {
		return nil, i.throwError(ctx, "ReferenceError", "Cannot access '"+b.bindingName+"' before initialization")
	}
	return bind.value, nil
}

// namespaceDefineOwn implements the Module Namespace [[DefineOwnProperty]]
// (§10.4.6.7) for a string export key: the redefinition is accepted only when it
// preserves the export's fixed shape — non-configurable, enumerable, a writable
// data property — and, if a value is given, leaves it unchanged. It never mutates
// the binding.
func (i *Interpreter) namespaceDefineOwn(ctx context.Context, o *Object, key PropertyKey, desc *Object) (bool, error) {
	cur, ok := o.getOwn(key)
	if !ok {
		return false, nil // not an export: no property to redefine
	}
	if desc.Has(StrKey("configurable")) {
		v, err := desc.GetStr(ctx, "configurable")
		if err != nil {
			return false, err
		}
		if ToBoolean(v) {
			return false, nil
		}
	}
	if desc.Has(StrKey("enumerable")) {
		v, err := desc.GetStr(ctx, "enumerable")
		if err != nil {
			return false, err
		}
		if !ToBoolean(v) {
			return false, nil
		}
	}
	if desc.Has(StrKey("get")) || desc.Has(StrKey("set")) {
		return false, nil // an accessor descriptor is rejected
	}
	if desc.Has(StrKey("writable")) {
		v, err := desc.GetStr(ctx, "writable")
		if err != nil {
			return false, err
		}
		if !ToBoolean(v) {
			return false, nil
		}
	}
	if desc.Has(StrKey("value")) {
		v, err := desc.GetStr(ctx, "value")
		if err != nil {
			return false, err
		}
		return sameValue(v, cur.Value), nil
	}
	return true, nil
}

// sortCodeUnits sorts names ascending by UTF-16 code unit, the ordering the spec
// prescribes for a Module Namespace object's own keys (§10.4.6.11 uses
// SortStringListByCodeUnit). This differs from Go's byte (UTF-8) ordering only
// for supplementary-plane characters, but matches the spec for all inputs.
func sortCodeUnits(names []string) {
	sort.Slice(names, func(a, b int) bool {
		return lessCodeUnits(names[a], names[b])
	})
}

// lessCodeUnits reports whether a precedes b in UTF-16 code-unit order.
func lessCodeUnits(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for k := 0; k < len(ua) && k < len(ub); k++ {
		if ua[k] != ub[k] {
			return ua[k] < ub[k]
		}
	}
	return len(ua) < len(ub)
}
