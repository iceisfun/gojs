package interp

import "context"

// allocateSharedArrayBuffer implements AllocateSharedArrayBuffer (§25.2.2.1).
// maxByteLength is -1 (hasMax=false) for a fixed-length shared buffer.
//
// A SharedArrayBuffer's bytes may be aliased by TypedArray/DataView views in
// other agents, so — unlike a resizable ArrayBuffer — a growable one must never
// reallocate on grow. We therefore pre-allocate maxByteLength bytes up front
// (len(data) == maxByteLength) and track the live byte length in ab.byteLen,
// which curByteLength surfaces to the view/bounds machinery. A fixed-length
// shared buffer allocates exactly byteLength bytes like an ordinary buffer.
func (i *Interpreter) allocateSharedArrayBuffer(ctx context.Context, newTarget Value, byteLength, maxByteLength int, hasMax bool) (Value, error) {
	if hasMax && byteLength > maxByteLength {
		return nil, i.throwError(ctx, "RangeError", "SharedArrayBuffer: byteLength exceeds maxByteLength")
	}
	if hasMax && maxByteLength > maxByteDataBlock {
		return nil, i.throwError(ctx, "RangeError", "SharedArrayBuffer: maxByteLength is too large")
	}
	proto, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.sharedArrayBufferProto })
	if err != nil {
		return nil, err
	}
	// For a growable buffer the whole maxByteLength block must be creatable
	// (§25.2.2.1 step 5): allocate it now so grow never reallocates.
	blockLen := byteLength
	if hasMax {
		blockLen = maxByteLength
	}
	block, err := i.createByteDataBlock(ctx, blockLen)
	if err != nil {
		return nil, err
	}
	obj := NewObject(proto)
	obj.class = "SharedArrayBuffer"
	ab := &arrayBufferData{data: block, shared: true, byteLen: byteLength}
	if hasMax {
		ab.resizable = true
		ab.maxByteLength = maxByteLength
	}
	obj.internal = map[string]any{"ArrayBuffer": ab}
	return obj, nil
}

// initSharedArrayBuffer installs the SharedArrayBuffer constructor and prototype
// (§25.2). The backing store shares the "ArrayBuffer" internal slot with an
// ordinary ArrayBuffer (distinguished by arrayBufferData.shared), so TypedArray,
// DataView and Atomics view a shared buffer transparently.
func (i *Interpreter) initSharedArrayBuffer() {
	proto := i.sharedArrayBufferProto

	construct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		if IsUndefined(newTarget) || newTarget == nil {
			return nil, i.throwError(ctx, "TypeError", "Constructor SharedArrayBuffer requires 'new'")
		}
		byteLength, err := i.toIndex(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		maxByteLength, hasMax, err := i.getMaxByteLengthOption(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		return i.allocateSharedArrayBuffer(ctx, newTarget, byteLength, maxByteLength, hasMax)
	}
	callFn := func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor SharedArrayBuffer requires 'new'")
	}
	ctor := i.newNativeCtor("SharedArrayBuffer", 1, callFn, construct)
	i.sharedArrayBufferCtor = ctor
	linkCtor(ctor, proto)
	// get SharedArrayBuffer[Symbol.species] — §25.2.3.2.
	i.defineSpeciesGetter(ctor)

	// sharedOf returns the shared backing store for this, or a TypeError if the
	// receiver is not a SharedArrayBuffer.
	sharedOf := func(ctx context.Context, this Value, method string) (*arrayBufferData, error) {
		ab, ok := arrayBufferOf(this)
		if !ok || !ab.shared {
			return nil, i.throwError(ctx, "TypeError", method+" called on incompatible receiver")
		}
		return ab, nil
	}

	// get SharedArrayBuffer.prototype.byteLength — §25.2.4.1.
	i.defineGetter(proto, "byteLength", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		ab, err := sharedOf(ctx, this, "get SharedArrayBuffer.prototype.byteLength")
		if err != nil {
			return nil, err
		}
		return Number(float64(ab.curByteLength())), nil
	})

	// get SharedArrayBuffer.prototype.maxByteLength — §25.2.4.3.
	i.defineGetter(proto, "maxByteLength", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		ab, err := sharedOf(ctx, this, "get SharedArrayBuffer.prototype.maxByteLength")
		if err != nil {
			return nil, err
		}
		if ab.resizable {
			return Number(float64(ab.maxByteLength)), nil
		}
		return Number(float64(ab.curByteLength())), nil
	})

	// get SharedArrayBuffer.prototype.growable — §25.2.4.2.
	i.defineGetter(proto, "growable", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		ab, err := sharedOf(ctx, this, "get SharedArrayBuffer.prototype.growable")
		if err != nil {
			return nil, err
		}
		return Boolean(ab.resizable), nil
	})

	// SharedArrayBuffer.prototype.grow(newLength) — §25.2.4.4. Growth never
	// reallocates (bytes are aliased across agents); it only bumps the live
	// length within the pre-allocated maxByteLength block. Shrinking is rejected.
	i.defineMethod(proto, "grow", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		ab, err := sharedOf(ctx, this, "SharedArrayBuffer.prototype.grow")
		if err != nil {
			return nil, err
		}
		if !ab.resizable {
			return nil, i.throwError(ctx, "TypeError", "SharedArrayBuffer.prototype.grow called on non-growable SharedArrayBuffer")
		}
		newByteLength, err := i.toIndex(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		// §25.2.4.4 step 6: newByteLength must be within [currentByteLength,
		// maxByteLength]; anything else is a RangeError (this covers shrinking).
		if newByteLength < ab.byteLen || newByteLength > ab.maxByteLength {
			return nil, i.throwError(ctx, "RangeError", "SharedArrayBuffer.prototype.grow: invalid length")
		}
		ab.byteLen = newByteLength
		return Undef, nil
	})

	// SharedArrayBuffer.prototype.slice(start, end) — §25.2.4.5.
	i.defineMethod(proto, "slice", 2, i.sharedArrayBufferSlice)

	// SharedArrayBuffer.prototype[Symbol.toStringTag] = "SharedArrayBuffer",
	// { w:false, e:false, c:true } — §25.2.4.6.
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("SharedArrayBuffer"), Writable: false, Enumerable: false, Configurable: true})

	i.setGlobalHidden("SharedArrayBuffer", ctor)
}

// sharedArrayBufferSlice implements SharedArrayBuffer.prototype.slice
// (§25.2.4.5): it copies a byte range into a new SharedArrayBuffer produced via
// the receiver's SpeciesConstructor. Unlike ArrayBuffer.prototype.slice there is
// no detach to guard against, but the species constructor may still run user code
// and grow the source buffer.
func (i *Interpreter) sharedArrayBufferSlice(ctx context.Context, this Value, args []Value) (Value, error) {
	ab, ok := arrayBufferOf(this)
	if !ok || !ab.shared {
		return nil, i.throwError(ctx, "TypeError", "SharedArrayBuffer.prototype.slice called on incompatible receiver")
	}
	length := ab.curByteLength()

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
		return nil, i.throwError(ctx, "TypeError", "SharedArrayBuffer.prototype.slice called on incompatible receiver")
	}
	ctor, err := i.speciesConstructor(ctx, obj, i.sharedArrayBufferCtor)
	if err != nil {
		return nil, err
	}
	newV, err := ctor.fn.construct(ctx, ctor, []Value{Number(float64(newLength))})
	if err != nil {
		return nil, err
	}
	newAB, ok := arrayBufferOf(newV)
	if !ok || !newAB.shared {
		return nil, i.throwError(ctx, "TypeError", "SharedArrayBuffer.prototype.slice: species constructor did not return a SharedArrayBuffer")
	}
	if newV == this {
		return nil, i.throwError(ctx, "TypeError", "SharedArrayBuffer.prototype.slice: species constructor returned the same SharedArrayBuffer")
	}
	if newAB.curByteLength() < newLength {
		return nil, i.throwError(ctx, "TypeError", "SharedArrayBuffer.prototype.slice: species constructor returned a SharedArrayBuffer that is too small")
	}
	// A species constructor running user code may have grown the source buffer;
	// re-read its length and copy only what is still available.
	currentLength := ab.curByteLength()
	if first < currentLength {
		count := newLength
		if avail := currentLength - first; avail < count {
			count = avail
		}
		copy(newAB.data, ab.data[first:first+count])
	}
	return newV, nil
}
