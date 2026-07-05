package interp

import (
	"context"
	"math"
)

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
	key     Value
	val     Value
	deleted bool // tombstone: the entry was removed but its slot is preserved
}

// orderedMap is a lightweight ordered map backed by a slice. Iteration always
// visits entries in the order they were first inserted. Mutation does not
// change a key's position in the slice; deletion replaces the entry with a
// tombstone (deleted=true) so existing iterators stay index-stable and observe
// spec-mandated semantics (§24.1.3.1: "The existing [[MapData]] List is
// preserved because there may be existing iterators suspended midway"). The
// tombstones are cleared only by clear(), which resets the whole list.
//
// Lookup is O(n) by linear scan while the map is small (matching what V8 /
// SpiderMonkey do for small maps, and avoiding a hash-map allocation for the
// many short-lived collections real code creates). Once a map grows past
// mapIndexThreshold live entries it builds a hash index (see hkey/buildIndex),
// after which get/set/has/delete are O(1) — so a loop of N inserts is O(N)
// instead of O(N²).
type orderedMap struct {
	entries []mapEntry
	count   int          // number of live (non-tombstone) entries
	index   map[hkey]int // live key -> entries index; nil below the threshold
}

// mapIndexThreshold is the live-entry count above which an orderedMap switches
// from linear scan to a hash index. Small maps stay allocation-free.
const mapIndexThreshold = 16

// hkey is a comparable encoding of a Value's SameValueZero (§7.2.11) equivalence
// class, usable as a Go map key. The encoding is faithful: two Values are
// SameValueZero-equal iff their hkeys are ==, so the index resolves a key with a
// single map lookup and no secondary equality check. The kind tag keeps the
// primitive domains disjoint (so Number 1, BigInt 1n, String "1", and Boolean
// true never collide), all NaNs collapse to one class, and -0 folds to +0.
type hkey struct {
	kind uint8
	f    float64 // Number value, or 0/1 for Boolean
	s    string  // String content, or a BigInt's canonical decimal
	ptr  any     // *Object / *Symbol identity (nil for value kinds)
}

const (
	hkUndefined uint8 = iota
	hkNull
	hkBool
	hkNumber
	hkNaN
	hkString
	hkBigInt
	hkSymbol
	hkObject
)

// hashKey maps a Value to its hkey. It canonicalizes NaN (to a single class) and
// -0 (to +0), mirroring canonicalizeKey/sameValueZero, so a lookup key need not
// be pre-canonicalized.
func hashKey(v Value) hkey {
	switch x := v.(type) {
	case Undefined:
		return hkey{kind: hkUndefined}
	case Null:
		return hkey{kind: hkNull}
	case Boolean:
		if bool(x) {
			return hkey{kind: hkBool, f: 1}
		}
		return hkey{kind: hkBool}
	case Number:
		f := float64(x)
		if math.IsNaN(f) {
			return hkey{kind: hkNaN}
		}
		if f == 0 { // fold -0 to +0
			f = 0
		}
		return hkey{kind: hkNumber, f: f}
	case String:
		return hkey{kind: hkString, s: string(x)}
	case *vmString:
		return hkey{kind: hkString, s: x.build()}
	case *BigInt:
		return hkey{kind: hkBigInt, s: x.Int.String()}
	case *Symbol:
		return hkey{kind: hkSymbol, ptr: x}
	case *Object:
		return hkey{kind: hkObject, ptr: x}
	default:
		// Unreachable for spec Values; kind 255 keeps any stray type isolated.
		return hkey{kind: 255}
	}
}

// buildIndex populates the hash index from the live entries. Called once when a
// growing map first crosses mapIndexThreshold; thereafter set/delete keep it in
// step incrementally.
func (m *orderedMap) buildIndex() {
	m.index = make(map[hkey]int, m.count*2)
	for idx := range m.entries {
		if !m.entries[idx].deleted {
			m.index[hashKey(m.entries[idx].key)] = idx
		}
	}
}

// find returns the index of the first live entry whose key is SameValueZero-
// equal to k, or -1 when absent. It uses the hash index once one exists,
// otherwise scans linearly.
func (m *orderedMap) find(k Value) int {
	if m.index != nil {
		if idx, ok := m.index[hashKey(k)]; ok {
			return idx
		}
		return -1
	}
	for idx := range m.entries {
		if !m.entries[idx].deleted && sameValueZero(m.entries[idx].key, k) {
			return idx
		}
	}
	return -1
}

// canonicalizeKey implements CanonicalizeKeyedCollectionKey (§24.5.1): the
// number -0 is normalized to +0 so it is stored and reported as +0.
func canonicalizeKey(v Value) Value {
	if n, ok := v.(Number); ok && float64(n) == 0 {
		return Number(0)
	}
	return v
}

// set inserts or updates the entry for k. Insertion order is preserved:
// existing keys stay in their original position.
func (m *orderedMap) set(k, v Value) {
	k = canonicalizeKey(k)
	if idx := m.find(k); idx >= 0 {
		m.entries[idx].val = v
		return
	}
	idx := len(m.entries)
	m.entries = append(m.entries, mapEntry{key: k, val: v})
	m.count++
	if m.index != nil {
		m.index[hashKey(k)] = idx
	} else if m.count > mapIndexThreshold {
		m.buildIndex()
	}
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

// delete tombstones the entry for k, reporting whether one existed.
func (m *orderedMap) delete(k Value) bool {
	idx := m.find(k)
	if idx < 0 {
		return false
	}
	if m.index != nil {
		delete(m.index, hashKey(m.entries[idx].key))
	}
	m.entries[idx] = mapEntry{deleted: true}
	m.count--
	return true
}

// size returns the number of live entries.
func (m *orderedMap) size() int { return m.count }

// clear removes all entries. The backing list is reset; any live iterators read
// the list length afresh and so observe completion.
func (m *orderedMap) clear() {
	m.entries = nil
	m.count = 0
	m.index = nil
}

// nextLive returns the first live entry at or after idx (skipping tombstones)
// together with the index to resume from; ok is false when none remain. It
// re-reads the backing list on each call, so entries appended after an iterator
// was created are observed (spec CreateMapIterator/CreateSetIterator re-read the
// entry count on every step).
func (m *orderedMap) nextLive(idx int) (mapEntry, int, bool) {
	for idx < len(m.entries) {
		e := m.entries[idx]
		idx++
		if !e.deleted {
			return e, idx, true
		}
	}
	return mapEntry{}, idx, false
}

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
	// Route through orderedMap.set so the hash index (once built) is maintained;
	// re-setting an existing key's sentinel value is a harmless no-op.
	s.m.set(v, setSentinel)
}

// values returns a snapshot slice of the set's live values in insertion order.
func (s *orderedSet) values() []Value {
	out := make([]Value, 0, s.m.count)
	for idx := range s.m.entries {
		if !s.m.entries[idx].deleted {
			out = append(out, s.m.entries[idx].key)
		}
	}
	return out
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
	i.initWeakRef()
	i.initFinalizationRegistry()
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

	// %MapIteratorPrototype% (§24.1.5.2): inherits %Iterator.prototype% and
	// carries @@toStringTag "Map Iterator".
	i.mapIteratorProto = NewObject(i.iteratorProto)
	i.mapIteratorProto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value: String("Map Iterator"), Writable: false, Enumerable: false, Configurable: true,
	})
	i.defineCollectionIteratorNext(i.mapIteratorProto, "MapIterator", "%MapIteratorPrototype%")

	// Constructor: new Map(iterable?). §24.1.1.1
	// The iterable (if supplied) must produce [key, value] pairs.
	//
	// Called without new (NewTarget undefined) → TypeError.
	mapCall := func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor Map requires 'new'")
	}
	mapConstruct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		proto0, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.mapProto })
		if err != nil {
			return nil, err
		}
		obj := NewObject(proto0)
		obj.class = "Map"
		m := &orderedMap{}
		obj.internal = map[string]any{"Map": m}

		// Populate from optional iterable argument via AddEntriesFromIterable.
		if iterable := arg(args, 0); !IsNullish(iterable) {
			// adder = ? Get(map, "set"); require callable.
			adder, err := obj.GetStr(ctx, "set")
			if err != nil {
				return nil, err
			}
			ao, ok := adder.(*Object)
			if !ok || !ao.IsCallable() {
				return nil, i.throwError(ctx, "TypeError", "Map: 'set' is not callable")
			}
			if err := i.addFromIterable(ctx, iterable, func(item Value) error {
				// Each entry must be an Object; read "0" and "1", then call set.
				itemObj, ok := item.(*Object)
				if !ok {
					return i.throwError(ctx, "TypeError", "Map iterable items must be objects")
				}
				k, err := itemObj.GetStr(ctx, "0")
				if err != nil {
					return err
				}
				v, err := itemObj.GetStr(ctx, "1")
				if err != nil {
					return err
				}
				_, e := i.call(ctx, adder, obj, []Value{k, v})
				return e
			}); err != nil {
				return nil, err
			}
		}
		return obj, nil
	}

	ctor := i.newNativeCtor("Map", 0, mapCall, mapConstruct)
	linkCtor(ctor, proto)
	i.defineSpeciesGetter(ctor)

	// Map.groupBy(items, callback) → Map keyed by callback results (§24.1.1.2).
	i.defineMethod(ctor, "groupBy", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		cb, ok := arg(args, 1).(*Object)
		if !ok || !cb.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Map.groupBy callback is not a function")
		}
		groups := &orderedMap{}
		idx := 0
		err := i.iterate(ctx, arg(args, 0), func(v Value) error {
			kv, err := cb.fn.call(ctx, Undef, []Value{v, Number(float64(idx))})
			if err != nil {
				return err
			}
			key := canonicalizeKey(kv)
			if bucket, ok := groups.get(key); ok {
				bucket.(*Object).elems = append(bucket.(*Object).elems, v)
			} else {
				groups.set(key, i.newArray([]Value{v}))
			}
			idx++
			return nil
		})
		if err != nil {
			return nil, err
		}
		out := NewObject(proto)
		out.class = "Map"
		out.internal = map[string]any{"Map": groups}
		return out, nil
	})

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

	// getOrInsert(key, value) → existing value or the inserted value (§24.1.3.x).
	i.defineMethod(proto, "getOrInsert", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.getOrInsert called on incompatible receiver")
		}
		key := arg(args, 0)
		if v, ok := m.get(key); ok {
			return v, nil
		}
		value := arg(args, 1)
		m.set(key, value)
		return value, nil
	})

	// getOrInsertComputed(key, callback) → existing value or the value computed
	// by callback, which is inserted (§24.1.3.x).
	i.defineMethod(proto, "getOrInsertComputed", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.getOrInsertComputed called on incompatible receiver")
		}
		cb, ok := arg(args, 1).(*Object)
		if !ok || !cb.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.getOrInsertComputed callback is not callable")
		}
		key := canonicalizeKey(arg(args, 0))
		if v, ok := m.get(key); ok {
			return v, nil
		}
		value, err := cb.fn.call(ctx, Undef, []Value{key})
		if err != nil {
			return nil, err
		}
		// The callback may have modified the map; re-check before inserting.
		if idx := m.find(key); idx >= 0 {
			m.entries[idx].val = value
		} else {
			m.set(key, value)
		}
		return value, nil
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
		if co, ok := cb.(*Object); !ok || !co.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.forEach callback is not callable")
		}
		cbThis := arg(args, 1)
		// Walk the live backing list by index so entries added during the
		// callback are visited and tombstoned (deleted) entries are skipped.
		idx := 0
		for {
			e, next, ok := m.nextLive(idx)
			if !ok {
				break
			}
			idx = next
			if _, err := i.call(ctx, cb, cbThis, []Value{e.val, e.key, this}); err != nil {
				return nil, err
			}
		}
		return Undef, nil
	})

	// keys() → iterator of keys (live over the backing list; §24.1.5.1)
	mapKeysFn := i.newNativeFunc("keys", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.keys called on incompatible receiver")
		}
		idx, done := 0, false
		return i.newCollectionIterator(i.mapIteratorProto, "Map Iterator", "MapIterator", func() (Value, bool) {
			e, next, ok := m.nextLive(idx)
			if done || !ok {
				done = true
				return Undef, false
			}
			idx = next
			return e.key, true
		}), nil
	})
	proto.SetHidden("keys", mapKeysFn)

	// values() → iterator of values
	mapValuesFn := i.newNativeFunc("values", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.values called on incompatible receiver")
		}
		idx, done := 0, false
		return i.newCollectionIterator(i.mapIteratorProto, "Map Iterator", "MapIterator", func() (Value, bool) {
			e, next, ok := m.nextLive(idx)
			if done || !ok {
				done = true
				return Undef, false
			}
			idx = next
			return e.val, true
		}), nil
	})
	proto.SetHidden("values", mapValuesFn)

	// entries() → iterator of [key, value] pairs
	mapEntriesFn := i.newNativeFunc("entries", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := mapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "Map.prototype.entries called on incompatible receiver")
		}
		idx, done := 0, false
		return i.newCollectionIterator(i.mapIteratorProto, "Map Iterator", "MapIterator", func() (Value, bool) {
			e, next, ok := m.nextLive(idx)
			if done || !ok {
				done = true
				return Undef, false
			}
			idx = next
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

	// Map.prototype[Symbol.toStringTag] = "Map" (§24.1.3.13).
	proto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value:        String("Map"),
		Writable:     false,
		Enumerable:   false,
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

	// %SetIteratorPrototype% (§24.2.5.2): inherits %Iterator.prototype% and
	// carries @@toStringTag "Set Iterator".
	i.setIteratorProto = NewObject(i.iteratorProto)
	i.setIteratorProto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value: String("Set Iterator"), Writable: false, Enumerable: false, Configurable: true,
	})
	i.defineCollectionIteratorNext(i.setIteratorProto, "SetIterator", "%SetIteratorPrototype%")

	// Constructor: new Set(iterable?). §24.2.1.1
	//
	// Called without new (NewTarget undefined) → TypeError.
	setCall := func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor Set requires 'new'")
	}
	setConstruct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		proto0, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.setProto })
		if err != nil {
			return nil, err
		}
		obj := NewObject(proto0)
		obj.class = "Set"
		s := &orderedSet{}
		obj.internal = map[string]any{"Set": s}

		if iterable := arg(args, 0); !IsNullish(iterable) {
			// adder = ? Get(set, "add"); require callable.
			adder, err := obj.GetStr(ctx, "add")
			if err != nil {
				return nil, err
			}
			ao, ok := adder.(*Object)
			if !ok || !ao.IsCallable() {
				return nil, i.throwError(ctx, "TypeError", "Set: 'add' is not callable")
			}
			if err := i.addFromIterable(ctx, iterable, func(v Value) error {
				_, e := i.call(ctx, adder, obj, []Value{v})
				return e
			}); err != nil {
				return nil, err
			}
		}
		return obj, nil
	}

	ctor := i.newNativeCtor("Set", 0, setCall, setConstruct)
	linkCtor(ctor, proto)
	i.defineSpeciesGetter(ctor)

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
		if co, ok := cb.(*Object); !ok || !co.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.forEach callback is not callable")
		}
		cbThis := arg(args, 1)
		// Walk the live backing list by index so values added during the callback
		// are visited and tombstoned values are skipped (§24.2.3.6 re-reads
		// entriesCount each step).
		idx := 0
		for {
			e, next, ok := s.m.nextLive(idx)
			if !ok {
				break
			}
			idx = next
			if _, err := i.call(ctx, cb, cbThis, []Value{e.key, e.key, this}); err != nil {
				return nil, err
			}
		}
		return Undef, nil
	})

	// values() → iterator of values (also aliased as keys()). The iterator reads
	// the live backing list by index, so values added before it is exhausted are
	// visited (§24.2.5.1 CreateSetIterator re-reads entriesCount each step).
	setValuesFn := i.newNativeFunc("values", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.values called on incompatible receiver")
		}
		idx, done := 0, false
		return i.newCollectionIterator(i.setIteratorProto, "Set Iterator", "SetIterator", func() (Value, bool) {
			e, next, ok := s.m.nextLive(idx)
			if done || !ok {
				done = true // once exhausted, the iterator stays done (§24.2.5.1)
				return Undef, false
			}
			idx = next
			return e.key, true
		}), nil
	})
	proto.SetHidden("values", setValuesFn)

	// keys() is the very same function object as values() per spec (§24.2.3.10).
	proto.SetHidden("keys", setValuesFn)

	// entries() → iterator of [value, value] pairs (§24.2.3.4)
	i.defineMethod(proto, "entries", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := setSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "Set.prototype.entries called on incompatible receiver")
		}
		idx, done := 0, false
		return i.newCollectionIterator(i.setIteratorProto, "Set Iterator", "SetIterator", func() (Value, bool) {
			e, next, ok := s.m.nextLive(idx)
			if done || !ok {
				done = true // once exhausted, the iterator stays done (§24.2.5.1)
				return Undef, false
			}
			idx = next
			return i.newArray([]Value{e.key, e.key}), true
		}), nil
	})

	// Set.prototype[Symbol.iterator] === values
	proto.defineOwn(SymKey(i.symIterator), &Property{
		Value:        setValuesFn,
		Writable:     true,
		Configurable: true,
	})

	// ES2025 set-method family. Each takes a set-like `other` and follows the
	// GetSetRecord semantics precisely (§24.2.3).
	i.defineMethod(proto, "union", 1, i.setUnion)
	i.defineMethod(proto, "intersection", 1, i.setIntersection)
	i.defineMethod(proto, "difference", 1, i.setDifference)
	i.defineMethod(proto, "symmetricDifference", 1, i.setSymmetricDifference)
	i.defineMethod(proto, "isSubsetOf", 1, i.setIsSubsetOf)
	i.defineMethod(proto, "isSupersetOf", 1, i.setIsSupersetOf)
	i.defineMethod(proto, "isDisjointFrom", 1, i.setIsDisjointFrom)

	// Set.prototype[Symbol.toStringTag] = "Set" (§24.2.3.13).
	proto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value:        String("Set"),
		Writable:     false,
		Enumerable:   false,
		Configurable: true,
	})

	i.setGlobalHidden("Set", ctor)
}

// ---------------------------------------------------------------------------
// WeakMap
// ---------------------------------------------------------------------------

// initWeakMap builds and registers the WeakMap constructor and
// WeakMap.prototype. Per §24.3, WeakMap keys must satisfy CanBeHeldWeakly
// (§7.3.11): an Object or a non-registered Symbol. set() throws a TypeError for
// any other key; has/get/delete simply report absence (they never throw for an
// unsuitable key, since such a key can never be present).
//
// NOTE: This implementation stores keys by identity in the same orderedMap
// structure (sameValueZero matches object/symbol identity). gojs has no
// GC-finalization hook, so entries are held strongly forever and are never
// reclaimed. That is a conforming implementation choice — a spec-compliant
// engine is permitted to never collect (see wontfix/weak-references-never-cleared).
func (i *Interpreter) initWeakMap() {
	weakMapProto := NewObject(i.objectProto)
	weakMapProto.class = "WeakMap"
	i.weakMapProto = weakMapProto

	// Constructor: new WeakMap(iterable?). §24.3.1.1. Called without new
	// (NewTarget undefined) → TypeError.
	wmCall := func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor WeakMap requires 'new'")
	}
	wmConstruct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		proto0, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.weakMapProto })
		if err != nil {
			return nil, err
		}
		obj := NewObject(proto0)
		obj.class = "WeakMap"
		m := &orderedMap{}
		obj.internal = map[string]any{"WeakMap": m}

		// AddEntriesFromIterable (§24.3.1.1 step 8): read a user-visible adder and
		// invoke it per [key,value] pair so overrides/side effects are observed.
		if iterable := arg(args, 0); !IsNullish(iterable) {
			adder, err := obj.GetStr(ctx, "set")
			if err != nil {
				return nil, err
			}
			ao, ok := adder.(*Object)
			if !ok || !ao.IsCallable() {
				return nil, i.throwError(ctx, "TypeError", "WeakMap: 'set' is not callable")
			}
			if err := i.addFromIterable(ctx, iterable, func(item Value) error {
				itemObj, ok := item.(*Object)
				if !ok {
					return i.throwError(ctx, "TypeError", "WeakMap iterable items must be objects")
				}
				k, err := itemObj.GetStr(ctx, "0")
				if err != nil {
					return err
				}
				v, err := itemObj.GetStr(ctx, "1")
				if err != nil {
					return err
				}
				_, e := i.call(ctx, adder, obj, []Value{k, v})
				return e
			}); err != nil {
				return nil, err
			}
		}
		return obj, nil
	}

	ctor := i.newNativeCtor("WeakMap", 0, wmCall, wmConstruct)
	linkCtor(ctor, weakMapProto)

	// set(key, value) → this — key must be CanBeHeldWeakly.
	i.defineMethod(weakMapProto, "set", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := weakMapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.set called on incompatible receiver")
		}
		k := arg(args, 0)
		if !canBeHeldWeakly(k) {
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
		if !canBeHeldWeakly(arg(args, 0)) {
			return Undef, nil
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
		if !canBeHeldWeakly(arg(args, 0)) {
			return False, nil
		}
		return Bool(m.has(arg(args, 0))), nil
	})

	// delete(key) → boolean
	i.defineMethod(weakMapProto, "delete", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := weakMapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.delete called on incompatible receiver")
		}
		if !canBeHeldWeakly(arg(args, 0)) {
			return False, nil
		}
		return Bool(m.delete(arg(args, 0))), nil
	})

	// getOrInsert(key, value) → existing or inserted value (Upsert proposal).
	i.defineMethod(weakMapProto, "getOrInsert", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := weakMapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.getOrInsert called on incompatible receiver")
		}
		key := arg(args, 0)
		if !canBeHeldWeakly(key) {
			return nil, i.throwError(ctx, "TypeError", "Invalid value used as weak map key")
		}
		if v, ok := m.get(key); ok {
			return v, nil
		}
		value := arg(args, 1)
		m.set(key, value)
		return value, nil
	})

	// getOrInsertComputed(key, callback) → existing value or the value computed
	// by callback (Upsert proposal).
	i.defineMethod(weakMapProto, "getOrInsertComputed", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		m := weakMapSlot(this)
		if m == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.getOrInsertComputed called on incompatible receiver")
		}
		key := arg(args, 0)
		if !canBeHeldWeakly(key) {
			return nil, i.throwError(ctx, "TypeError", "Invalid value used as weak map key")
		}
		cb, ok := arg(args, 1).(*Object)
		if !ok || !cb.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "WeakMap.prototype.getOrInsertComputed callback is not callable")
		}
		if v, ok := m.get(key); ok {
			return v, nil
		}
		value, err := cb.fn.call(ctx, Undef, []Value{key})
		if err != nil {
			return nil, err
		}
		// The callback may have mutated the map; re-check before inserting.
		if idx := m.find(key); idx >= 0 {
			m.entries[idx].val = value
		} else {
			m.set(key, value)
		}
		return value, nil
	})

	// WeakMap.prototype[Symbol.toStringTag] = "WeakMap" (§24.3.3.5).
	weakMapProto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value:        String("WeakMap"),
		Writable:     false,
		Enumerable:   false,
		Configurable: true,
	})

	i.setGlobalHidden("WeakMap", ctor)
}

// ---------------------------------------------------------------------------
// WeakSet
// ---------------------------------------------------------------------------

// initWeakSet builds and registers the WeakSet constructor and
// WeakSet.prototype. Per §24.4, values must satisfy CanBeHeldWeakly (§7.3.11):
// an Object or a non-registered Symbol. add() throws a TypeError otherwise;
// has/delete report absence without throwing. Like WeakMap, entries are never
// reclaimed (gojs has no GC-finalization hook — a conforming choice).
func (i *Interpreter) initWeakSet() {
	weakSetProto := NewObject(i.objectProto)
	weakSetProto.class = "WeakSet"
	i.weakSetProto = weakSetProto

	// Constructor: new WeakSet(iterable?). §24.4.1.1. Called without new
	// (NewTarget undefined) → TypeError.
	wsCall := func(ctx context.Context, this Value, args []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor WeakSet requires 'new'")
	}
	wsConstruct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		proto0, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.weakSetProto })
		if err != nil {
			return nil, err
		}
		obj := NewObject(proto0)
		obj.class = "WeakSet"
		s := &orderedSet{}
		obj.internal = map[string]any{"WeakSet": s}

		// §24.4.1.1 step 8: read a user-visible adder ("add") and invoke it per
		// value so overrides/side effects are observed.
		if iterable := arg(args, 0); !IsNullish(iterable) {
			adder, err := obj.GetStr(ctx, "add")
			if err != nil {
				return nil, err
			}
			ao, ok := adder.(*Object)
			if !ok || !ao.IsCallable() {
				return nil, i.throwError(ctx, "TypeError", "WeakSet: 'add' is not callable")
			}
			if err := i.addFromIterable(ctx, iterable, func(v Value) error {
				_, e := i.call(ctx, adder, obj, []Value{v})
				return e
			}); err != nil {
				return nil, err
			}
		}
		return obj, nil
	}

	ctor := i.newNativeCtor("WeakSet", 0, wsCall, wsConstruct)
	linkCtor(ctor, weakSetProto)

	// add(value) → this — value must be CanBeHeldWeakly.
	i.defineMethod(weakSetProto, "add", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := weakSetSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakSet.prototype.add called on incompatible receiver")
		}
		v := arg(args, 0)
		if !canBeHeldWeakly(v) {
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
		if !canBeHeldWeakly(arg(args, 0)) {
			return False, nil
		}
		return Bool(s.has(arg(args, 0))), nil
	})

	// delete(value) → boolean
	i.defineMethod(weakSetProto, "delete", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s := weakSetSlot(this)
		if s == nil {
			return nil, i.throwError(ctx, "TypeError", "WeakSet.prototype.delete called on incompatible receiver")
		}
		if !canBeHeldWeakly(arg(args, 0)) {
			return False, nil
		}
		return Bool(s.delete(arg(args, 0))), nil
	})

	// WeakSet.prototype[Symbol.toStringTag] = "WeakSet" (§24.4.3.5).
	weakSetProto.defineOwn(SymKey(i.symToStringTag), &Property{
		Value:        String("WeakSet"),
		Writable:     false,
		Enumerable:   false,
		Configurable: true,
	})

	i.setGlobalHidden("WeakSet", ctor)
}
