package interp

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"
)

// initJSON installs the JSON namespace with parse and stringify.
func (i *Interpreter) initJSON() {
	j := NewObject(i.objectProto)
	j.class = "JSON"

	i.defineMethod(j, "stringify", 3, func(ctx context.Context, this Value, args []Value) (Value, error) {
		st := &jsonState{i: i, seen: map[*Object]bool{}}

		// Determine ReplacerFunction and PropertyList (§25.5.2).
		replacer := arg(args, 1)
		if ro, ok := replacer.(*Object); ok {
			if ro.IsCallable() {
				st.replacerFn = ro
			} else {
				// IsArray recurses through a Proxy and throws for a revoked one.
				isArr, err := i.isArrayV(ctx, ro)
				if err != nil {
					return nil, err
				}
				if isArr {
					list, err := i.jsonPropertyList(ctx, ro)
					if err != nil {
						return nil, err
					}
					st.propList = list
				}
			}
		}

		// Determine the gap string from the space argument (unwrapping Number and
		// String wrapper objects first).
		gap, err := i.jsonGap(ctx, arg(args, 2))
		if err != nil {
			return nil, err
		}
		st.gap = gap

		// wrapper = OrdinaryObjectCreate(%Object.prototype%); wrapper[""] = value.
		wrapper := NewObject(i.objectProto)
		wrapper.SetData("", arg(args, 0))

		var b strings.Builder
		ok, err := st.serializeProperty(ctx, &b, wrapper, "", "")
		if err != nil {
			return nil, err
		}
		if !ok {
			return Undef, nil
		}
		return String(b.String()), nil
	})

	i.defineMethod(j, "parse", 2, func(ctx context.Context, this Value, args []Value) (Value, error) {
		src, err := i.argStr(ctx, args, 0)
		if err != nil {
			return nil, err
		}
		p := &jsonParser{s: src, i: i}
		p.skipWS()
		node, err := p.parseValue(ctx)
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos != len(p.s) {
			return nil, i.throwError(ctx, "SyntaxError", "Unexpected non-whitespace character after JSON")
		}
		// Apply an optional reviver via InternalizeJSONProperty (§25.5.1).
		if reviver, ok := arg(args, 1).(*Object); ok && reviver.IsCallable() {
			root := NewObject(i.objectProto)
			root.SetData("", node.value)
			return i.internalizeJSONProperty(ctx, root, "", reviver, node)
		}
		return node.value, nil
	})

	// JSON.isRawJSON(obj) → whether obj carries an [[IsRawJSON]] slot (§25.5.3).
	i.defineMethod(j, "isRawJSON", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		if o, ok := arg(args, 0).(*Object); ok && o.internal != nil {
			if _, raw := o.internal["IsRawJSON"]; raw {
				return True, nil
			}
		}
		return False, nil
	})

	// JSON.rawJSON(text) → a frozen [[IsRawJSON]] object wrapping raw JSON text
	// for a primitive value (§25.5.1).
	i.defineMethod(j, "rawJSON", 1, func(ctx context.Context, this Value, args []Value) (Value, error) {
		s, err := i.ToStringV(ctx, arg(args, 0))
		if err != nil {
			return nil, err
		}
		rs := []rune(s)
		if len(rs) == 0 {
			return nil, i.throwError(ctx, "SyntaxError", "JSON.rawJSON text must not be empty")
		}
		first, last := rs[0], rs[len(rs)-1]
		okFirst := (first >= 'a' && first <= 'z') || (first >= '0' && first <= '9') || first == '"' || first == '-'
		okLast := (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9') || last == '"'
		if !okFirst || !okLast {
			return nil, i.throwError(ctx, "SyntaxError", "Invalid JSON.rawJSON text")
		}
		// Validate that the text is a single well-formed JSON primitive.
		p := &jsonParser{s: s, i: i}
		p.skipWS()
		node, err := p.parseValue(ctx)
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos != len(p.s) {
			return nil, i.throwError(ctx, "SyntaxError", "Invalid JSON.rawJSON text")
		}
		if _, isObj := node.value.(*Object); isObj {
			return nil, i.throwError(ctx, "SyntaxError", "JSON.rawJSON text must be a primitive value")
		}
		obj := NewObject(nil)
		obj.internal = map[string]any{"IsRawJSON": true}
		obj.defineOwn(StrKey("rawJSON"), &Property{
			Value:        String(s),
			Writable:     false,
			Enumerable:   true,
			Configurable: false,
		})
		obj.extensible = false
		return obj, nil
	})

	// JSON[Symbol.toStringTag] = "JSON" (§25.5.3).
	j.defineOwn(SymKey(i.symToStringTag), &Property{
		Value:        String("JSON"),
		Writable:     false,
		Enumerable:   false,
		Configurable: true,
	})

	i.setGlobalHidden("JSON", j)
}

// jsonState holds the serialization state shared across a single
// JSON.stringify call (§25.5.2): the replacer function, an optional property
// allow-list (from an array replacer), the indentation gap, and the stack of
// objects currently being serialized (for cyclic-structure detection).
type jsonState struct {
	i          *Interpreter
	replacerFn *Object          // ReplacerFunction, or nil
	propList   []string         // PropertyList (array replacer), or nil
	gap        string           // indentation unit
	seen       map[*Object]bool // objects on the current serialization stack
}

// jsonPropertyList builds the ordered, de-duplicated PropertyList from an array
// replacer: each element that is a String, a Number, or a Number/String wrapper
// object contributes its string form (§25.5.2 step 5.b.iii).
func (i *Interpreter) jsonPropertyList(ctx context.Context, arr *Object) ([]string, error) {
	list := []string{} // non-nil: an array replacer is an allow-list even if empty
	seen := map[string]bool{}
	// LengthOfArrayLike + [[Get]] so a Proxy replacer's traps run (§25.5.2).
	n, err := i.lengthOfArrayLike(ctx, arr)
	if err != nil {
		return nil, err
	}
	for idx := 0; idx < n; idx++ {
		v, err := arr.GetStr(ctx, intToStr(idx))
		if err != nil {
			return nil, err
		}
		var item string
		var ok bool
		switch x := flattenRope(v).(type) {
		case String:
			item, ok = string(x), true
		case Number:
			item, ok = NumberToString(float64(x)), true
		case *Object:
			if x.class == "String" || x.class == "Number" {
				s, err := i.ToStringV(ctx, v)
				if err != nil {
					return nil, err
				}
				item, ok = s, true
			}
		}
		if ok && !seen[item] {
			seen[item] = true
			list = append(list, item)
		}
	}
	return list, nil
}

// jsonGap computes the indentation gap from the space argument, unwrapping
// Number and String wrapper objects first (§25.5.2 steps 6-8).
func (i *Interpreter) jsonGap(ctx context.Context, space Value) (string, error) {
	if o, ok := space.(*Object); ok {
		switch o.class {
		case "Number":
			f, err := i.ToNumberV(ctx, o)
			if err != nil {
				return "", err
			}
			space = Number(f)
		case "String":
			s, err := i.ToStringV(ctx, o)
			if err != nil {
				return "", err
			}
			space = String(s)
		}
	}
	switch x := flattenRope(space).(type) {
	case Number:
		n := int(ToInteger(float64(x)))
		if n > 10 {
			n = 10
		}
		if n < 0 {
			n = 0
		}
		return strings.Repeat(" ", n), nil
	case String:
		// Truncate to the first 10 UTF-16 code units (§25.5.2.1 step 6).
		units := codeUnits(string(x))
		if len(units) > 10 {
			return unitsToString(units[:10]), nil
		}
		return string(x), nil
	default:
		return "", nil
	}
}

// serializeProperty implements SerializeJSONProperty (§25.5.2.1): it fetches
// holder[key], applies toJSON and the replacer, unwraps primitive wrappers, and
// serializes the result. ok=false means the value is omitted (undefined, a
// function, or a symbol).
func (st *jsonState) serializeProperty(ctx context.Context, b *strings.Builder, holder *Object, key, cur string) (bool, error) {
	i := st.i
	value, err := holder.GetStr(ctx, key)
	if err != nil {
		return false, err
	}

	// toJSON: applies to Objects and BigInts, incl. primitive BigInts whose
	// toJSON is inherited from BigInt.prototype (§25.5.2.1 step 2).
	if _, isObj := value.(*Object); isObj || isBigIntValue(value) {
		tj, err := i.getProperty(ctx, value, StrKey("toJSON"))
		if err != nil {
			return false, err
		}
		if fn, ok := tj.(*Object); ok && fn.IsCallable() {
			value, err = fn.fn.call(ctx, value, []Value{String(key)})
			if err != nil {
				return false, err
			}
		}
	}

	// ReplacerFunction: Call(replacer, holder, «key, value»).
	if st.replacerFn != nil {
		value, err = st.replacerFn.fn.call(ctx, holder, []Value{String(key), value})
		if err != nil {
			return false, err
		}
	}

	// A rawJSON object is emitted verbatim (§25.5.2.1 step for [[IsRawJSON]]).
	if o, ok := value.(*Object); ok && o.internal != nil {
		if _, raw := o.internal["IsRawJSON"]; raw {
			rj, err := o.GetStr(ctx, "rawJSON")
			if err != nil {
				return false, err
			}
			b.WriteString(stringValue(rj))
			return true, nil
		}
	}

	// Unwrap Number/String/Boolean/BigInt primitive wrappers.
	if o, ok := value.(*Object); ok {
		switch o.class {
		case "Number":
			f, err := i.ToNumberV(ctx, o)
			if err != nil {
				return false, err
			}
			value = Number(f)
		case "String":
			s, err := i.ToStringV(ctx, o)
			if err != nil {
				return false, err
			}
			value = String(s)
		case "Boolean":
			value = o.primitive
		case "BigInt":
			value = o.primitive
		}
	}

	// A concatenation value is a string primitive held as a lazy rope; flatten
	// it so the String case below matches (otherwise it would fall through and
	// be treated as unserializable).
	value = flattenRope(value)

	switch x := value.(type) {
	case Undefined, *Symbol:
		return false, nil
	case Null:
		b.WriteString("null")
		return true, nil
	case Boolean:
		if bool(x) {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		return true, nil
	case Number:
		if isFiniteNum(float64(x)) {
			b.WriteString(NumberToString(float64(x)))
		} else {
			b.WriteString("null")
		}
		return true, nil
	case String:
		writeJSONString(b, string(x))
		return true, nil
	case *BigInt:
		return false, i.throwError(ctx, "TypeError", "Do not know how to serialize a BigInt")
	case *Object:
		if x.IsCallable() {
			return false, nil
		}
		if st.seen[x] {
			return false, i.throwError(ctx, "TypeError", "Converting circular structure to JSON")
		}
		st.seen[x] = true
		defer delete(st.seen, x)
		// IsArray recurses through a Proxy so an array behind a proxy is
		// serialized as an array (§25.5.2.1 step 10).
		isArr, err := i.isArrayV(ctx, x)
		if err != nil {
			return false, err
		}
		if isArr {
			return true, st.serializeArray(ctx, b, x, cur)
		}
		return true, st.serializeObject(ctx, b, x, cur)
	default:
		return false, nil
	}
}

// isBigIntValue reports whether value is a primitive BigInt.
func isBigIntValue(value Value) bool {
	_, ok := value.(*BigInt)
	return ok
}

// serializeArray implements SerializeJSONArray (§25.5.2.4).
func (st *jsonState) serializeArray(ctx context.Context, b *strings.Builder, o *Object, cur string) error {
	// LengthOfArrayLike + [[Get]] per index so a Proxy array's traps run.
	length, err := st.i.lengthOfArrayLike(ctx, o)
	if err != nil {
		return err
	}
	if length == 0 {
		b.WriteString("[]")
		return nil
	}
	next := cur + st.gap
	b.WriteByte('[')
	for idx := 0; idx < length; idx++ {
		if idx > 0 {
			b.WriteByte(',')
		}
		writeNewlineIndent(b, st.gap, next)
		var sub strings.Builder
		ok, err := st.serializeProperty(ctx, &sub, o, intToStr(idx), next)
		if err != nil {
			return err
		}
		if ok {
			b.WriteString(sub.String())
		} else {
			b.WriteString("null")
		}
	}
	writeNewlineIndent(b, st.gap, cur)
	b.WriteByte(']')
	return nil
}

// serializeObject implements SerializeJSONObject (§25.5.2.5).
func (st *jsonState) serializeObject(ctx context.Context, b *strings.Builder, o *Object, cur string) error {
	next := cur + st.gap

	// K = PropertyList (array replacer) or EnumerableOwnPropertyNames(value, key),
	// which routes through [[OwnPropertyKeys]]/[[GetOwnProperty]] so a Proxy's
	// traps run (§25.5.2.5 step 5).
	var keys []string
	if st.propList != nil {
		keys = st.propList
	} else {
		names, err := st.i.jsonEnumerableOwnNames(ctx, o)
		if err != nil {
			return err
		}
		keys = names
	}

	b.WriteByte('{')
	first := true
	for _, name := range keys {
		var sub strings.Builder
		serOK, err := st.serializeProperty(ctx, &sub, o, name, next)
		if err != nil {
			return err
		}
		if !serOK {
			continue // skip undefined/function/symbol members
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		writeNewlineIndent(b, st.gap, next)
		writeJSONString(b, name)
		b.WriteByte(':')
		if st.gap != "" {
			b.WriteByte(' ')
		}
		b.WriteString(sub.String())
	}
	if !first {
		writeNewlineIndent(b, st.gap, cur)
	}
	b.WriteByte('}')
	return nil
}

func writeNewlineIndent(b *strings.Builder, indent, cur string) {
	if indent != "" {
		b.WriteByte('\n')
		b.WriteString(cur)
	}
}

// writeJSONString writes a JSON-quoted string, implementing QuoteJSONString
// (§25.5.2.2): it iterates UTF-16 code units so that a lone surrogate is emitted
// as a \uXXXX escape (well-formed JSON) rather than being folded to U+FFFD.
func writeJSONString(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	writeUnicodeEscape := func(cu uint16) {
		b.WriteString(`\u`)
		b.WriteByte(hex[(cu>>12)&0xF])
		b.WriteByte(hex[(cu>>8)&0xF])
		b.WriteByte(hex[(cu>>4)&0xF])
		b.WriteByte(hex[cu&0xF])
	}
	b.WriteByte('"')
	units := codeUnits(s)
	for k := 0; k < len(units); k++ {
		cu := units[k]
		switch cu {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case cu < 0x20:
				writeUnicodeEscape(cu)
			case cu >= 0xD800 && cu <= 0xDBFF:
				// A high surrogate paired with a following low surrogate is an
				// astral code point, written verbatim; an unpaired one escapes.
				if k+1 < len(units) && units[k+1] >= 0xDC00 && units[k+1] <= 0xDFFF {
					cp := 0x10000 + (rune(cu)-0xD800)<<10 + (rune(units[k+1]) - 0xDC00)
					b.WriteRune(cp)
					k++
				} else {
					writeUnicodeEscape(cu)
				}
			case cu >= 0xDC00 && cu <= 0xDFFF:
				writeUnicodeEscape(cu) // unpaired low surrogate
			default:
				b.WriteRune(rune(cu))
			}
		}
	}
	b.WriteByte('"')
}

func isFiniteNum(f float64) bool {
	return f == f && f-f == 0
}

// ---------------------------------------------------------------------------
// JSON parser
// ---------------------------------------------------------------------------

type jsonParser struct {
	s   string
	pos int
	i   *Interpreter
}

func (p *jsonParser) skipWS() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) errf(ctx context.Context, msg string) error {
	return p.i.throwError(ctx, "SyntaxError", msg)
}

// jsonNode is a JSON Parse Record analog (§25.5.2, CreateJSONParseRecord): it
// pairs a parsed value with the source text that produced it (for primitives)
// and the child records of arrays/objects, so JSON.parse can expose the matched
// `source` to a reviver (the parse-with-source proposal).
type jsonNode struct {
	value    Value
	source   string      // matched source text (primitive values only)
	elements []*jsonNode // array element records, in order
	entries  []jsonEntry // object entry records; the last write per key wins
}

type jsonEntry struct {
	key  string
	node *jsonNode
}

// setEntry records child under key, preserving the first-seen position but
// keeping the last value (mirroring duplicate-key handling in an ObjectLiteral).
func (n *jsonNode) setEntry(key string, child *jsonNode) {
	for idx := range n.entries {
		if n.entries[idx].key == key {
			n.entries[idx].node = child
			return
		}
	}
	n.entries = append(n.entries, jsonEntry{key: key, node: child})
}

func (p *jsonParser) parseValue(ctx context.Context) (*jsonNode, error) {
	p.skipWS()
	if p.pos >= len(p.s) {
		return nil, p.errf(ctx, "Unexpected end of JSON input")
	}
	start := p.pos
	switch c := p.s[p.pos]; {
	case c == '{':
		return p.parseObject(ctx)
	case c == '[':
		return p.parseArray(ctx)
	case c == '"':
		s, err := p.parseString(ctx)
		if err != nil {
			return nil, err
		}
		return &jsonNode{value: String(s), source: p.s[start:p.pos]}, nil
	case c == 't':
		return p.parseLiteral(ctx, "true", True, start)
	case c == 'f':
		return p.parseLiteral(ctx, "false", False, start)
	case c == 'n':
		return p.parseLiteral(ctx, "null", Nul, start)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber(ctx)
	default:
		return nil, p.errf(ctx, "Unexpected token in JSON")
	}
}

func (p *jsonParser) parseLiteral(ctx context.Context, word string, v Value, start int) (*jsonNode, error) {
	if strings.HasPrefix(p.s[p.pos:], word) {
		p.pos += len(word)
		return &jsonNode{value: v, source: p.s[start:p.pos]}, nil
	}
	return nil, p.errf(ctx, "Unexpected token in JSON")
}

func (p *jsonParser) parseObject(ctx context.Context) (*jsonNode, error) {
	o := NewObject(p.i.objectProto)
	node := &jsonNode{value: o}
	p.pos++ // {
	p.skipWS()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return node, nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.s) || p.s[p.pos] != '"' {
			return nil, p.errf(ctx, "Expected property name string in JSON")
		}
		key, err := p.parseString(ctx)
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos >= len(p.s) || p.s[p.pos] != ':' {
			return nil, p.errf(ctx, "Expected ':' after property name in JSON")
		}
		p.pos++
		child, err := p.parseValue(ctx)
		if err != nil {
			return nil, err
		}
		o.SetData(key, child.value)
		node.setEntry(key, child)
		p.skipWS()
		if p.pos >= len(p.s) {
			return nil, p.errf(ctx, "Unexpected end of JSON input")
		}
		if p.s[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.s[p.pos] == '}' {
			p.pos++
			return node, nil
		}
		return nil, p.errf(ctx, "Expected ',' or '}' in JSON")
	}
}

func (p *jsonParser) parseArray(ctx context.Context) (*jsonNode, error) {
	arr := p.i.newArray(nil)
	node := &jsonNode{value: arr}
	p.pos++ // [
	p.skipWS()
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return node, nil
	}
	for {
		child, err := p.parseValue(ctx)
		if err != nil {
			return nil, err
		}
		arr.elems = append(arr.elems, child.value)
		node.elements = append(node.elements, child)
		p.skipWS()
		if p.pos >= len(p.s) {
			return nil, p.errf(ctx, "Unexpected end of JSON input")
		}
		if p.s[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.s[p.pos] == ']' {
			p.pos++
			return node, nil
		}
		return nil, p.errf(ctx, "Expected ',' or ']' in JSON")
	}
}

func (p *jsonParser) parseString(ctx context.Context) (string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case c == '"':
			p.pos++
			return b.String(), nil
		case c == '\\':
			p.pos++
			if p.pos >= len(p.s) {
				return "", p.errf(ctx, "Unexpected end of JSON input")
			}
			switch e := p.s[p.pos]; e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if p.pos+4 >= len(p.s) {
					return "", p.errf(ctx, "Bad Unicode escape in JSON")
				}
				cp, err := strconv.ParseInt(p.s[p.pos+1:p.pos+5], 16, 32)
				if err != nil {
					return "", p.errf(ctx, "Bad Unicode escape in JSON")
				}
				var buf [utf8.UTFMax]byte
				n := utf8.EncodeRune(buf[:], rune(cp))
				b.Write(buf[:n])
				p.pos += 4
			default:
				return "", p.errf(ctx, "Bad escape in JSON")
			}
			p.pos++
		case c < 0x20:
			// Unescaped control characters U+0000-U+001F are not permitted in a
			// JSONString (§25.5.1 JSON grammar).
			return "", p.errf(ctx, "Bad control character in string literal in JSON")
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
	return "", p.errf(ctx, "Unterminated string in JSON")
}

func (p *jsonParser) parseNumber(ctx context.Context) (*jsonNode, error) {
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
			p.pos++
		} else {
			break
		}
	}
	f, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil {
		return nil, p.errf(ctx, "Invalid number in JSON")
	}
	return &jsonNode{value: Number(f), source: p.s[start:p.pos]}, nil
}

// ---------------------------------------------------------------------------
// Reviver (parse) — InternalizeJSONProperty
// ---------------------------------------------------------------------------

// internalizeJSONProperty implements InternalizeJSONProperty (§25.5.1.1): it
// recursively revives children before their parent and passes the reviver a
// context object whose "source" property (present only for unmodified primitive
// values) exposes the matched JSON source text (parse-with-source proposal).
func (i *Interpreter) internalizeJSONProperty(ctx context.Context, holder *Object, name string, reviver *Object, rec *jsonNode) (Value, error) {
	value, err := holder.GetStr(ctx, name)
	if err != nil {
		return nil, err
	}

	context := NewObject(i.objectProto)
	var elemRecs []*jsonNode
	var entryRecs []jsonEntry
	// The parse record applies only while the value is untouched by the reviver.
	if rec != nil && sameValue(rec.value, value) {
		if _, isObj := value.(*Object); !isObj {
			context.SetData("source", String(rec.source))
		}
		elemRecs = rec.elements
		entryRecs = rec.entries
	}

	if o, ok := value.(*Object); ok {
		// IsArray recurses through a Proxy and throws for a revoked one; the
		// child keys and mutations then go through the object internal methods
		// (LengthOfArrayLike/[[Get]]/[[Delete]]/[[DefineOwnProperty]]) so a
		// Proxy value installed by the reviver has its traps invoked (§25.5.1.1).
		isArr, err := i.isArrayV(ctx, o)
		if err != nil {
			return nil, err
		}
		if isArr {
			length, err := i.lengthOfArrayLike(ctx, o)
			if err != nil {
				return nil, err
			}
			for idx := 0; idx < length; idx++ {
				key := intToStr(idx)
				var er *jsonNode
				if idx < len(elemRecs) {
					er = elemRecs[idx]
				}
				newEl, err := i.internalizeJSONProperty(ctx, o, key, reviver, er)
				if err != nil {
					return nil, err
				}
				if IsUndefined(newEl) {
					if _, err := i.deleteV(ctx, o, StrKey(key)); err != nil {
						return nil, err
					}
				} else {
					if _, err := i.definePropertyV(ctx, o, StrKey(key), i.dataDescriptorObject(newEl)); err != nil {
						return nil, err
					}
				}
			}
		} else {
			keys, err := i.jsonEnumerableOwnNames(ctx, o)
			if err != nil {
				return nil, err
			}
			for _, key := range keys {
				var er *jsonNode
				for _, e := range entryRecs {
					if e.key == key {
						er = e.node
						break
					}
				}
				newEl, err := i.internalizeJSONProperty(ctx, o, key, reviver, er)
				if err != nil {
					return nil, err
				}
				if IsUndefined(newEl) {
					if _, err := i.deleteV(ctx, o, StrKey(key)); err != nil {
						return nil, err
					}
				} else {
					if _, err := i.definePropertyV(ctx, o, StrKey(key), i.dataDescriptorObject(newEl)); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	return reviver.fn.call(ctx, holder, []Value{String(name), value, context})
}

// jsonEnumerableOwnNames implements EnumerableOwnPropertyNames(o, key) for
// string keys (§7.3.23): each own string key from [[OwnPropertyKeys]] whose
// [[GetOwnProperty]] descriptor is enumerable, in order. Routing through the
// object internal methods lets a Proxy's ownKeys/getOwnPropertyDescriptor traps
// run.
func (i *Interpreter) jsonEnumerableOwnNames(ctx context.Context, o *Object) ([]string, error) {
	ownKeys, err := i.ownKeysV(ctx, o)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, k := range ownKeys {
		if k.IsSymbol() {
			continue
		}
		desc, ok, err := i.getOwnPropertyV(ctx, o, k)
		if err != nil {
			return nil, err
		}
		if !ok || !desc.Enumerable {
			continue
		}
		keys = append(keys, k.Str)
	}
	return keys, nil
}
