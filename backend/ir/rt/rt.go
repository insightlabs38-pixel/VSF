// Package rt supports generated IR codecs: strict decoding over the canonical value model.
package rt

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
)

type Version [3]uint32

func (a Version) Less(b Version) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

type Error struct {
	Path string
	Code string
}

func (e *Error) Error() string {
	p := e.Path
	if p == "" {
		p = "/"
	}
	return e.Code + " at " + p
}

func Fail(path, code string) error { return &Error{Path: path, Code: code} }

type Ctx struct{ Version Version }

func Child(path, key string) string { return path + "/" + key }

type Obj struct {
	o    *canon.Object
	used map[string]bool
	Path string
}

func NewObj(v any, path string) (*Obj, error) {
	o, ok := v.(*canon.Object)
	if !ok {
		return nil, Fail(path, "ir.not_object")
	}
	return &Obj{o: o, used: map[string]bool{}, Path: path}, nil
}

func (o *Obj) find(key string) (any, bool) {
	v, ok := o.o.Fields[key]
	if ok {
		o.used[key] = true
	}
	return v, ok
}

func (o *Obj) Req(key string) (any, error) {
	v, ok := o.find(key)
	if !ok {
		return nil, Fail(Child(o.Path, key), "ir.missing_field")
	}
	return v, nil
}

func (o *Obj) Opt(key string) (any, bool) { return o.find(key) }

func (o *Obj) Since(key string, since Version, required bool, ctx Ctx) (any, bool, error) {
	v, found := o.find(key)
	if ctx.Version.Less(since) {
		if found {
			return nil, false, Fail(Child(o.Path, key), "ir.field_from_future")
		}
		return nil, false, nil
	}
	if !found && required {
		return nil, false, Fail(Child(o.Path, key), "ir.missing_field")
	}
	return v, found, nil
}

func (o *Obj) Finish() error {
	for _, k := range o.o.Keys {
		if !o.used[k] {
			return Fail(Child(o.Path, k), "ir.unknown_field")
		}
	}
	return nil
}

func Array(v any, path string, min, max int) ([]any, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, Fail(path, "ir.not_array")
	}
	if len(a) > max {
		return nil, Fail(path, "ir.too_many_items")
	}
	if len(a) < min {
		return nil, Fail(path, "ir.too_few_items")
	}
	return a, nil
}

func List[T any](v any, path string, min, max int, f func(any, string) (T, error)) ([]T, error) {
	items, err := Array(v, path, min, max)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(items))
	for i, e := range items {
		x, err := f(e, path+"/"+strconv.Itoa(i))
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

func Map[K ~string, T any](v any, path string, max int, key func(string) (K, error), f func(any, string) (T, error)) (map[K]T, error) {
	o, err := MapEntries(v, path, max)
	if err != nil {
		return nil, err
	}
	out := make(map[K]T, len(o.Keys))
	for _, k := range o.Keys {
		p := Child(path, k)
		kk, err := key(k)
		if err != nil {
			return nil, Fail(p, "ir.bad_key")
		}
		x, err := f(o.Fields[k], p)
		if err != nil {
			return nil, err
		}
		out[kk] = x
	}
	return out, nil
}

func MapEntries(v any, path string, max int) (*canon.Object, error) {
	o, ok := v.(*canon.Object)
	if !ok {
		return nil, Fail(path, "ir.not_object")
	}
	if len(o.Keys) > max {
		return nil, Fail(path, "ir.too_many_items")
	}
	return o, nil
}

func Bool(v any, path string) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, Fail(path, "ir.wrong_type")
	}
	return b, nil
}

func Int(v any, path string, min, max int64) (int64, error) {
	n, ok := v.(int64)
	if !ok {
		return 0, Fail(path, "ir.wrong_type")
	}
	if n < min || n > max {
		return 0, Fail(path, "ir.out_of_range")
	}
	return n, nil
}

const (
	InstantMin int64 = -62135596800000
	InstantMax int64 = 253402300799999
)

func Instant(v any, path string) (int64, error) {
	n, ok := v.(int64)
	if !ok {
		return 0, Fail(path, "ir.wrong_type")
	}
	if n < InstantMin || n > InstantMax {
		return 0, Fail(path, "ir.bad_instant")
	}
	return n, nil
}

type TextMode int

const (
	TextNone TextMode = iota
	TextLabel
	TextMultiline
)

type StrRule struct {
	Pattern *regexp.Regexp
	MaxLen  int
	Text    TextMode
}

func forbiddenChar(r rune, multiline bool) bool {
	if multiline && (r == '\n' || r == '\t') {
		return false
	}
	return r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == 0x2028 || r == 0x2029 ||
		(r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0xfffe || r == 0xffff
}

func CheckString(s, path string, rule StrRule) error {
	if utf8.RuneCountInString(s) > rule.MaxLen {
		return Fail(path, "ir.too_long")
	}
	if rule.Text != TextNone {
		for _, r := range s {
			if forbiddenChar(r, rule.Text == TextMultiline) {
				return Fail(path, "ir.bad_text")
			}
		}
	}
	if rule.Pattern != nil && !rule.Pattern.MatchString(s) {
		return Fail(path, "ir.bad_pattern")
	}
	return nil
}

func String(v any, path string, rule StrRule) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", Fail(path, "ir.wrong_type")
	}
	return s, CheckString(s, path, rule)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func ValidDecimal(s string, scale int) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	neg := strings.HasPrefix(s, "-")
	body := s
	if neg {
		body = s[1:]
	}
	intPart, frac, hasFrac := strings.Cut(body, ".")
	if !isDigits(intPart) || (len(intPart) > 1 && intPart[0] == '0') {
		return false
	}
	var fracOK bool
	switch {
	case !hasFrac:
		fracOK = scale <= 0
	case scale > 0:
		fracOK = len(frac) == scale && isDigits(frac)
	case scale < 0:
		fracOK = isDigits(frac) && !strings.HasSuffix(frac, "0")
	default:
		fracOK = false
	}
	if !fracOK {
		return false
	}
	zero := strings.Trim(body, "0.") == ""
	return !(neg && zero)
}

// Decimal validates with scale -1 meaning "minimal form, any scale".
func Decimal(v any, path string, scale int) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", Fail(path, "ir.wrong_type")
	}
	if !ValidDecimal(s, scale) {
		return "", Fail(path, "ir.bad_decimal")
	}
	return s, nil
}

func ValidDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' || !isDigits(s[0:4]) || !isDigits(s[5:7]) || !isDigits(s[8:10]) {
		return false
	}
	atoi := func(t string) int {
		n := 0
		for i := 0; i < len(t); i++ {
			n = n*10 + int(t[i]-'0')
		}
		return n
	}
	y, m, d := atoi(s[0:4]), atoi(s[5:7]), atoi(s[8:10])
	leap := (y%4 == 0 && y%100 != 0) || y%400 == 0
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if leap {
		days[1] = 29
	}
	return y >= 1 && m >= 1 && m <= 12 && d >= 1 && d <= days[m-1]
}

func Date(v any, path string) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", Fail(path, "ir.wrong_type")
	}
	if !ValidDate(s) {
		return "", Fail(path, "ir.bad_date")
	}
	return s, nil
}

func EnumStr(v any, path string) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", Fail(path, "ir.wrong_type")
	}
	return s, nil
}

var versionRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func ParseVersion(s string) (Version, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, false
	}
	var v Version
	for i := 0; i < 3; i++ {
		n := uint64(0)
		for _, c := range m[i+1] {
			n = n*10 + uint64(c-'0')
			if n > 0xffffffff {
				return Version{}, false
			}
		}
		v[i] = uint32(n)
	}
	return v, true
}
