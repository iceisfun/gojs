package interp

import "context"

// This file implements the Uint8Array base64/hex proposal: the statics
// Uint8Array.fromBase64 / fromHex and the %Uint8Array.prototype% methods
// toBase64 / toHex / setFromBase64 / setFromHex. Decoding tolerates ASCII
// whitespace (base64 only), rejects any other stray character with a SyntaxError,
// and supports the base64/base64url alphabets plus the three lastChunkHandling
// modes (loose / strict / stop-before-partial). The setFrom* methods write into
// an existing array up to its length and report { read, written }.

const b64Std = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
const b64Url = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// b64Decode maps an ASCII byte to its 6-bit value, or -1 if not in the alphabet.
func b64Decode(url bool) *[256]int {
	if url {
		return &b64UrlTable
	}
	return &b64StdTable
}

var b64StdTable, b64UrlTable = func() ([256]int, [256]int) {
	var std, url [256]int
	for i := range std {
		std[i], url[i] = -1, -1
	}
	for i := 0; i < 64; i++ {
		std[b64Std[i]] = i
		url[b64Url[i]] = i
	}
	return std, url
}()

// isB64Whitespace reports the ASCII whitespace the decoder skips: tab, LF, FF,
// CR, and space (U+000B vertical tab is deliberately excluded).
func isB64Whitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// decodeBase64 decodes s, returning the bytes, the number of input characters
// consumed (read), or a SyntaxError. maxBytes caps the output (-1 = unlimited);
// when the next chunk's bytes would not all fit, decoding stops before it and
// read reflects only the fully-decoded input.
func (i *Interpreter) decodeBase64(ctx context.Context, s string, url bool, lastChunk string, maxBytes int) ([]byte, int, error) {
	// A zero-capacity target consumes nothing and never errors (FromBase64 step 3).
	if maxBytes == 0 {
		return nil, 0, nil
	}
	table := b64Decode(url)
	var out []byte
	var chunk [4]int
	// On error, return the bytes decoded so far (the valid prefix) so setFrom*
	// can write them before propagating the SyntaxError (fromBase64 discards them).
	syntax := func() ([]byte, int, error) { return out, len(out), i.throwError(ctx, "SyntaxError", "invalid base64") }
	chunkLen := 0
	read := 0 // input index after the last fully-decoded chunk
	idx := 0
	n := len(s)
	for {
		if idx >= n {
			if chunkLen == 0 {
				return out, n, nil
			}
			switch lastChunk {
			case "stop-before-partial":
				return out, read, nil
			case "strict":
				return syntax()
			default: // loose
				if chunkLen == 1 {
					return syntax()
				}
				b, ok := decodePartial(chunk, chunkLen, false)
				if !ok {
					return syntax()
				}
				if maxBytes >= 0 && len(out)+len(b) > maxBytes {
					return out, read, nil
				}
				return append(out, b...), n, nil
			}
		}
		c := s[idx]
		if isB64Whitespace(c) {
			idx++
			continue
		}
		if c == '=' {
			if chunkLen != 2 && chunkLen != 3 {
				return syntax()
			}
			idx++
			// A 2-character chunk needs "==" to be a complete 4-character chunk; a
			// 3-character chunk needs a single "=". An incomplete padded chunk stops
			// stop-before-partial (leaving it undecoded) and is a SyntaxError otherwise.
			if chunkLen == 2 {
				for idx < n && isB64Whitespace(s[idx]) {
					idx++
				}
				if idx >= n || s[idx] != '=' {
					if lastChunk == "stop-before-partial" {
						return out, read, nil
					}
					return syntax()
				}
				idx++
			}
			b, ok := decodePartial(chunk, chunkLen, lastChunk == "strict")
			if !ok {
				return syntax()
			}
			// Only whitespace may follow the padding.
			for idx < n {
				if !isB64Whitespace(s[idx]) {
					return syntax()
				}
				idx++
			}
			if maxBytes >= 0 && len(out)+len(b) > maxBytes {
				return out, read, nil
			}
			return append(out, b...), n, nil
		}
		v := table[c]
		if v < 0 {
			return syntax()
		}
		chunk[chunkLen] = v
		chunkLen++
		idx++
		if chunkLen == 4 {
			if maxBytes >= 0 && len(out)+3 > maxBytes {
				return out, read, nil
			}
			out = append(out, byte(chunk[0]<<2|chunk[1]>>4), byte(chunk[1]<<4|chunk[2]>>2), byte(chunk[2]<<6|chunk[3]))
			chunkLen = 0
			read = idx
			// Output is full: stop immediately, ignoring any trailing input
			// (FromBase64 step 10.l.v).
			if maxBytes >= 0 && len(out) == maxBytes {
				return out, read, nil
			}
		}
	}
}

// decodePartial decodes a 2- or 3-character trailing chunk into 1 or 2 bytes. In
// strict mode the unused low bits of the last sextet must be zero.
func decodePartial(chunk [4]int, chunkLen int, strict bool) ([]byte, bool) {
	if chunkLen == 2 {
		if strict && chunk[1]&0x0F != 0 {
			return nil, false
		}
		return []byte{byte(chunk[0]<<2 | chunk[1]>>4)}, true
	}
	// chunkLen == 3
	if strict && chunk[2]&0x03 != 0 {
		return nil, false
	}
	return []byte{byte(chunk[0]<<2 | chunk[1]>>4), byte(chunk[1]<<4 | chunk[2]>>2)}, true
}

// decodeHex decodes hex pairs, up to maxBytes bytes (-1 = unlimited), returning
// bytes and characters consumed. Odd length (when not capped) or a non-hex
// character is a SyntaxError.
func (i *Interpreter) decodeHex(ctx context.Context, s string, maxBytes int) ([]byte, int, error) {
	n := len(s)
	// Odd length is rejected up front, before any byte is produced (FromHex step
	// 5): no data is written even when a valid prefix exists.
	if n%2 != 0 {
		return nil, 0, i.throwError(ctx, "SyntaxError", "invalid hex string length")
	}
	var out []byte
	j := 0
	for j < n {
		if maxBytes >= 0 && len(out) == maxBytes {
			break
		}
		hi, lo := hexVal(s[j]), hexVal(s[j+1])
		if hi < 0 || lo < 0 {
			// Bad character: the valid prefix IS written, then the error propagates.
			return out, j, i.throwError(ctx, "SyntaxError", "invalid hex character")
		}
		out = append(out, byte(hi<<4|lo))
		j += 2
	}
	return out, j, nil
}

func encodeBase64(b []byte, url, omitPadding bool) string {
	alpha := b64Std
	if url {
		alpha = b64Url
	}
	var sb []byte
	for i := 0; i+3 <= len(b); i += 3 {
		n := int(b[i])<<16 | int(b[i+1])<<8 | int(b[i+2])
		sb = append(sb, alpha[n>>18&63], alpha[n>>12&63], alpha[n>>6&63], alpha[n&63])
	}
	switch len(b) % 3 {
	case 1:
		n := int(b[len(b)-1]) << 16
		sb = append(sb, alpha[n>>18&63], alpha[n>>12&63])
		if !omitPadding {
			sb = append(sb, '=', '=')
		}
	case 2:
		n := int(b[len(b)-2])<<16 | int(b[len(b)-1])<<8
		sb = append(sb, alpha[n>>18&63], alpha[n>>12&63], alpha[n>>6&63])
		if !omitPadding {
			sb = append(sb, '=')
		}
	}
	return string(sb)
}

func encodeHex(b []byte) string {
	const digits = "0123456789abcdef"
	sb := make([]byte, 0, len(b)*2)
	for _, c := range b {
		sb = append(sb, digits[c>>4], digits[c&0x0F])
	}
	return string(sb)
}

// --- option / receiver helpers ---------------------------------------------

// toOptionsObject implements GetOptionsObject: undefined → a fresh null-proto
// object, an object → itself, anything else → TypeError.
func (i *Interpreter) toOptionsObject(ctx context.Context, v Value) (*Object, error) {
	if IsUndefined(v) {
		return NewObject(nil), nil
	}
	o, ok := v.(*Object)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "options must be an object or undefined")
	}
	return o, nil
}

// getEnumOption reads a string-valued option that must be one of allowed (or
// undefined → def). The value must be a primitive String (no coercion), else a
// TypeError; an unrecognized value is also a TypeError.
func (i *Interpreter) getEnumOption(ctx context.Context, opts *Object, name string, allowed []string, def string) (string, error) {
	v, err := opts.GetStr(ctx, name)
	if err != nil {
		return "", err
	}
	if IsUndefined(v) {
		return def, nil
	}
	s, ok := v.(String)
	if !ok {
		return "", i.throwError(ctx, "TypeError", name+" must be a string")
	}
	for _, a := range allowed {
		if string(s) == a {
			return a, nil
		}
	}
	return "", i.throwError(ctx, "TypeError", "invalid value for "+name)
}

func (i *Interpreter) getBoolOption(ctx context.Context, opts *Object, name string) (bool, error) {
	v, err := opts.GetStr(ctx, name)
	if err != nil {
		return false, err
	}
	if IsUndefined(v) {
		return false, nil
	}
	return ToBoolean(v), nil
}

// validateUint8Array implements ValidateUint8Array: this must be a Uint8Array.
func (i *Interpreter) validateUint8Array(ctx context.Context, this Value) (*typedArrayData, error) {
	td, ok := typedArrayOf(this)
	if !ok || td.kind != taUint8 {
		return nil, i.throwError(ctx, "TypeError", "not a Uint8Array")
	}
	return td, nil
}

func (i *Interpreter) newUint8ArrayFromBytes(ctx context.Context, b []byte) (*Object, error) {
	o := i.allocateTypedArray(taUint8, i.typedArrayKindProtos[taUint8])
	if err := i.allocateTypedArrayBuffer(ctx, o, len(b)); err != nil {
		return nil, err
	}
	ab, _ := arrayBufferOf(o.typedArray.buffer)
	copy(ab.data, b)
	return o, nil
}

// setFromResult builds the { read, written } record returned by setFrom*.
func (i *Interpreter) setFromResult(read, written int) *Object {
	o := NewObject(i.objectProto)
	o.SetData("read", Number(float64(read)))
	o.SetData("written", Number(float64(written)))
	return o
}

// --- the six methods --------------------------------------------------------

func (i *Interpreter) uint8FromBase64(ctx context.Context, _ Value, args []Value) (Value, error) {
	str, ok := arg(args, 0).(String)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Uint8Array.fromBase64 argument must be a string")
	}
	opts, err := i.toOptionsObject(ctx, arg(args, 1))
	if err != nil {
		return nil, err
	}
	alphabet, err := i.getEnumOption(ctx, opts, "alphabet", []string{"base64", "base64url"}, "base64")
	if err != nil {
		return nil, err
	}
	lastChunk, err := i.getEnumOption(ctx, opts, "lastChunkHandling", []string{"loose", "strict", "stop-before-partial"}, "loose")
	if err != nil {
		return nil, err
	}
	bytes, _, err := i.decodeBase64(ctx, string(str), alphabet == "base64url", lastChunk, -1)
	if err != nil {
		return nil, err
	}
	return i.newUint8ArrayFromBytes(ctx, bytes)
}

func (i *Interpreter) uint8FromHex(ctx context.Context, _ Value, args []Value) (Value, error) {
	str, ok := arg(args, 0).(String)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "Uint8Array.fromHex argument must be a string")
	}
	bytes, _, err := i.decodeHex(ctx, string(str), -1)
	if err != nil {
		return nil, err
	}
	return i.newUint8ArrayFromBytes(ctx, bytes)
}

func (i *Interpreter) uint8ToBase64(ctx context.Context, this Value, args []Value) (Value, error) {
	td, err := i.validateUint8Array(ctx, this)
	if err != nil {
		return nil, err
	}
	opts, err := i.toOptionsObject(ctx, arg(args, 0))
	if err != nil {
		return nil, err
	}
	alphabet, err := i.getEnumOption(ctx, opts, "alphabet", []string{"base64", "base64url"}, "base64")
	if err != nil {
		return nil, err
	}
	omitPadding, err := i.getBoolOption(ctx, opts, "omitPadding")
	if err != nil {
		return nil, err
	}
	// An option getter may have detached/shrunk the buffer out of bounds.
	if oob, _ := td.outOfBounds(); oob {
		return nil, i.throwError(ctx, "TypeError", "Uint8Array is out of bounds")
	}
	return String(encodeBase64(taBytes(td), alphabet == "base64url", omitPadding)), nil
}

func (i *Interpreter) uint8ToHex(ctx context.Context, this Value, _ []Value) (Value, error) {
	td, err := i.validateUint8Array(ctx, this)
	if err != nil {
		return nil, err
	}
	if oob, _ := td.outOfBounds(); oob {
		return nil, i.throwError(ctx, "TypeError", "Uint8Array is out of bounds")
	}
	return String(encodeHex(taBytes(td))), nil
}

func (i *Interpreter) uint8SetFromBase64(ctx context.Context, this Value, args []Value) (Value, error) {
	td, err := i.validateUint8Array(ctx, this)
	if err != nil {
		return nil, err
	}
	str, ok := arg(args, 0).(String)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "setFromBase64 argument must be a string")
	}
	opts, err := i.toOptionsObject(ctx, arg(args, 1))
	if err != nil {
		return nil, err
	}
	alphabet, err := i.getEnumOption(ctx, opts, "alphabet", []string{"base64", "base64url"}, "base64")
	if err != nil {
		return nil, err
	}
	lastChunk, err := i.getEnumOption(ctx, opts, "lastChunkHandling", []string{"loose", "strict", "stop-before-partial"}, "loose")
	if err != nil {
		return nil, err
	}
	oob, n := td.outOfBounds()
	if oob {
		return nil, i.throwError(ctx, "TypeError", "Uint8Array is out of bounds")
	}
	bytes, read, derr := i.decodeBase64(ctx, string(str), alphabet == "base64url", lastChunk, n)
	// The valid prefix is written even when decoding then fails ("writes up to
	// error"): write first, then propagate the SyntaxError.
	i.writeUint8(td, bytes)
	if derr != nil {
		return nil, derr
	}
	return i.setFromResult(read, len(bytes)), nil
}

func (i *Interpreter) uint8SetFromHex(ctx context.Context, this Value, args []Value) (Value, error) {
	td, err := i.validateUint8Array(ctx, this)
	if err != nil {
		return nil, err
	}
	str, ok := arg(args, 0).(String)
	if !ok {
		return nil, i.throwError(ctx, "TypeError", "setFromHex argument must be a string")
	}
	oob, n := td.outOfBounds()
	if oob {
		return nil, i.throwError(ctx, "TypeError", "Uint8Array is out of bounds")
	}
	bytes, read, derr := i.decodeHex(ctx, string(str), n)
	i.writeUint8(td, bytes)
	if derr != nil {
		return nil, derr
	}
	return i.setFromResult(read, len(bytes)), nil
}

// taBytes copies the current in-bounds bytes of a Uint8Array (element size 1).
func taBytes(td *typedArrayData) []byte {
	oob, n := td.outOfBounds()
	if oob {
		return nil
	}
	ab, _ := arrayBufferOf(td.buffer)
	out := make([]byte, n)
	copy(out, ab.data[td.byteOffset:td.byteOffset+n])
	return out
}

// writeUint8 copies bytes into the Uint8Array's buffer, bounded by its length.
func (i *Interpreter) writeUint8(td *typedArrayData, bytes []byte) {
	oob, n := td.outOfBounds()
	if oob {
		return
	}
	ab, _ := arrayBufferOf(td.buffer)
	m := len(bytes)
	if m > n {
		m = n
	}
	copy(ab.data[td.byteOffset:td.byteOffset+m], bytes[:m])
}

// initUint8Base64 registers the six methods on Uint8Array / its prototype.
func (i *Interpreter) initUint8Base64() {
	ctor := i.typedArrayKindCtors[taUint8]
	proto := i.typedArrayKindProtos[taUint8]
	if ctor == nil || proto == nil {
		return
	}
	i.defineMethod(ctor, "fromBase64", 1, i.uint8FromBase64)
	i.defineMethod(ctor, "fromHex", 1, i.uint8FromHex)
	i.defineMethod(proto, "toBase64", 0, i.uint8ToBase64)
	i.defineMethod(proto, "toHex", 0, i.uint8ToHex)
	i.defineMethod(proto, "setFromBase64", 1, i.uint8SetFromBase64)
	i.defineMethod(proto, "setFromHex", 1, i.uint8SetFromHex)
}
