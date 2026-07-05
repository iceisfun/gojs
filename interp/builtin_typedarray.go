package interp

import (
	"context"
	"encoding/binary"
	"math"
	"math/big"
)

// This file implements the %TypedArray% intrinsic and the 11 concrete
// TypedArray constructors (ECMA-262 §23.2), including the integer-indexed
// exotic behavior that serves canonical numeric index property keys directly
// from a shared ArrayBuffer's bytes. The exotic internal methods are hooked
// into the object core (see object.go / property.go / builtin_reflect.go);
// this file provides the backing data structure, element access, and the
// constructors.

// taKind identifies one of the concrete TypedArray element types.
type taKind uint8

const (
	taInt8 taKind = iota
	taUint8
	taUint8Clamped
	taInt16
	taUint16
	taInt32
	taUint32
	taFloat32
	taFloat64
	taBigInt64
	taBigUint64
)

// taInfo describes a TypedArray element type.
type taInfo struct {
	name    string // constructor name, e.g. "Int8Array"
	size    int    // element size in bytes
	bigInt  bool   // [[ContentType]] is BigInt
	clamped bool   // Uint8ClampedArray
}

// taKinds is indexed by taKind. The order here also defines the iteration order
// used when installing the concrete constructors.
var taKinds = [...]taInfo{
	taInt8:         {"Int8Array", 1, false, false},
	taUint8:        {"Uint8Array", 1, false, false},
	taUint8Clamped: {"Uint8ClampedArray", 1, false, true},
	taInt16:        {"Int16Array", 2, false, false},
	taUint16:       {"Uint16Array", 2, false, false},
	taInt32:        {"Int32Array", 4, false, false},
	taUint32:       {"Uint32Array", 4, false, false},
	taFloat32:      {"Float32Array", 4, false, false},
	taFloat64:      {"Float64Array", 8, false, false},
	taBigInt64:     {"BigInt64Array", 8, true, false},
	taBigUint64:    {"BigUint64Array", 8, true, false},
}

// allTAKinds lists the kinds in installation order.
var allTAKinds = []taKind{
	taInt8, taUint8, taUint8Clamped, taInt16, taUint16, taInt32, taUint32,
	taFloat32, taFloat64, taBigInt64, taBigUint64,
}

// typedArrayData is the backing record for a TypedArray instance, stored in the
// object's typedArray field. It shares the byte block of an ArrayBuffer via the
// [[ViewedArrayBuffer]] slot.
type typedArrayData struct {
	i           *Interpreter // owner, for value coercions on write
	buffer      *Object      // [[ViewedArrayBuffer]] (an ArrayBuffer object)
	abCache     *arrayBufferData
	byteOffset  int // [[ByteOffset]]
	arrayLength int // [[ArrayLength]] (valid when !autoLength)
	kind        taKind
	autoLength  bool // [[ArrayLength]]/[[ByteLength]] is ~auto~ (length-tracking)
}

// ab returns the backing ArrayBuffer record, caching the pointer on first use.
// A view's [[ViewedArrayBuffer]] never changes after construction, and detach/
// resize mutate that same arrayBufferData in place, so the pointer is stable for
// the view's whole life — this replaces a string-keyed map lookup on
// buffer.internal["ArrayBuffer"] on every element access, the dominant cost of
// tight typed-array loops.
func (td *typedArrayData) ab() (*arrayBufferData, bool) {
	if td.abCache != nil {
		return td.abCache, true
	}
	ab, ok := arrayBufferOf(td.buffer)
	if ok {
		td.abCache = ab
	}
	return ab, ok
}

// elemSize returns the element size in bytes.
func (td *typedArrayData) elemSize() int { return taKinds[td.kind].size }

// typedArrayOf returns the TypedArray record for v, or (nil, false).
func typedArrayOf(v Value) (*typedArrayData, bool) {
	o, ok := v.(*Object)
	if !ok || o.typedArray == nil {
		return nil, false
	}
	return o.typedArray, true
}

// outOfBounds reports whether the view exceeds its buffer (modelling
// IsTypedArrayOutOfBounds), and, when in bounds, the current element length.
func (td *typedArrayData) outOfBounds() (bool, int) {
	ab, ok := td.ab()
	if !ok || ab.detached {
		return true, 0
	}
	bufLen := ab.curByteLength()
	size := taKinds[td.kind].size
	if td.autoLength {
		if td.byteOffset > bufLen {
			return true, 0
		}
		return false, (bufLen - td.byteOffset) / size
	}
	if td.byteOffset+td.arrayLength*size > bufLen {
		return true, 0
	}
	return false, td.arrayLength
}

// isFixedLength implements IsTypedArrayFixedLength (§10.4.5.10): a length-
// tracking view, or one backed by a resizable ArrayBuffer, is not fixed-length.
func (td *typedArrayData) isFixedLength() bool {
	if td.autoLength {
		return false
	}
	ab, ok := td.ab()
	if !ok || ab.resizable {
		return false
	}
	return true
}

// length returns the current element length, or 0 when out of bounds.
func (td *typedArrayData) length() int {
	_, n := td.outOfBounds()
	return n
}

// validIndex implements IsValidIntegerIndex (§10.4.5): it returns the integer
// index and true when index is an in-bounds, non-negative integral Number
// (excluding -0) on a non-detached, in-bounds view.
// normalizeIndex maps a Number used as a computed property key to the value its
// canonical index string denotes: ToString(-0) is "0", so a Number -0 indexes
// element 0 (unlike the string key "-0", which validIndex rejects). Every other
// value passes through unchanged.
func normalizeIndex(f float64) float64 {
	if f == 0 { // true for both +0 and -0; the literal 0 is +0
		return 0
	}
	return f
}

func (td *typedArrayData) validIndex(index float64) (int, bool) {
	if math.IsNaN(index) || math.IsInf(index, 0) || index != math.Trunc(index) {
		return 0, false
	}
	if index == 0 && math.Signbit(index) { // -0
		return 0, false
	}
	if index < 0 {
		return 0, false
	}
	oob, length := td.outOfBounds()
	if oob {
		return 0, false
	}
	idx := int(index)
	if idx >= length {
		return 0, false
	}
	return idx, true
}

// getElement reads the element at a valid integer index (TypedArrayGetElement).
func (td *typedArrayData) getElement(idx int) Value {
	ab, _ := td.ab()
	size := taKinds[td.kind].size
	off := td.byteOffset + idx*size
	return taReadElement(td.kind, ab.data[off:off+size])
}

// setElementNum stores a numeric value at a valid integer index.
func (td *typedArrayData) setElementNum(idx int, num float64) {
	ab, _ := td.ab()
	size := taKinds[td.kind].size
	off := td.byteOffset + idx*size
	taWriteNum(td.kind, ab.data[off:off+size], num)
}

// setElementBig stores a BigInt value at a valid integer index.
func (td *typedArrayData) setElementBig(idx int, v *big.Int) {
	ab, _ := td.ab()
	off := td.byteOffset + idx*8
	binary.LittleEndian.PutUint64(ab.data[off:off+8], bigIntToUint64(v))
}

// canonicalNumericIndex implements CanonicalNumericIndexString (§7.1.21): it
// returns the Number for a string that is "-0" or the canonical decimal form of
// some Number, and false otherwise.
func canonicalNumericIndex(s string) (float64, bool) {
	if s == "-0" {
		return math.Copysign(0, -1), true
	}
	n := ToNumber(String(s))
	if NumberToString(n) == s {
		return n, true
	}
	return 0, false
}

// taReadElement interprets len(b)==size bytes (little-endian) as an element.
func taReadElement(kind taKind, b []byte) Value {
	switch kind {
	case taInt8:
		return Number(float64(int8(b[0])))
	case taUint8, taUint8Clamped:
		return Number(float64(b[0]))
	case taInt16:
		return Number(float64(int16(binary.LittleEndian.Uint16(b))))
	case taUint16:
		return Number(float64(binary.LittleEndian.Uint16(b)))
	case taInt32:
		return Number(float64(int32(binary.LittleEndian.Uint32(b))))
	case taUint32:
		return Number(float64(binary.LittleEndian.Uint32(b)))
	case taFloat32:
		return Number(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))))
	case taFloat64:
		return Number(math.Float64frombits(binary.LittleEndian.Uint64(b)))
	case taBigInt64:
		return NewBigInt(int64(binary.LittleEndian.Uint64(b)))
	case taBigUint64:
		return &BigInt{Int: new(big.Int).SetUint64(binary.LittleEndian.Uint64(b))}
	}
	return Undef
}

// taWriteNum writes a numeric value into b (little-endian) per the element type.
func taWriteNum(kind taKind, b []byte, num float64) {
	switch kind {
	case taInt8, taUint8:
		b[0] = byte(ToUint32(num))
	case taUint8Clamped:
		b[0] = toUint8Clamp(num)
	case taInt16, taUint16:
		binary.LittleEndian.PutUint16(b, uint16(ToUint32(num)))
	case taInt32, taUint32:
		binary.LittleEndian.PutUint32(b, ToUint32(num))
	case taFloat32:
		binary.LittleEndian.PutUint32(b, math.Float32bits(float32(num)))
	case taFloat64:
		binary.LittleEndian.PutUint64(b, math.Float64bits(num))
	}
}

// toUint8Clamp implements ToUint8Clamp (§7.1.11): clamp to [0,255] rounding ties
// to even.
func toUint8Clamp(num float64) byte {
	if math.IsNaN(num) || num <= 0 {
		return 0
	}
	if num >= 255 {
		return 255
	}
	return byte(math.RoundToEven(num))
}

// typedArraySetElement implements TypedArraySetElement (§10.4.5): it coerces the
// value (which may run user code and throw) and stores it only when the index is
// valid. It always reports a successful [[Set]].
func (i *Interpreter) typedArraySetElement(ctx context.Context, td *typedArrayData, index float64, value Value) (bool, error) {
	if taKinds[td.kind].bigInt {
		bv, err := i.toBigIntStrict(ctx, value)
		if err != nil {
			return false, err
		}
		if idx, ok := td.validIndex(index); ok {
			td.setElementBig(idx, bv.(*BigInt).Int)
		}
		return true, nil
	}
	num, err := i.ToNumberV(ctx, value)
	if err != nil {
		return false, err
	}
	if idx, ok := td.validIndex(index); ok {
		td.setElementNum(idx, num)
	}
	return true, nil
}

// taValidateElementDescriptor applies the [[DefineOwnProperty]] rules for a
// valid TypedArray index (§10.4.5.3, after IsValidIntegerIndex): reject an
// accessor or any non-{writable,enumerable,configurable} constraint, then write
// the value (if present) through TypedArraySetElement.
func (i *Interpreter) taValidateElementDescriptor(ctx context.Context, o *Object, index float64, desc *Object) (bool, error) {
	if desc.HasOwn(StrKey("configurable")) {
		v, err := desc.GetStr(ctx, "configurable")
		if err != nil {
			return false, err
		}
		if !ToBoolean(v) {
			return false, nil
		}
	}
	if desc.HasOwn(StrKey("enumerable")) {
		v, err := desc.GetStr(ctx, "enumerable")
		if err != nil {
			return false, err
		}
		if !ToBoolean(v) {
			return false, nil
		}
	}
	if desc.HasOwn(StrKey("get")) || desc.HasOwn(StrKey("set")) {
		return false, nil
	}
	if desc.HasOwn(StrKey("writable")) {
		v, err := desc.GetStr(ctx, "writable")
		if err != nil {
			return false, err
		}
		if !ToBoolean(v) {
			return false, nil
		}
	}
	if desc.HasOwn(StrKey("value")) {
		val, err := desc.GetStr(ctx, "value")
		if err != nil {
			return false, err
		}
		if _, err := i.typedArraySetElement(ctx, o.typedArray, index, val); err != nil {
			return false, err
		}
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

// allocateTypedArray creates a bare TypedArray object of the given kind with the
// given prototype, with no viewed buffer yet.
func (i *Interpreter) allocateTypedArray(kind taKind, proto *Object) *Object {
	obj := NewObject(proto)
	obj.class = taKinds[kind].name
	obj.typedArray = &typedArrayData{i: i, kind: kind}
	return obj
}

// allocateTypedArrayBuffer implements AllocateTypedArrayBuffer (§23.2.5.1.6): it
// allocates a fresh zeroed ArrayBuffer of length elements and associates it.
func (i *Interpreter) allocateTypedArrayBuffer(ctx context.Context, o *Object, length int) error {
	td := o.typedArray
	byteLength := taKinds[td.kind].size * length
	bufV, err := i.allocateArrayBuffer(ctx, i.arrayBufferCtor, byteLength, -1, false)
	if err != nil {
		return err
	}
	td.buffer = bufV.(*Object)
	td.byteOffset = 0
	td.arrayLength = length
	td.autoLength = false
	return nil
}

// typedArrayConstruct implements the concrete TypedArray constructor body
// (§23.2.5.1) for the given kind.
func (i *Interpreter) typedArrayConstruct(ctx context.Context, kind taKind, newTarget Value, args []Value) (Value, error) {
	if IsUndefined(newTarget) || newTarget == nil {
		return nil, i.throwError(ctx, "TypeError", "Constructor "+taKinds[kind].name+" requires 'new'")
	}
	first := arg(args, 0)
	fo, isObj := first.(*Object)
	if !isObj {
		// new T(length): a non-object first argument is a length. Per
		// %TypedArray% ( ...args ) the element length is computed with ToIndex
		// *before* AllocateTypedArray reaches GetPrototypeFromConstructor, so a
		// throwing ToIndex (e.g. a Symbol argument) must not evaluate the
		// newTarget "prototype" getter.
		length, err := i.toIndex(ctx, first)
		if err != nil {
			return nil, err
		}
		proto, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.typedArrayKindProtos[kind] })
		if err != nil {
			return nil, err
		}
		o := i.allocateTypedArray(kind, proto)
		if err := i.allocateTypedArrayBuffer(ctx, o, length); err != nil {
			return nil, err
		}
		return o, nil
	}

	proto, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.typedArrayKindProtos[kind] })
	if err != nil {
		return nil, err
	}
	o := i.allocateTypedArray(kind, proto)

	switch {
	case fo.typedArray != nil:
		if err := i.initTAFromTypedArray(ctx, o, fo); err != nil {
			return nil, err
		}
	case func() bool { _, ok := arrayBufferOf(fo); return ok }():
		if err := i.initTAFromArrayBuffer(ctx, o, fo, arg(args, 1), arg(args, 2)); err != nil {
			return nil, err
		}
	default:
		iterFn, err := i.getMethod(ctx, fo, i.symIterator)
		if err != nil {
			return nil, err
		}
		if iterFn != nil {
			var values []Value
			if err := i.iterate(ctx, fo, func(v Value) error {
				values = append(values, v)
				return nil
			}); err != nil {
				return nil, err
			}
			if err := i.initTAFromList(ctx, o, values); err != nil {
				return nil, err
			}
		} else {
			if err := i.initTAFromArrayLike(ctx, o, fo); err != nil {
				return nil, err
			}
		}
	}
	return o, nil
}

// initTAFromList implements InitializeTypedArrayFromList (§23.2.5.1.3).
func (i *Interpreter) initTAFromList(ctx context.Context, o *Object, values []Value) error {
	if err := i.allocateTypedArrayBuffer(ctx, o, len(values)); err != nil {
		return err
	}
	for k, v := range values {
		if _, err := i.typedArraySetElement(ctx, o.typedArray, float64(k), v); err != nil {
			return err
		}
	}
	return nil
}

// initTAFromArrayLike implements InitializeTypedArrayFromArrayLike (§23.2.5.1.5).
func (i *Interpreter) initTAFromArrayLike(ctx context.Context, o, arrayLike *Object) error {
	length, err := i.lengthOfArrayLike(ctx, arrayLike)
	if err != nil {
		return err
	}
	if err := i.allocateTypedArrayBuffer(ctx, o, length); err != nil {
		return err
	}
	for k := 0; k < length; k++ {
		v, err := arrayLike.GetStr(ctx, intToStr(k))
		if err != nil {
			return err
		}
		if _, err := i.typedArraySetElement(ctx, o.typedArray, float64(k), v); err != nil {
			return err
		}
	}
	return nil
}

// initTAFromTypedArray implements InitializeTypedArrayFromTypedArray (§23.2.5.1.2).
func (i *Interpreter) initTAFromTypedArray(ctx context.Context, o, src *Object) error {
	std := src.typedArray
	dtd := o.typedArray
	oob, srcLen := std.outOfBounds()
	if oob {
		return i.throwError(ctx, "TypeError", "Cannot construct a TypedArray from an out-of-bounds source")
	}
	if taKinds[std.kind].bigInt != taKinds[dtd.kind].bigInt {
		return i.throwError(ctx, "TypeError", "Cannot mix BigInt and non-BigInt TypedArrays")
	}
	byteLength := taKinds[dtd.kind].size * srcLen
	bufV, err := i.allocateArrayBuffer(ctx, i.arrayBufferCtor, byteLength, -1, false)
	if err != nil {
		return err
	}
	buf := bufV.(*Object)
	dtd.buffer = buf
	dtd.byteOffset = 0
	dtd.arrayLength = srcLen
	dtd.autoLength = false
	// Copy element by element with conversion (which is a no-op when the element
	// types match, but preserves value semantics across kinds).
	for k := 0; k < srcLen; k++ {
		v := std.getElement(k)
		if taKinds[dtd.kind].bigInt {
			dtd.setElementBig(k, v.(*BigInt).Int)
		} else {
			dtd.setElementNum(k, ToNumber(v))
		}
	}
	return nil
}

// initTAFromArrayBuffer implements InitializeTypedArrayFromArrayBuffer
// (§23.2.5.1.1).
func (i *Interpreter) initTAFromArrayBuffer(ctx context.Context, o, buffer *Object, byteOffset, length Value) error {
	td := o.typedArray
	elementSize := taKinds[td.kind].size
	offset, err := i.toIndex(ctx, byteOffset)
	if err != nil {
		return err
	}
	if offset%elementSize != 0 {
		return i.throwError(ctx, "RangeError", "start offset of "+taKinds[td.kind].name+" is not aligned to element size")
	}
	ab, _ := arrayBufferOf(buffer)
	bufferIsFixedLength := !ab.resizable
	var newLength int
	hasLength := !IsUndefined(length)
	if hasLength {
		newLength, err = i.toIndex(ctx, length)
		if err != nil {
			return err
		}
	}
	if ab.detached {
		return i.throwError(ctx, "TypeError", "Cannot construct a TypedArray on a detached ArrayBuffer")
	}
	bufferByteLength := ab.curByteLength()
	if !hasLength && !bufferIsFixedLength {
		if offset > bufferByteLength {
			return i.throwError(ctx, "RangeError", "start offset is outside the bounds of the buffer")
		}
		td.buffer = buffer
		td.byteOffset = offset
		td.autoLength = true
		td.arrayLength = 0
		return nil
	}
	var newByteLength int
	if !hasLength {
		if bufferByteLength%elementSize != 0 {
			return i.throwError(ctx, "RangeError", "buffer length for "+taKinds[td.kind].name+" should be a multiple of "+intToStr(elementSize))
		}
		newByteLength = bufferByteLength - offset
		if newByteLength < 0 {
			return i.throwError(ctx, "RangeError", "start offset is outside the bounds of the buffer")
		}
	} else {
		newByteLength = newLength * elementSize
		if offset+newByteLength > bufferByteLength {
			return i.throwError(ctx, "RangeError", "invalid "+taKinds[td.kind].name+" length")
		}
	}
	td.buffer = buffer
	td.byteOffset = offset
	td.arrayLength = newByteLength / elementSize
	td.autoLength = false
	return nil
}

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

// initTypedArray installs the %TypedArray% intrinsic, its prototype, and the 11
// concrete TypedArray constructors.
func (i *Interpreter) initTypedArray() {
	i.typedArrayKindProtos = make(map[taKind]*Object)
	i.typedArrayKindCtors = make(map[taKind]*Object)

	proto := i.typedArrayProto

	// The abstract %TypedArray% constructor throws when invoked directly.
	abstractCall := func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Abstract class TypedArray not directly constructable")
	}
	abstractConstruct := func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Abstract class TypedArray not directly constructable")
	}
	taCtor := i.newNativeCtor("TypedArray", 0, abstractCall, abstractConstruct)
	i.typedArrayCtor = taCtor
	linkCtor(taCtor, proto)
	i.defineSpeciesGetter(taCtor)

	i.initTypedArrayProto()
	i.defineMethod(taCtor, "from", 1, i.typedArrayFrom)
	i.defineMethod(taCtor, "of", 0, i.typedArrayOf)

	// The concrete constructors: each is a subclass of %TypedArray% with its own
	// prototype (inheriting %TypedArray.prototype%) and BYTES_PER_ELEMENT.
	for _, kind := range allTAKinds {
		kind := kind
		info := taKinds[kind]
		kproto := NewObject(proto)
		i.typedArrayKindProtos[kind] = kproto

		call := func(ctx context.Context, _ Value, _ []Value) (Value, error) {
			return nil, i.throwError(ctx, "TypeError", "Constructor "+info.name+" requires 'new'")
		}
		construct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
			return i.typedArrayConstruct(ctx, kind, newTarget, args)
		}
		ctor := i.newNativeCtor(info.name, 3, call, construct)
		ctor.proto = taCtor // [[Prototype]] is %TypedArray%
		i.typedArrayKindCtors[kind] = ctor
		linkCtor(ctor, kproto)

		bpe := Number(float64(info.size))
		ctor.defineOwn(StrKey("BYTES_PER_ELEMENT"), &Property{Value: bpe, Writable: false, Enumerable: false, Configurable: false})
		kproto.defineOwn(StrKey("BYTES_PER_ELEMENT"), &Property{Value: bpe, Writable: false, Enumerable: false, Configurable: false})

		i.setGlobalHidden(info.name, ctor)
	}

	// Uint8Array-only base64/hex conversion methods (the base64 proposal).
	i.initUint8Base64()
}

// ---------------------------------------------------------------------------
// %TypedArray%.from / of
// ---------------------------------------------------------------------------

// thisTAConstructor validates that `this` is a TypedArray constructor and
// returns it.
func (i *Interpreter) thisTAConstructor(ctx context.Context, this Value) (*Object, error) {
	c, ok := this.(*Object)
	if !ok || !c.IsConstructor() {
		return nil, i.throwError(ctx, "TypeError", "TypedArray constructor requires a constructor this value")
	}
	return c, nil
}

// typedArrayCreate constructs a TypedArray via the given constructor with the
// given arguments and validates the result (§23.2.4.1, TypedArrayCreate).
func (i *Interpreter) typedArrayCreateFromCtor(ctx context.Context, ctor *Object, args []Value) (*Object, error) {
	v, err := ctor.fn.construct(ctx, ctor, args)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*Object)
	if !ok || o.typedArray == nil {
		return nil, i.throwError(ctx, "TypeError", "TypedArray constructor did not return a TypedArray")
	}
	oob, length := o.typedArray.outOfBounds()
	if oob {
		return nil, i.throwError(ctx, "TypeError", "TypedArray constructor returned an out-of-bounds array")
	}
	// TypedArrayCreate (§23.2.4.1): when the argument list is a single Number,
	// the result must be at least that long.
	if len(args) == 1 {
		if n, ok := args[0].(Number); ok && float64(length) < float64(n) {
			return nil, i.throwError(ctx, "TypeError", "TypedArray species constructor produced an array that is too small")
		}
	}
	return o, nil
}

// typedArrayFrom implements %TypedArray%.from (§23.2.2.1).
func (i *Interpreter) typedArrayFrom(ctx context.Context, this Value, args []Value) (Value, error) {
	ctor, err := i.thisTAConstructor(ctx, this)
	if err != nil {
		return nil, err
	}
	source := arg(args, 0)
	mapfn := arg(args, 1)
	var mapper *Object
	if !IsUndefined(mapfn) {
		mo, ok := mapfn.(*Object)
		if !ok || !mo.IsCallable() {
			return nil, i.throwError(ctx, "TypeError", "TypedArray.from: mapfn is not callable")
		}
		mapper = mo
	}
	thisArg := arg(args, 2)

	// If source has an iterator, collect its values, then create.
	iterFn, err := i.getMethod(ctx, source, i.symIterator)
	if err != nil {
		return nil, err
	}
	var values []Value
	if iterFn != nil {
		if err := i.iterate(ctx, source, func(v Value) error {
			values = append(values, v)
			return nil
		}); err != nil {
			return nil, err
		}
	} else {
		// Not iterable: treat as array-like. ToObject boxes a primitive (e.g. a
		// String becomes an indexable wrapper) and throws for null/undefined.
		so, err := i.ToObject(ctx, source)
		if err != nil {
			return nil, err
		}
		length, err := i.lengthOfArrayLike(ctx, so)
		if err != nil {
			return nil, err
		}
		for k := 0; k < length; k++ {
			v, err := so.GetStr(ctx, intToStr(k))
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		}
	}

	target, err := i.typedArrayCreateFromCtor(ctx, ctor, []Value{Number(float64(len(values)))})
	if err != nil {
		return nil, err
	}
	for k, v := range values {
		mapped := v
		if mapper != nil {
			mapped, err = mapper.fn.call(ctx, thisArg, []Value{v, Number(float64(k))})
			if err != nil {
				return nil, err
			}
		}
		if _, err := i.typedArraySetElement(ctx, target.typedArray, float64(k), mapped); err != nil {
			return nil, err
		}
	}
	return target, nil
}

// typedArrayOf implements %TypedArray%.of (§23.2.2.2).
func (i *Interpreter) typedArrayOf(ctx context.Context, this Value, args []Value) (Value, error) {
	ctor, err := i.thisTAConstructor(ctx, this)
	if err != nil {
		return nil, err
	}
	target, err := i.typedArrayCreateFromCtor(ctx, ctor, []Value{Number(float64(len(args)))})
	if err != nil {
		return nil, err
	}
	for k, v := range args {
		if _, err := i.typedArraySetElement(ctx, target.typedArray, float64(k), v); err != nil {
			return nil, err
		}
	}
	return target, nil
}
