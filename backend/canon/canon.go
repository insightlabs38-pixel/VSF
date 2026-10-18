// Package canon implements the VSF canonical JSON profile shared by Rust, TypeScript and Go.
package canon

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	MaxInputBytes = 16 * 1024 * 1024
	MaxDepth      = 64
	MaxSafeInt    = 9007199254740991
)

// Object preserves parse order; canonical output sorts keys by UTF-16 code units.
type Object struct {
	Keys   []string
	Fields map[string]any
}

func NewObject() *Object { return &Object{Fields: map[string]any{}} }

func (o *Object) Set(key string, value any) {
	if _, ok := o.Fields[key]; !ok {
		o.Keys = append(o.Keys, key)
	}
	o.Fields[key] = value
}

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

func fail(code string) error { return &Error{Code: code} }

func Parse(input []byte) (any, error) {
	if len(input) > MaxInputBytes {
		return nil, fail("canon.too_large")
	}
	if !utf8.Valid(input) {
		return nil, fail("canon.invalid_utf8")
	}
	p := &parser{src: input}
	p.skipWS()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos != len(p.src) {
		return nil, fail("canon.trailing_data")
	}
	return v, nil
}

func Canonicalize(input []byte) ([]byte, error) {
	v, err := Parse(input)
	if err != nil {
		return nil, err
	}
	return ToCanonical(v), nil
}

func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ToCanonical(v any) []byte {
	var sb strings.Builder
	write(&sb, v)
	return []byte(sb.String())
}

func utf16Less(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

func write(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case int64:
		sb.WriteString(strconv.FormatInt(t, 10))
	case string:
		writeString(sb, t)
	case []any:
		sb.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			write(sb, item)
		}
		sb.WriteByte(']')
	case *Object:
		keys := append([]string(nil), t.Keys...)
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeString(sb, k)
			sb.WriteByte(':')
			write(sb, t.Fields[k])
		}
		sb.WriteByte('}')
	}
}

const hexDigits = "0123456789abcdef"

func writeString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				sb.WriteString(`\u00`)
				sb.WriteByte(hexDigits[r>>4])
				sb.WriteByte(hexDigits[r&0xf])
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

type parser struct {
	src []byte
	pos int
}

func (p *parser) peek() int {
	if p.pos < len(p.src) {
		return int(p.src[p.pos])
	}
	return -1
}

func (p *parser) skipWS() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) expect(b byte) error {
	if p.peek() != int(b) {
		return fail("canon.syntax")
	}
	p.pos++
	return nil
}

func (p *parser) literal(word string, v any) (any, error) {
	if !strings.HasPrefix(string(p.src[p.pos:min(len(p.src), p.pos+len(word))]), word) {
		return nil, fail("canon.syntax")
	}
	p.pos += len(word)
	return v, nil
}

func (p *parser) value(depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fail("canon.depth_exceeded")
	}
	switch c := p.peek(); {
	case c == 'n':
		return p.literal("null", nil)
	case c == 't':
		return p.literal("true", true)
	case c == 'f':
		return p.literal("false", false)
	case c == '"':
		return p.str()
	case c == '[':
		return p.array(depth)
	case c == '{':
		return p.object(depth)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return nil, fail("canon.syntax")
}

func (p *parser) array(depth int) (any, error) {
	if err := p.expect('['); err != nil {
		return nil, err
	}
	items := []any{}
	p.skipWS()
	if p.peek() == ']' {
		p.pos++
		return items, nil
	}
	for {
		p.skipWS()
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
		p.skipWS()
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return items, nil
		default:
			return nil, fail("canon.syntax")
		}
	}
}

func (p *parser) object(depth int) (any, error) {
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	obj := NewObject()
	p.skipWS()
	if p.peek() == '}' {
		p.pos++
		return obj, nil
	}
	for {
		p.skipWS()
		if p.peek() != '"' {
			return nil, fail("canon.syntax")
		}
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		key := k.(string)
		p.skipWS()
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		p.skipWS()
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		if _, dup := obj.Fields[key]; dup {
			return nil, fail("canon.duplicate_key")
		}
		obj.Set(key, v)
		p.skipWS()
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return obj, nil
		default:
			return nil, fail("canon.syntax")
		}
	}
}

func isDigit(c int) bool { return c >= '0' && c <= '9' }

func (p *parser) number() (any, error) {
	negative := p.peek() == '-'
	if negative {
		p.pos++
	}
	start := p.pos
	switch c := p.peek(); {
	case c == '0':
		p.pos++
	case c >= '1' && c <= '9':
		for isDigit(p.peek()) {
			p.pos++
		}
	default:
		return nil, fail("canon.syntax")
	}
	if c := p.peek(); c == '.' || c == 'e' || c == 'E' {
		return nil, fail("canon.non_integer")
	}
	if isDigit(p.peek()) {
		return nil, fail("canon.syntax")
	}
	digits := p.src[start:p.pos]
	if len(digits) > 16 {
		return nil, fail("canon.integer_out_of_range")
	}
	n, _ := strconv.ParseInt(string(digits), 10, 64)
	if n > MaxSafeInt {
		return nil, fail("canon.integer_out_of_range")
	}
	if negative {
		n = -n
	}
	return n, nil
}

func (p *parser) hex4() (rune, error) {
	if p.pos+4 > len(p.src) {
		return 0, fail("canon.invalid_escape")
	}
	var n rune
	for _, b := range p.src[p.pos : p.pos+4] {
		var d byte
		switch {
		case b >= '0' && b <= '9':
			d = b - '0'
		case b >= 'a' && b <= 'f':
			d = b - 'a' + 10
		case b >= 'A' && b <= 'F':
			d = b - 'A' + 10
		default:
			return 0, fail("canon.invalid_escape")
		}
		n = n*16 + rune(d)
	}
	p.pos += 4
	return n, nil
}

func (p *parser) str() (any, error) {
	if err := p.expect('"'); err != nil {
		return nil, err
	}
	var sb strings.Builder
	for {
		start := p.pos
		for p.pos < len(p.src) {
			b := p.src[p.pos]
			if b == '"' || b == '\\' || b < 0x20 {
				break
			}
			p.pos++
		}
		sb.Write(p.src[start:p.pos])
		switch p.peek() {
		case '"':
			p.pos++
			return sb.String(), nil
		case '\\':
			p.pos++
			esc := p.peek()
			if esc < 0 {
				return nil, fail("canon.invalid_escape")
			}
			p.pos++
			switch esc {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case '/':
				sb.WriteByte('/')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'u':
				hi, err := p.hex4()
				if err != nil {
					return nil, err
				}
				switch {
				case hi >= 0xD800 && hi < 0xDC00:
					if p.pos+2 > len(p.src) || p.src[p.pos] != '\\' || p.src[p.pos+1] != 'u' {
						return nil, fail("canon.lone_surrogate")
					}
					p.pos += 2
					lo, err := p.hex4()
					if err != nil {
						return nil, err
					}
					if lo < 0xDC00 || lo >= 0xE000 {
						return nil, fail("canon.lone_surrogate")
					}
					sb.WriteRune(utf16.DecodeRune(hi, lo))
				case hi >= 0xDC00 && hi < 0xE000:
					return nil, fail("canon.lone_surrogate")
				default:
					sb.WriteRune(hi)
				}
			default:
				return nil, fail("canon.invalid_escape")
			}
		default:
			return nil, fail("canon.syntax")
		}
	}
}
