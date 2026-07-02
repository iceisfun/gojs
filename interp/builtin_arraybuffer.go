package interp

import (
	"context"
	"math"
)

// maxSafeInteger is 2**53 - 1, the ToIndex upper bound (§7.1.22).
const maxSafeInteger = 9007199254740991

// maxByteDataBlock caps the size of a Data Block we will actually allocate.
// CreateByteDataBlock (§6.2.9.1) throws a RangeError when a block cannot be
// created; we refuse implausibly large requests up front rather than let the Go
// runtime panic on an out-of-range make.
const maxByteDataBlock = 0x7FFFFFFF // 2 GiB - 1

// createByteDataBlock implements CreateByteDataBlock (§6.2.9.1): a zeroed byte
// block of the given size, or a RangeError when the size is too large.
func (i *Interpreter) createByteDataBlock(ctx context.Context, size int) ([]byte, error) {
	if size < 0 || size > maxByteDataBlock {
		return nil, i.throwError(ctx, "RangeError", "ArrayBuffer allocation failed: requested length is too large")
	}
	return make([]byte, size), nil
}

// arrayBufferData is the backing store for an ArrayBuffer instance. It is kept
// in the object's internal slot map under key "ArrayBuffer" so that a future
// TypedArray implementation can share the same bytes.
type arrayBufferData struct {
	// data is the byte block ([[ArrayBufferData]]); nil once detached.
	data []byte
	// detached mirrors [[ArrayBufferData]] being null.
	detached bool
	// resizable is true when the buffer was allocated with a maxByteLength
	// (it then has an [[ArrayBufferMaxByteLength]] internal slot).
	resizable bool
	// maxByteLength is [[ArrayBufferMaxByteLength]] for resizable buffers.
	maxByteLength int
}

// arrayBufferOf returns the ArrayBuffer backing data for v, or (nil, false) when
// v is not an ArrayBuffer instance.
func arrayBufferOf(v Value) (*arrayBufferData, bool) {
	o, ok := v.(*Object)
	if !ok || o.internal == nil {
		return nil, false
	}
	ab, ok := o.internal["ArrayBuffer"].(*arrayBufferData)
	return ab, ok
}

// toIndex implements ToIndex (§7.1.22): undefined→0, else ToIntegerOrInfinity
// with RangeError for negatives or values above 2**53-1.
func (i *Interpreter) toIndex(ctx context.Context, v Value) (int, error) {
	if IsUndefined(v) {
		return 0, nil
	}
	n, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, err
	}
	integer := ToInteger(n) // NaN→0, keeps ±Inf
	if integer < 0 {
		return 0, i.throwError(ctx, "RangeError", "invalid index: must not be negative")
	}
	if integer > maxSafeInteger {
		return 0, i.throwError(ctx, "RangeError", "invalid index: exceeds 2**53 - 1")
	}
	return int(integer), nil
}

// protoFromCtor implements GetPrototypeFromConstructor (§10.1.13) for native
// constructors: it reads newTarget.prototype and falls back to def when that is
// not an object.
func (i *Interpreter) protoFromCtor(ctx context.Context, newTarget Value, def *Object) (*Object, error) {
	nt, ok := newTarget.(*Object)
	if !ok {
		return def, nil
	}
	pv, err := nt.GetStr(ctx, "prototype")
	if err != nil {
		return nil, err
	}
	if po, ok := pv.(*Object); ok {
		return po, nil
	}
	return def, nil
}

// getMaxByteLengthOption implements GetArrayBufferMaxByteLengthOption
// (§25.1.3.7): reads options.maxByteLength when options is an object, returning
// (-1, false) for "empty".
func (i *Interpreter) getMaxByteLengthOption(ctx context.Context, options Value) (int, bool, error) {
	o, ok := options.(*Object)
	if !ok {
		return -1, false, nil
	}
	mv, err := o.GetStr(ctx, "maxByteLength")
	if err != nil {
		return -1, false, err
	}
	if IsUndefined(mv) {
		return -1, false, nil
	}
	idx, err := i.toIndex(ctx, mv)
	if err != nil {
		return -1, false, err
	}
	return idx, true, nil
}

// allocateArrayBuffer implements AllocateArrayBuffer (§25.1.3.1). maxByteLength
// is -1 (with hasMax=false) for a fixed-length buffer.
func (i *Interpreter) allocateArrayBuffer(ctx context.Context, newTarget Value, byteLength, maxByteLength int, hasMax bool) (Value, error) {
	if hasMax && byteLength > maxByteLength {
		return nil, i.throwError(ctx, "RangeError", "ArrayBuffer: byteLength exceeds maxByteLength")
	}
	// For a resizable buffer the spec also requires that a Data Block of
	// maxByteLength bytes be creatable (§25.1.3.1 step 6.a).
	if hasMax && maxByteLength > maxByteDataBlock {
		return nil, i.throwError(ctx, "RangeError", "ArrayBuffer: maxByteLength is too large")
	}
	proto, err := i.protoFromCtor(ctx, newTarget, i.arrayBufferProto)
	if err != nil {
		return nil, err
	}
	block, err := i.createByteDataBlock(ctx, byteLength)
	if err != nil {
		return nil, err
	}
	obj := NewObject(proto)
	obj.class = "ArrayBuffer"
	ab := &arrayBufferData{data: block}
	if hasMax {
		ab.resizable = true
		ab.maxByteLength = maxByteLength
	}
	obj.internal = map[string]any{"ArrayBuffer": ab}
	return obj, nil
}

// initArrayBuffer installs the ArrayBuffer constructor and prototype.
func (i *Interpreter) initArrayBuffer() {
	proto := i.arrayBufferProto

	construct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		if IsUndefined(newTarget) || newTarget == nil {
			return nil, i.throwError(ctx, "TypeError", "Constructor ArrayBuffer requires 'new'")
		}
		byteLength, err := i.toIndex(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		maxByteLength, hasMax, err := i.getMaxByteLengthOption(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		return i.allocateArrayBuffer(ctx, newTarget, byteLength, maxByteLength, hasMax)
	}
	callFn := func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor ArrayBuffer requires 'new'")
	}
	ctor := i.newNativeCtor("ArrayBuffer", 1, callFn, construct)
	i.arrayBufferCtor = ctor
	linkCtor(ctor, proto)
	i.defineSpeciesGetter(ctor)

	// ArrayBuffer.isView(arg) — §25.1.5.1.
	i.defineMethod(ctor, "isView", 1, func(ctx context.Context, _ Value, args []Value) (Value, error) {
		o, ok := arg(args, 0).(*Object)
		if !ok {
			return Boolean(false), nil
		}
		// A view has a [[ViewedArrayBuffer]] internal slot: a TypedArray or a
		// DataView.
		if o.typedArray != nil {
			return Boolean(true), nil
		}
		if o.internal != nil {
			if _, ok := o.internal["DataView"].(*dataViewData); ok {
				return Boolean(true), nil
			}
		}
		return Boolean(false), nil
	})

	// get ArrayBuffer.prototype.byteLength — §25.1.6.1.
	i.defineGetter(proto, "byteLength", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		ab, ok := arrayBufferOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "get ArrayBuffer.prototype.byteLength called on incompatible receiver")
		}
		if ab.detached {
			return Number(0), nil
		}
		return Number(float64(len(ab.data))), nil
	})

	// get ArrayBuffer.prototype.detached — §25.1.6.3.
	i.defineGetter(proto, "detached", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		ab, ok := arrayBufferOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "get ArrayBuffer.prototype.detached called on incompatible receiver")
		}
		return Boolean(ab.detached), nil
	})

	// get ArrayBuffer.prototype.maxByteLength — §25.1.6.4.
	i.defineGetter(proto, "maxByteLength", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		ab, ok := arrayBufferOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "get ArrayBuffer.prototype.maxByteLength called on incompatible receiver")
		}
		if ab.detached {
			return Number(0), nil
		}
		if ab.resizable {
			return Number(float64(ab.maxByteLength)), nil
		}
		return Number(float64(len(ab.data))), nil
	})

	// get ArrayBuffer.prototype.resizable — §25.1.6.5.
	i.defineGetter(proto, "resizable", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		ab, ok := arrayBufferOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "get ArrayBuffer.prototype.resizable called on incompatible receiver")
		}
		return Boolean(ab.resizable), nil
	})

	// ArrayBuffer.prototype.resize(newLength) — §25.1.6.6.
	i.defineMethod(proto, "resize", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ab, ok := arrayBufferOf(this)
		if !ok || !ab.resizable {
			return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.resize called on non-resizable ArrayBuffer")
		}
		newByteLength, err := i.toIndex(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		if ab.detached {
			return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.resize called on detached ArrayBuffer")
		}
		if newByteLength > ab.maxByteLength {
			return nil, i.throwError(ctx, "RangeError", "ArrayBuffer.prototype.resize: newLength exceeds maxByteLength")
		}
		newBlock, err := i.createByteDataBlock(ctx, newByteLength)
		if err != nil {
			return nil, err
		}
		copy(newBlock, ab.data)
		ab.data = newBlock
		return Undef, nil
	})

	// ArrayBuffer.prototype.slice(start, end) — §25.1.6.7.
	i.defineMethod(proto, "slice", 2, i.arrayBufferSlice)

	// ArrayBuffer.prototype.transfer([newLength]) — §25.1.6.8.
	i.defineMethod(proto, "transfer", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.arrayBufferCopyAndDetach(ctx, this, arg(args, 0), true)
	})
	// ArrayBuffer.prototype.transferToFixedLength([newLength]) — §25.1.6.9.
	i.defineMethod(proto, "transferToFixedLength", 0, func(ctx context.Context, this Value, args []Value) (Value, error) {
		return i.arrayBufferCopyAndDetach(ctx, this, arg(args, 0), false)
	})

	// ArrayBuffer.prototype[Symbol.toStringTag] = "ArrayBuffer" — §25.1.6.10.
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("ArrayBuffer"), Writable: false, Enumerable: false, Configurable: true})

	i.setGlobalHidden("ArrayBuffer", ctor)
}

// arrayBufferSlice implements ArrayBuffer.prototype.slice (§25.1.6.7).
func (i *Interpreter) arrayBufferSlice(ctx context.Context, this Value, args []Value) (Value, error) {
	ab, ok := arrayBufferOf(this)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.slice called on incompatible receiver")
	}
	if ab.detached {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.slice called on detached ArrayBuffer")
	}
	length := len(ab.data)

	first, err := i.relativeIndex(ctx, arg(args, 0), length, 0)
	if err != nil {
		return nil, err
	}
	final := length
	if end := arg(args, 1); !IsUndefined(end) {
		final, err = i.relativeIndex(ctx, end, length, length)
		if err != nil {
			return nil, err
		}
	}
	newLength := final - first
	if newLength < 0 {
		newLength = 0
	}

	obj, ok := this.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.slice called on incompatible receiver")
	}
	ctor, err := i.speciesConstructor(ctx, obj, i.arrayBufferCtor)
	if err != nil {
		return nil, err
	}
	newV, err := ctor.fn.construct(ctx, ctor, []Value{Number(float64(newLength))})
	if err != nil {
		return nil, err
	}
	newAB, ok := arrayBufferOf(newV)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.slice: species constructor did not return an ArrayBuffer")
	}
	if newAB.detached {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.slice: species constructor returned a detached ArrayBuffer")
	}
	if newV == this {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.slice: species constructor returned the same ArrayBuffer")
	}
	if len(newAB.data) < newLength {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.slice: species constructor returned an ArrayBuffer that is too small")
	}
	// Side effects above may have detached or resized the source buffer.
	if ab.detached {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.slice: source ArrayBuffer became detached")
	}
	currentLength := len(ab.data)
	if first < currentLength {
		count := newLength
		if avail := currentLength - first; avail < count {
			count = avail
		}
		copy(newAB.data, ab.data[first:first+count])
	}
	return newV, nil
}

// relativeIndex resolves a spec "relative index" (ToIntegerOrInfinity clamped to
// [0, length]) as used by slice's start/end arguments.
func (i *Interpreter) relativeIndex(ctx context.Context, v Value, length, def int) (int, error) {
	if IsUndefined(v) {
		return def, nil
	}
	n, err := i.ToNumberV(ctx, v)
	if err != nil {
		return 0, err
	}
	rel := ToInteger(n)
	switch {
	case math.IsInf(rel, -1):
		return 0, nil
	case rel < 0:
		if r := float64(length) + rel; r > 0 {
			return int(r), nil
		}
		return 0, nil
	case math.IsInf(rel, 1) || rel > float64(length):
		return length, nil
	default:
		return int(rel), nil
	}
}

// arrayBufferCopyAndDetach implements ArrayBufferCopyAndDetach (§25.1.3.6),
// backing ArrayBuffer.prototype.transfer / transferToFixedLength.
func (i *Interpreter) arrayBufferCopyAndDetach(ctx context.Context, this Value, newLength Value, preserveResizability bool) (Value, error) {
	ab, ok := arrayBufferOf(this)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.transfer called on incompatible receiver")
	}
	var newByteLength int
	if IsUndefined(newLength) {
		newByteLength = len(ab.data)
	} else {
		var err error
		newByteLength, err = i.toIndex(ctx, newLength)
		if err != nil {
			return nil, err
		}
	}
	if ab.detached {
		return nil, i.throwError(ctx, "TypeError", "ArrayBuffer.prototype.transfer called on detached ArrayBuffer")
	}
	newMax, hasMax := -1, false
	if preserveResizability && ab.resizable {
		newMax, hasMax = ab.maxByteLength, true
	}
	newV, err := i.allocateArrayBuffer(ctx, i.arrayBufferCtor, newByteLength, newMax, hasMax)
	if err != nil {
		return nil, err
	}
	newAB, _ := arrayBufferOf(newV)
	copyLength := newByteLength
	if l := len(ab.data); l < copyLength {
		copyLength = l
	}
	copy(newAB.data, ab.data[:copyLength])
	// DetachArrayBuffer.
	ab.data = nil
	ab.detached = true
	return newV, nil
}
