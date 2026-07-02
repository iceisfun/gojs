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
			} else if ro.isArray {
				list, err := i.jsonPropertyList(ctx, ro)
				if err != nil {
					return nil, err
				}
				st.propList = list
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
		v, err := p.parseValue(ctx)
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos != len(p.s) {
			return nil, i.throwError(ctx, "SyntaxError", "Unexpected non-whitespace character after JSON")
		}
		// Apply an optional reviver, walking bottom-up.
		if reviver, ok := arg(args, 1).(*Object); ok && reviver.IsCallable() {
			holder := NewObject(i.objectProto)
			holder.SetData("", v)
			return i.jsonRevive(ctx, holder, "", reviver)
		}
		return v, nil
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
	n := len(arr.elems)
	for idx := 0; idx < n; idx++ {
		v := undefIfHole(arr.elems[idx])
		var item string
		var ok bool
		switch x := v.(type) {
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
	switch x := space.(type) {
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
		s := string(x)
		// Truncate to 10 code units (code points here).
		if r := []rune(s); len(r) > 10 {
			s = string(r[:10])
		}
		return s, nil
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
		if x.isArray {
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
	if len(o.elems) == 0 {
		b.WriteString("[]")
		return nil
	}
	next := cur + st.gap
	b.WriteByte('[')
	for idx := range o.elems {
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

	// K = PropertyList (array replacer) or the object's own enumerable keys.
	var keys []string
	if st.propList != nil {
		keys = st.propList
	} else {
		for _, name := range o.OwnKeys() {
			p, ok := o.getOwn(StrKey(name))
			if !ok || !p.Enumerable {
				continue
			}
			keys = append(keys, name)
		}
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

// writeJSONString writes a JSON-quoted string.
func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
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
			if r < 0x20 {
				b.WriteString(`\u`)
				const hex = "0123456789abcdef"
				b.WriteByte(hex[(r>>12)&0xF])
				b.WriteByte(hex[(r>>8)&0xF])
				b.WriteByte(hex[(r>>4)&0xF])
				b.WriteByte(hex[r&0xF])
			} else {
				b.WriteRune(r)
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

func (p *jsonParser) parseValue(ctx context.Context) (Value, error) {
	p.skipWS()
	if p.pos >= len(p.s) {
		return nil, p.errf(ctx, "Unexpected end of JSON input")
	}
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
		return String(s), nil
	case c == 't':
		return p.parseLiteral(ctx, "true", True)
	case c == 'f':
		return p.parseLiteral(ctx, "false", False)
	case c == 'n':
		return p.parseLiteral(ctx, "null", Nul)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber(ctx)
	default:
		return nil, p.errf(ctx, "Unexpected token in JSON")
	}
}

func (p *jsonParser) parseLiteral(ctx context.Context, word string, v Value) (Value, error) {
	if strings.HasPrefix(p.s[p.pos:], word) {
		p.pos += len(word)
		return v, nil
	}
	return nil, p.errf(ctx, "Unexpected token in JSON")
}

func (p *jsonParser) parseObject(ctx context.Context) (Value, error) {
	o := NewObject(p.i.objectProto)
	p.pos++ // {
	p.skipWS()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return o, nil
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
		val, err := p.parseValue(ctx)
		if err != nil {
			return nil, err
		}
		o.SetData(key, val)
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
			return o, nil
		}
		return nil, p.errf(ctx, "Expected ',' or '}' in JSON")
	}
}

func (p *jsonParser) parseArray(ctx context.Context) (Value, error) {
	p.pos++ // [
	p.skipWS()
	var elems []Value
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return p.i.newArray(elems), nil
	}
	for {
		val, err := p.parseValue(ctx)
		if err != nil {
			return nil, err
		}
		elems = append(elems, val)
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
			return p.i.newArray(elems), nil
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

func (p *jsonParser) parseNumber(ctx context.Context) (Value, error) {
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
	return Number(f), nil
}

// ---------------------------------------------------------------------------
// Replacer (stringify) and reviver (parse)
// ---------------------------------------------------------------------------
// jsonRevive applies a reviver function bottom-up: children are revived before
// their parent, and a child revived to undefined is deleted.
func (i *Interpreter) jsonRevive(ctx context.Context, holder *Object, key string, reviver *Object) (Value, error) {
	val, _ := holder.GetStr(ctx, key)
	if o, ok := val.(*Object); ok {
		if o.isArray {
			for idx := range o.elems {
				name := intToStr(idx)
				revived, err := i.jsonRevive(ctx, o, name, reviver)
				if err != nil {
					return nil, err
				}
				o.elems[idx] = revived // undefined becomes a hole/undefined
			}
		} else {
			for _, name := range o.OwnKeys() {
				revived, err := i.jsonRevive(ctx, o, name, reviver)
				if err != nil {
					return nil, err
				}
				if IsUndefined(revived) {
					o.Delete(StrKey(name))
				} else {
					o.SetData(name, revived)
				}
			}
		}
	}
	return reviver.fn.call(ctx, holder, []Value{String(key), val})
}
