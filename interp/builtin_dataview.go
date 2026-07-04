package interp

import (
	"context"
	"encoding/binary"
	"math"
	"math/big"
)

// dataViewData is the backing record for a DataView instance, stored in the
// object's internal slot map under key "DataView".
type dataViewData struct {
	buffer     *Object // [[ViewedArrayBuffer]] (an ArrayBuffer object)
	byteOffset int     // [[ByteOffset]]
	byteLength int     // [[ByteLength]] (valid when !autoLength)
	autoLength bool    // true when [[ByteLength]] is ~auto~ (length-tracking)
}

// dataViewOf returns the DataView record for v, or (nil, false).
func dataViewOf(v Value) (*dataViewData, bool) {
	o, ok := v.(*Object)
	if !ok || o.internal == nil {
		return nil, false
	}
	dv, ok := o.internal["DataView"].(*dataViewData)
	return dv, ok
}

// viewOutOfBounds reports whether the view's bounds exceed the current buffer,
// modelling IsViewOutOfBounds (§25.3.3.2). A detached buffer is out of bounds.
// When in bounds it also returns the effective view byte length.
func viewOutOfBounds(dv *dataViewData) (bool, int) {
	ab, ok := arrayBufferOf(dv.buffer)
	if !ok || ab.detached {
		return true, 0
	}
	bufLen := len(ab.data)
	if dv.byteOffset > bufLen {
		return true, 0
	}
	if dv.autoLength {
		return false, bufLen - dv.byteOffset
	}
	if dv.byteOffset+dv.byteLength > bufLen {
		return true, 0
	}
	return false, dv.byteLength
}

// initDataView installs the DataView constructor and prototype.
func (i *Interpreter) initDataView() {
	proto := i.dataViewProto

	construct := func(ctx context.Context, newTarget Value, args []Value) (Value, error) {
		if IsUndefined(newTarget) || newTarget == nil {
			return nil, i.throwError(ctx, "TypeError", "Constructor DataView requires 'new'")
		}
		ab, ok := arrayBufferOf(arg(args, 0))
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "DataView: first argument must be an ArrayBuffer")
		}
		offset, err := i.toIndex(ctx, arg(args, 1))
		if err != nil {
			return nil, err
		}
		if ab.detached {
			return nil, i.throwError(ctx, "TypeError", "DataView: ArrayBuffer is detached")
		}
		bufferByteLength := len(ab.data)
		if offset > bufferByteLength {
			return nil, i.throwError(ctx, "RangeError", "DataView: byteOffset is out of bounds")
		}

		var viewByteLength int
		autoLength := false
		if lv := arg(args, 2); IsUndefined(lv) {
			if ab.resizable {
				autoLength = true
			} else {
				viewByteLength = bufferByteLength - offset
			}
		} else {
			viewByteLength, err = i.toIndex(ctx, lv)
			if err != nil {
				return nil, err
			}
			if offset+viewByteLength > bufferByteLength {
				return nil, i.throwError(ctx, "RangeError", "DataView: byteLength is out of bounds")
			}
		}

		proto, err := i.protoFromConstructor(ctx, newTarget, func(r *Interpreter) *Object { return r.dataViewProto })
		if err != nil {
			return nil, err
		}
		// Re-check after OrdinaryCreateFromConstructor (which can run user code
		// via a Proxy/getter on newTarget.prototype).
		if ab.detached {
			return nil, i.throwError(ctx, "TypeError", "DataView: ArrayBuffer is detached")
		}
		bufferByteLength = len(ab.data)
		if offset > bufferByteLength {
			return nil, i.throwError(ctx, "RangeError", "DataView: byteOffset is out of bounds")
		}
		if !IsUndefined(arg(args, 2)) && offset+viewByteLength > bufferByteLength {
			return nil, i.throwError(ctx, "RangeError", "DataView: byteLength is out of bounds")
		}

		buf, _ := arg(args, 0).(*Object)
		obj := NewObject(proto)
		obj.class = "DataView"
		obj.internal = map[string]any{"DataView": &dataViewData{
			buffer:     buf,
			byteOffset: offset,
			byteLength: viewByteLength,
			autoLength: autoLength,
		}}
		return obj, nil
	}
	callFn := func(ctx context.Context, _ Value, _ []Value) (Value, error) {
		return nil, i.throwError(ctx, "TypeError", "Constructor DataView requires 'new'")
	}
	ctor := i.newNativeCtor("DataView", 1, callFn, construct)
	i.dataViewCtor = ctor
	linkCtor(ctor, proto)

	// get DataView.prototype.buffer — §25.3.4.1.
	i.defineGetter(proto, "buffer", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		dv, ok := dataViewOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "get DataView.prototype.buffer called on incompatible receiver")
		}
		return dv.buffer, nil
	})

	// get DataView.prototype.byteLength — §25.3.4.2.
	i.defineGetter(proto, "byteLength", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		dv, ok := dataViewOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "get DataView.prototype.byteLength called on incompatible receiver")
		}
		oob, size := viewOutOfBounds(dv)
		if oob {
			return nil, i.throwError(ctx, "TypeError", "get DataView.prototype.byteLength: view is out of bounds")
		}
		return Number(float64(size)), nil
	})

	// get DataView.prototype.byteOffset — §25.3.4.3.
	i.defineGetter(proto, "byteOffset", func(ctx context.Context, this Value, _ []Value) (Value, error) {
		dv, ok := dataViewOf(this)
		if !ok {
			return nil, i.throwError(ctx, "TypeError", "get DataView.prototype.byteOffset called on incompatible receiver")
		}
		if oob, _ := viewOutOfBounds(dv); oob {
			return nil, i.throwError(ctx, "TypeError", "get DataView.prototype.byteOffset: view is out of bounds")
		}
		return Number(float64(dv.byteOffset)), nil
	})

	// Getters and setters for each element type. §25.3.4.5 – §25.3.4.24.
	for _, e := range dataViewElementTypes {
		e := e
		i.defineMethod(proto, "get"+e.name, 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
			return i.getViewValue(ctx, this, arg(args, 0), arg(args, 1), e)
		})
		i.defineMethod(proto, "set"+e.name, 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
			return i.setViewValue(ctx, this, arg(args, 0), arg(args, 1), arg(args, 2), e)
		})
	}

	// DataView.prototype[Symbol.toStringTag] = "DataView" — §25.3.4.25.
	proto.defineOwn(SymKey(i.symToStringTag), &Property{Value: String("DataView"), Writable: false, Enumerable: false, Configurable: true})

	i.setGlobalHidden("DataView", ctor)
}

// dvElement describes a DataView element type: its name (as used in
// getX/setX method names), byte size, and whether it is a BigInt type.
type dvElement struct {
	name   string
	size   int
	bigInt bool
}

var dataViewElementTypes = []dvElement{
	{"Int8", 1, false},
	{"Uint8", 1, false},
	{"Int16", 2, false},
	{"Uint16", 2, false},
	{"Int32", 4, false},
	{"Uint32", 4, false},
	{"Float16", 2, false},
	{"Float32", 4, false},
	{"Float64", 8, false},
	{"BigInt64", 8, true},
	{"BigUint64", 8, true},
}

// getViewValue implements GetViewValue (§25.3.1.1).
func (i *Interpreter) getViewValue(ctx context.Context, view, requestIndex, littleEndian Value, e dvElement) (Value, error) {
	dv, ok := dataViewOf(view)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "DataView.prototype.get"+e.name+" called on incompatible receiver")
	}
	getIndex, err := i.toIndex(ctx, requestIndex)
	if err != nil {
		return nil, err
	}
	isLE := ToBoolean(littleEndian)
	oob, viewSize := viewOutOfBounds(dv)
	if oob {
		return nil, i.throwError(ctx, "TypeError", "DataView.prototype.get"+e.name+": view is out of bounds")
	}
	if getIndex+e.size > viewSize {
		return nil, i.throwError(ctx, "RangeError", "DataView.prototype.get"+e.name+": offset is out of bounds")
	}
	ab, _ := arrayBufferOf(dv.buffer)
	b := ab.data[dv.byteOffset+getIndex : dv.byteOffset+getIndex+e.size]
	return rawBytesToValue(b, isLE, e), nil
}

// setViewValue implements SetViewValue (§25.3.1.2).
func (i *Interpreter) setViewValue(ctx context.Context, view, requestIndex, value, littleEndian Value, e dvElement) (Value, error) {
	dv, ok := dataViewOf(view)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "DataView.prototype.set"+e.name+" called on incompatible receiver")
	}
	getIndex, err := i.toIndex(ctx, requestIndex)
	if err != nil {
		return nil, err
	}
	// Numeric conversion happens before the out-of-bounds / RangeError checks.
	var num float64
	var bnum *big.Int
	if e.bigInt {
		bv, err := i.toBigIntStrict(ctx, value)
		if err != nil {
			return nil, err
		}
		bnum = bv.(*BigInt).Int
	} else {
		num, err = i.ToNumberV(ctx, value)
		if err != nil {
			return nil, err
		}
	}
	isLE := ToBoolean(littleEndian)
	oob, viewSize := viewOutOfBounds(dv)
	if oob {
		return nil, i.throwError(ctx, "TypeError", "DataView.prototype.set"+e.name+": view is out of bounds")
	}
	if getIndex+e.size > viewSize {
		return nil, i.throwError(ctx, "RangeError", "DataView.prototype.set"+e.name+": offset is out of bounds")
	}
	ab, _ := arrayBufferOf(dv.buffer)
	b := ab.data[dv.byteOffset+getIndex : dv.byteOffset+getIndex+e.size]
	valueToRawBytes(b, num, bnum, isLE, e)
	return Undef, nil
}

// rawBytesToValue interprets len(b)==e.size bytes as an element of type e.
func rawBytesToValue(b []byte, littleEndian bool, e dvElement) Value {
	order := byteOrder(littleEndian)
	switch e.name {
	case "Int8":
		return Number(float64(int8(b[0])))
	case "Uint8":
		return Number(float64(b[0]))
	case "Int16":
		return Number(float64(int16(order.Uint16(b))))
	case "Uint16":
		return Number(float64(order.Uint16(b)))
	case "Int32":
		return Number(float64(int32(order.Uint32(b))))
	case "Uint32":
		return Number(float64(order.Uint32(b)))
	case "Float16":
		return Number(float16ToFloat64(order.Uint16(b)))
	case "Float32":
		return Number(float64(math.Float32frombits(order.Uint32(b))))
	case "Float64":
		return Number(math.Float64frombits(order.Uint64(b)))
	case "BigInt64":
		return NewBigInt(int64(order.Uint64(b)))
	case "BigUint64":
		return &BigInt{Int: new(big.Int).SetUint64(order.Uint64(b))}
	}
	return Undef
}

// valueToRawBytes writes num (for numeric types) or bnum (for BigInt types) into
// b following the element type e and endianness.
func valueToRawBytes(b []byte, num float64, bnum *big.Int, littleEndian bool, e dvElement) {
	order := byteOrder(littleEndian)
	switch e.name {
	case "Int8", "Uint8":
		b[0] = byte(ToUint32(num))
	case "Int16", "Uint16":
		order.PutUint16(b, uint16(ToUint32(num)))
	case "Int32", "Uint32":
		order.PutUint32(b, ToUint32(num))
	case "Float16":
		order.PutUint16(b, float64ToFloat16(num))
	case "Float32":
		order.PutUint32(b, math.Float32bits(float32(num)))
	case "Float64":
		order.PutUint64(b, math.Float64bits(num))
	case "BigInt64", "BigUint64":
		order.PutUint64(b, bigIntToUint64(bnum))
	}
}

// float16ToFloat64 decodes an IEEE 754-2019 binary16 value.
func float16ToFloat64(h uint16) float64 {
	sign := 1.0
	if h&0x8000 != 0 {
		sign = -1.0
	}
	exp := int((h >> 10) & 0x1F)
	frac := int(h & 0x3FF)
	switch exp {
	case 0:
		if frac == 0 {
			return sign * 0
		}
		return sign * math.Ldexp(float64(frac), -24)
	case 0x1F:
		if frac == 0 {
			return sign * math.Inf(1)
		}
		return math.NaN()
	default:
		return sign * math.Ldexp(float64(1024+frac), exp-25)
	}
}

// float64ToFloat16 rounds a Number to an IEEE 754-2019 binary16, ties to even.
func float64ToFloat16(f float64) uint16 {
	var sign uint16
	if math.Signbit(f) {
		sign = 0x8000
	}
	if math.IsNaN(f) {
		return sign | 0x7E00
	}
	if math.IsInf(f, 0) {
		return sign | 0x7C00
	}
	af := math.Abs(f)
	if af == 0 {
		return sign
	}
	frac, exp2 := math.Frexp(af) // af = frac * 2^exp2, frac in [0.5, 1)
	m := frac * 2                // in [1, 2)
	he := (exp2 - 1) + 15        // biased binary16 exponent
	if he >= 31 {
		return sign | 0x7C00 // overflow to infinity
	}
	if he >= 1 {
		// Normal: significand in [1024, 2048], round to nearest even.
		q := uint32(math.RoundToEven(m * 1024))
		if q >= 2048 {
			q = 1024
			he++
			if he >= 31 {
				return sign | 0x7C00
			}
		}
		return sign | uint16(he<<10) | uint16(q-1024)
	}
	// Subnormal or underflow: value = q * 2^-24.
	q := uint32(math.RoundToEven(math.Ldexp(af, 24)))
	if q == 0 {
		return sign
	}
	if q >= 1024 {
		return sign | (1 << 10) // rounded up to the smallest normal
	}
	return sign | uint16(q)
}

// toBigIntStrict implements the ToBigInt abstract operation (§7.1.13), which —
// unlike the BigInt() constructor's coercion — throws a TypeError for a Number.
func (i *Interpreter) toBigIntStrict(ctx context.Context, v Value) (Value, error) {
	prim, err := i.ToPrimitive(ctx, v, "number")
	if err != nil {
		return nil, err
	}
	if _, ok := prim.(Number); ok {
		return nil, i.throwError(ctx, "TypeError", "Cannot convert a Number value to a BigInt")
	}
	return i.toBigInt(ctx, prim)
}

// byteOrder returns the encoding/binary byte order for the given endianness.
func byteOrder(littleEndian bool) binary.ByteOrder {
	if littleEndian {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

// bigIntToUint64 reduces v modulo 2**64 and returns the low 64 bits.
func bigIntToUint64(v *big.Int) uint64 {
	// mask = 2**64 - 1
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	m := new(big.Int).And(new(big.Int).Mod(v, new(big.Int).Lsh(big.NewInt(1), 64)), mask)
	return m.Uint64()
}
