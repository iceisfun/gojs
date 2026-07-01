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
		indent := jsonIndent(ctx, i, arg(args, 2))
		var b strings.Builder
		ok, err := i.jsonStringify(ctx, &b, arg(args, 0), indent, "", map[*Object]bool{})
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
		return v, nil
	})

	i.setGlobalHidden("JSON", j)
}

// jsonIndent computes the gap string from the space argument.
func jsonIndent(ctx context.Context, i *Interpreter, space Value) string {
	switch x := space.(type) {
	case Number:
		n := int(ToInteger(float64(x)))
		if n > 10 {
			n = 10
		}
		if n < 0 {
			n = 0
		}
		return strings.Repeat(" ", n)
	case String:
		s := string(x)
		if len(s) > 10 {
			s = s[:10]
		}
		return s
	default:
		return ""
	}
}

// jsonStringify serializes v. It returns ok=false when v is not serializable
// (undefined, a function, or a symbol) at the top level, so callers can emit
// undefined.
func (i *Interpreter) jsonStringify(ctx context.Context, b *strings.Builder, v Value, indent, cur string, seen map[*Object]bool) (bool, error) {
	// Honor a toJSON method when present.
	if o, ok := v.(*Object); ok {
		if tj, _ := o.GetStr(ctx, "toJSON"); tj != nil {
			if fn, ok := tj.(*Object); ok && fn.IsCallable() {
				res, err := fn.fn.call(ctx, o, nil)
				if err != nil {
					return false, err
				}
				v = res
			}
		}
	}

	switch x := v.(type) {
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
	case *Object:
		if x.IsCallable() {
			return false, nil
		}
		if seen[x] {
			return false, i.throwError(ctx, "TypeError", "Converting circular structure to JSON")
		}
		seen[x] = true
		defer delete(seen, x)
		if x.isArray {
			return true, i.jsonArray(ctx, b, x, indent, cur, seen)
		}
		return true, i.jsonObject(ctx, b, x, indent, cur, seen)
	default:
		return false, nil
	}
}

func (i *Interpreter) jsonArray(ctx context.Context, b *strings.Builder, o *Object, indent, cur string, seen map[*Object]bool) error {
	if len(o.elems) == 0 {
		b.WriteString("[]")
		return nil
	}
	next := cur + indent
	b.WriteByte('[')
	for idx, e := range o.elems {
		if idx > 0 {
			b.WriteByte(',')
		}
		writeNewlineIndent(b, indent, next)
		var sub strings.Builder
		ok, err := i.jsonStringify(ctx, &sub, e, indent, next, seen)
		if err != nil {
			return err
		}
		if ok {
			b.WriteString(sub.String())
		} else {
			b.WriteString("null")
		}
	}
	writeNewlineIndent(b, indent, cur)
	b.WriteByte(']')
	return nil
}

func (i *Interpreter) jsonObject(ctx context.Context, b *strings.Builder, o *Object, indent, cur string, seen map[*Object]bool) error {
	next := cur + indent
	b.WriteByte('{')
	first := true
	for _, name := range o.OwnKeys() {
		p, ok := o.getOwn(StrKey(name))
		if !ok || !p.Enumerable {
			continue
		}
		val, err := o.GetStr(ctx, name)
		if err != nil {
			return err
		}
		var sub strings.Builder
		serOK, err := i.jsonStringify(ctx, &sub, val, indent, next, seen)
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
		writeNewlineIndent(b, indent, next)
		writeJSONString(b, name)
		b.WriteByte(':')
		if indent != "" {
			b.WriteByte(' ')
		}
		b.WriteString(sub.String())
	}
	if !first {
		writeNewlineIndent(b, indent, cur)
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
