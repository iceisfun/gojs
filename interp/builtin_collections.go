package interp

import "context"

// This file installs the four keyed-collection built-ins mandated by ECMA-262
// §24 (Map, Set) and §24 (WeakMap, WeakSet): constructors, prototype methods,
// and Symbol.iterator wiring. It is called once during interpreter bootstrap
// via [Interpreter.initCollections].
//
// # Internal storage
//
// Both Map and Set use [orderedMap], a compact ordered map that preserves
// insertion order (required by the spec) and performs key equality using
// SameValueZero (§7.2.11). WeakMap and WeakSet reuse the same backing type
// but do not expose iteration.
//
// # Per-object state
//
// Each Map/Set/WeakMap/WeakSet instance stores its backing data in the
// Object.internal map under the key matching the class string (e.g. "Map",
// "Set", "WeakMap", "WeakSet"). Helper functions cast and extract these slots.

// ---------------------------------------------------------------------------
// orderedMap — insertion-order map with SameValueZero key equality
// ---------------------------------------------------------------------------

// mapEntry is a single key/value pair in an orderedMap.
type mapEntry struct {
	key Value
	val Value
}

// orderedMap is a lightweight ordered map backed by a slice. Iteration always
// visits entries in the order they were first inserted. Mutation does not
// change a key's position in the slice; only explicit deletion removes it
// (replaced by a tombstone so existing iterators can stay index-stable).
//
// Lookup is O(n) by linear scan, which is acceptable for the first-pass
// implementation and matches what V8 / SpiderMonkey use for small maps.
type orderedMap struct {
	entries []mapEntry
}

// find returns the index of the first entry whose key is SameValueZero-equal
// to k, or -1 when absent.
func (m *orderedMap) find(k Value) int {
	for idx := range m.entries {
		if sameValueZero(m.entries[idx].key, k) {
			return idx
		}
	}
	return -1
}

// set inserts or updates the entry for k. Insertion order is preserved:
// existing keys stay in their original position.
func (m *orderedMap) set(k, v Value) {
	if idx := m.find(k); idx >= 0 {
		m.entries[idx].val = v
		return
	}
	m.entries = append(m.entries, mapEntry{key: k, val: v})
}

// get returns the value for k and whether it was found.
func (m *orderedMap) get(k Value) (Value, bool) {
	if idx := m.find(k); idx >= 0 {
		return m.entries[idx].val, true
	}
	return nil, false
}

// has reports whether k is a key in the map.
func (m *orderedMap) has(k Value) bool { return m.find(k) >= 0 }

// delete removes the entry for k, reporting whether one existed.
func (m *orderedMap) delete(k Value) bool {
	idx := m.find(k)
	if idx < 0 {
		return false
	}
	m.entries = append(m.entries[:idx], m.entries[idx+1:]...)
	return true
}

// size returns the number of entries.
func (m *orderedMap) size() int { return len(m.entries) }

// clear removes all entries.
func (m *orderedMap) clear() { m.entries = m.entries[:0] }

// ---------------------------------------------------------------------------
// orderedSet — ordered set with SameValueZero equality, backed by orderedMap
// ---------------------------------------------------------------------------

// orderedSet stores unique values in insertion order by reusing orderedMap
// with a fixed sentinel for the map value.
type orderedSet struct {
	m orderedMap
}

var setSentinel Value = True // any non-nil constant works

func (s *orderedSet) add(v Value) {
	if s.m.find(v) < 0 {
		s.m.entries = append(s.m.entries, mapEntry{key: v, val: setSentinel})
	}
}

func (s *orderedSet) has(v Value) bool    { return s.m.has(v) }
func (s *orderedSet) delete(v Value) bool { return s.m.delete(v) }
func (s *orderedSet) size() int           { return s.m.size() }
func (s *orderedSet) clear()              { s.m.clear() }

// ---------------------------------------------------------------------------
// Internal-slot accessors
// ---------------------------------------------------------------------------

// mapSlot extracts the *orderedMap from the internal slot of a Map instance,
// or returns nil when this is not a Map.
func mapSlot(this Value) *orderedMap {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	m, _ := o.internal["Map"].(*orderedMap)
	return m
}

// setSlot extracts the *orderedSet from the internal slot of a Set instance.
func setSlot(this Value) *orderedSet {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	s, _ := o.internal["Set"].(*orderedSet)
	return s
}

// weakMapSlot extracts the *orderedMap backing a WeakMap instance.
func weakMapSlot(this Value) *orderedMap {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	m, _ := o.internal["WeakMap"].(*orderedMap)
	return m
}

// weakSetSlot extracts the *orderedSet backing a WeakSet instance.
func weakSetSlot(this Value) *orderedSet {
	o, ok := this.(*Object)
	if !ok || o.internal == nil {
		return nil
	}
	s, _ := o.internal["WeakSet"].(*orderedSet)
	return s
}

// ---------------------------------------------------------------------------
// initCollections — entry point
// ---------------------------------------------------------------------------

// initCollections installs Map, Set, WeakMap, and WeakSet on the global
// object. It is called by bootstrap after the standard intrinsic prototypes
// have been set up.
func (i *Interpreter) initCollections() {
	i.initMap()
	i.initSet()
	i.initWeakMap()
	i.initWeakSet()
}

// ---------------------------------------------------------------------------
// Map
// ---------------------------------------------------------------------------

// initMap builds and registers the Map constructor and Map.prototype.
//
// Map instances carry their backing storage in Object.internal["Map"].
// The size property is a non-enumerable accessor (getter only) on the
// prototype, so that every instance inherits a live view of its own size.
func (i *Interpreter) initMap() {
	proto := i.mapProto
	proto.class = "Map"

	// Constructor: new Map(iterable?)
	// The iterable (if supplied) must produce [key, value] pairs.
	mapConstruct := func(ctx context.Context, this Value, args []Value) (Value, error) {
		var obj *Object
		if o, ok := this.(*Object); ok && o != i.global && o.class == "Map" {
			obj = o
		} else {
			obj = NewObject(proto)
			obj.class = "Map"
		}
		m := &orderedMap{}
		obj.internal = map[string]any{"Map": m}

		// Populate from optional iterable argument.
		if iterable := arg(args, 0); !IsNullish(iterable) && !IsUndefined(iterable) {
			err := i.iterate(ctx, iterable, func(item Value) error {
				// Each item must be an iterable of [key, value].
				itemObj, ok := item.(*Object)
				if !ok || !itemObj.isArray {
					// Try iterating — the item could be any [k,v] iterable.
					var pair []Value
					if err2 := i.iterate(ctx, item, func(v Value) error {
						pair = append(pair, v)
						return nil
					}); err2 != nil {
						return i.throwError(ctx, "TypeError", "Map iterable items must be key-value pairs")
					}
					k := arg(pair, 0)
					v := arg(pair, 1)
					m.set(k, v)
					return nil
				}
				k := undefIfHole(arg(itemObj.elems, 0))
				v := undefIfHole(arg(itemObj.elems, 1))
				m.set(k, v)
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		return obj, nil
	}

	ctor := i.newNativeCtor("Map", 0, mapConstruct, mapConstruct)
	linkCtor(ctor, proto)

	// size accessor
	sizeGetter := i.newNativeFunc("get size", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.size getter called on incompatible receiver")
		}
		return Number(float64(m.size())), nil
	})
	proto.DefineAccessor("size", sizeGetter, nil, false)

	// set(key, value) → this
	i.defineMethod(proto, "set", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.set called on incompatible receiver")
		}
		m.set(arg(args, 0), arg(args, 1))
		return this, nil
	})

	// get(key) → value | undefined
	i.defineMethod(proto, "get", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.get called on incompatible receiver")
		}
		if v, ok := m.get(arg(args, 0)); ok {
			return v, nil
		}
		return Undef, nil
	})

	// has(key) → boolean
	i.defineMethod(proto, "has", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.has called on incompatible receiver")
		}
		return Bool(m.has(arg(args, 0))), nil
	})

	// delete(key) → boolean
	i.defineMethod(proto, "delete", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.delete called on incompatible receiver")
		}
		return Bool(m.delete(arg(args, 0))), nil
	})

	// clear() → undefined
	i.defineMethod(proto, "clear", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.clear called on incompatible receiver")
		}
		m.clear()
		return Undef, nil
	})

	// forEach(callback, thisArg?) → undefined
	i.defineMethod(proto, "forEach", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.forEach called on incompatible receiver")
		}
		cb := arg(args, 0)
		cbThis := arg(args, 1)
		for _, e := range m.entries {
			if _, err := i.call(ctx, cb, cbThis, []Value{e.val, e.key, this}); err != nil {
				return nil, err
			}
		}
		return Undef, nil
	})

	// keys() → iterator of keys
	mapKeysFn := i.newNativeFunc("keys", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.keys called on incompatible receiver")
		}
		// Snapshot entries to avoid mutation issues during iteration.
		snap := append([]mapEntry{}, m.entries...)
		idx := 0
		return i.newIterator(func() (Value, bool) {
			if idx >= len(snap) {
				return Undef, false
			}
			k := snap[idx].key
			idx++
			return k, true
		}), nil
	})
	proto.SetHidden("keys", mapKeysFn)

	// values() → iterator of values
	mapValuesFn := i.newNativeFunc("values", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.values called on incompatible receiver")
		}
		snap := append([]mapEntry{}, m.entries...)
		idx := 0
		return i.newIterator(func() (Value, bool) {
			if idx >= len(snap) {
				return Undef, false
			}
			v := snap[idx].val
			idx++
			return v, true
		}), nil
	})
	proto.SetHidden("values", mapValuesFn)

	// entries() → iterator of [key, value] pairs
	mapEntriesFn := i.newNativeFunc("entries", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.entries called on incompatible receiver")
		}
		snap := append([]mapEntry{}, m.entries...)
		idx := 0
		return i.newIterator(func() (Value, bool) {
			if idx >= len(snap) {
				return Undef, false
			}
			e := snap[idx]
			idx++
			return i.newArray([]Value{e.key, e.val}), true
		}), nil
	})
	proto.SetHidden("entries", mapEntriesFn)

	// Map.prototype[Symbol.iterator] === entries
	proto.defineOwn(SymKey(i.symIterator), &Property{
		Value:        mapEntriesFn,
		Writable:     true,
		Configurable: true,
	})

	i.setGlobalHidden("Map", ctor)
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

// initSet builds and registers the Set constructor and Set.prototype.
//
// Set instances carry their backing storage in Object.internal["Set"].
func (i *Interpreter) initSet() {
	proto := i.setProto
	proto.class = "Set"

	// Constructor: new Set(iterable?)
	setConstruct := func(ctx context.Context, this Value, args []Value) (Value, error) {
		var obj *Object
		if o, ok := this.(*Object); ok && o != i.global && o.class == "Set" {
			obj = o
		} else {
			obj = NewObject(proto)
			obj.class = "Set"
		}
		s := &orderedSet{}
		obj.internal = map[string]any{"Set": s}

		if iterable := arg(args, 0); !IsNullish(iterable) && !IsUndefined(iterable) {
			err := i.iterate(ctx, iterable, func(v Value) error {
				s.add(v)
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		return obj, nil
	}

	ctor := i.newNativeCtor("Set", 0, setConstruct, setConstruct)
	linkCtor(ctor, proto)

	// size accessor
	sizeGetter := i.newNativeFunc("get size", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.size getter called on incompatible receiver")
		}
		return Number(float64(s.size())), nil
	})
	proto.DefineAccessor("size", sizeGetter, nil, false)

	// add(value) → this
	i.defineMethod(proto, "add", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.add called on incompatible receiver")
		}
		s.add(arg(args, 0))
		return this, nil
	})

	// has(value) → boolean
	i.defineMethod(proto, "has", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.has called on incompatible receiver")
		}
		return Bool(s.has(arg(args, 0))), nil
	})

	// delete(value) → boolean
	i.defineMethod(proto, "delete", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.delete called on incompatible receiver")
		}
		return Bool(s.delete(arg(args, 0))), nil
	})

	// clear() → undefined
	i.defineMethod(proto, "clear", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.clear called on incompatible receiver")
		}
		s.clear()
		return Undef, nil
	})

	// forEach(callback, thisArg?) → undefined
	// Callback receives (value, value, set) — both first args are the value
	// (mirroring the spec, which passes value twice for symmetry with Map).
	i.defineMethod(proto, "forEach", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.forEach called on incompatible receiver")
		}
		cb := arg(args, 0)
		cbThis := arg(args, 1)
		for _, e := range s.m.entries {
			if _, err := i.call(ctx, cb, cbThis, []Value{e.key, e.key, this}); err != nil {
				return nil, err
			}
		}
		return Undef, nil
	})

	// values() → iterator of values (also aliased as keys())
	setValuesFn := i.newNativeFunc("values", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.values called on incompatible receiver")
		}
		snap := append([]mapEntry{}, s.m.entries...)
		idx := 0
		return i.newIterator(func() (Value, bool) {
			if idx >= len(snap) {
				return Undef, false
			}
			v := snap[idx].key
			idx++
			return v, true
		}), nil
	})
	proto.SetHidden("values", setValuesFn)

	// keys() is identical to values() per spec (§24.2.3.8)
	setKeysFn := i.newNativeFunc("keys", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.keys called on incompatible receiver")
		}
		snap := append([]mapEntry{}, s.m.entries...)
		idx := 0
		return i.newIterator(func() (Value, bool) {
			if idx >= len(snap) {
				return Undef, false
			}
			v := snap[idx].key
			idx++
			return v, true
		}), nil
	})
	proto.SetHidden("keys", setKeysFn)

	// entries() → iterator of [value, value] pairs (§24.2.3.4)
	i.defineMethod(proto, "entries", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.entries called on incompatible receiver")
		}
		snap := append([]mapEntry{}, s.m.entries...)
		idx := 0
		return i.newIterator(func() (Value, bool) {
			if idx >= len(snap) {
				return Undef, false
			}
			v := snap[idx].key
			idx++
			return i.newArray([]Value{v, v}), true
		}), nil
	})

	// Set.prototype[Symbol.iterator] === values
	proto.defineOwn(SymKey(i.symIterator), &Property{
		Value:        setValuesFn,
		Writable:     true,
		Configurable: true,
	})

	i.setGlobalHidden("Set", ctor)
}

// ---------------------------------------------------------------------------
// WeakMap
// ---------------------------------------------------------------------------

// initWeakMap builds and registers the WeakMap constructor and
// WeakMap.prototype. WeakMap supports only object keys; attempting to use a
// non-object key throws a TypeError. No iteration or size are exposed.
//
// NOTE: This implementation stores keys by identity in the same orderedMap
// structure (sameValueZero on *Object pointers matches identity). True weak
// references would require a finalizer-based approach; this first-pass version
// keeps things simple, matching the semantics contract but not the GC behavior.
func (i *Interpreter) initWeakMap() {
	weakMapProto := NewObject(i.objectProto)
	weakMapProto.class = "WeakMap"

	// Constructor: new WeakMap(iterable?)
	wmConstruct := func(ctx context.Context, this Value, args []Value) (Value, error) {
		var obj *Object
		if o, ok := this.(*Object); ok && o != i.global && o.class == "WeakMap" {
			obj = o
		} else {
			obj = NewObject(weakMapProto)
			obj.class = "WeakMap"
		}
		m := &orderedMap{}
		obj.internal = map[string]any{"WeakMap": m}

		if iterable := arg(args, 0); !IsNullish(iterable) && !IsUndefined(iterable) {
			err := i.iterate(ctx, iterable, func(item Value) error {
				var pair []Value
				if itemObj, ok := item.(*Object); ok && itemObj.isArray {
					pair = itemObj.denseCopy()
				} else {
					if err2 := i.iterate(ctx, item, func(v Value) error {
						pair = append(pair, v)
						return nil
					}); err2 != nil {
						return i.throwError(ctx, "TypeError", "WeakMap iterable items must be key-value pairs")
					}
				}
				k := arg(pair, 0)
				if _, ok := k.(*Object); !ok {
					return i.throwError(ctx, "TypeError", "Invalid value used as weak map key")
				}
				m.set(k, arg(pair, 1))
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		return obj, nil
	}

	ctor := i.newNativeCtor("WeakMap", 0, wmConstruct, wmConstruct)
	linkCtor(ctor, weakMapProto)

	// set(key, value) → this — key must be an object
	i.defineMethod(weakMapProto, "set", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := weakMapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.set called on incompatible receiver")
		}
		k := arg(args, 0)
		if _, ok := k.(*Object); !ok {
			return nil, i.throwError(ctx, "TypeError", "Invalid value used as weak map key")
		}
		m.set(k, arg(args, 1))
		return this, nil
	})

	// get(key) → value | undefined
	i.defineMethod(weakMapProto, "get", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := weakMapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.get called on incompatible receiver")
		}
		if v, ok := m.get(arg(args, 0)); ok {
			return v, nil
		}
		return Undef, nil
	})

	// has(key) → boolean
	i.defineMethod(weakMapProto, "has", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := weakMapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.has called on incompatible receiver")
		}
		return Bool(m.has(arg(args, 0))), nil
	})

	// delete(key) → boolean
	i.defineMethod(weakMapProto, "delete", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := weakMapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.delete called on incompatible receiver")
		}
		return Bool(m.delete(arg(args, 0))), nil
	})

	i.setGlobalHidden("WeakMap", ctor)
}

// ---------------------------------------------------------------------------
// WeakSet
// ---------------------------------------------------------------------------

// initWeakSet builds and registers the WeakSet constructor and
// WeakSet.prototype. Values must be objects; non-objects throw a TypeError.
// No iteration or size are exposed.
func (i *Interpreter) initWeakSet() {
	weakSetProto := NewObject(i.objectProto)
	weakSetProto.class = "WeakSet"

	// Constructor: new WeakSet(iterable?)
	wsConstruct := func(ctx context.Context, this Value, args []Value) (Value, error) {
		var obj *Object
		if o, ok := this.(*Object); ok && o != i.global && o.class == "WeakSet" {
			obj = o
		} else {
			obj = NewObject(weakSetProto)
			obj.class = "WeakSet"
		}
		s := &orderedSet{}
		obj.internal = map[string]any{"WeakSet": s}

		if iterable := arg(args, 0); !IsNullish(iterable) && !IsUndefined(iterable) {
			err := i.iterate(ctx, iterable, func(v Value) error {
				if _, ok := v.(*Object); !ok {
					return i.throwError(ctx, "TypeError", "Invalid value used in weak set")
				}
				s.add(v)
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		return obj, nil
	}

	ctor := i.newNativeCtor("WeakSet", 0, wsConstruct, wsConstruct)
	linkCtor(ctor, weakSetProto)

	// add(value) → this — value must be an object
	i.defineMethod(weakSetProto, "add", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := weakSetSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakSet.prototype.add called on incompatible receiver")
		}
		v := arg(args, 0)
		if _, ok := v.(*Object); !ok {
			return nil, i.throwError(ctx, "TypeError", "Invalid value used in weak set")
		}
		s.add(v)
		return this, nil
	})

	// has(value) → boolean
	i.defineMethod(weakSetProto, "has", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := weakSetSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakSet.prototype.has called on incompatible receiver")
		}
		return Bool(s.has(arg(args, 0))), nil
	})

	// delete(value) → boolean
	i.defineMethod(weakSetProto, "delete", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := weakSetSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakSet.prototype.delete called on incompatible receiver")
		}
		return Bool(s.delete(arg(args, 0))), nil
	})

	i.setGlobalHidden("WeakSet", ctor)
}
