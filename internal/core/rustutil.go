package core

// Small helpers reproducing Rust std semantics the port depends on.

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Regex fragments for Rust `regex`'s Unicode-aware classes (Go's are ASCII-only).
const (
	// reDigit is Rust's Unicode `\d` (`\p{Nd}`).
	reDigit = `\p{Nd}`
	// reSpace is Rust's Unicode `\s` (the White_Space property).
	reSpace = `[\t\n\v\f\r \x{85}\x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}]`
)

func ptr[T any](v T) *T { return &v }

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// trimSpace is `str::trim` (Unicode White_Space, same set as unicode.IsSpace).
func trimSpace(s string) string { return strings.TrimSpace(s) }

// asciiLower is `str::to_ascii_lowercase`.
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// asciiEqualFold is `str::eq_ignore_ascii_case`.
func asciiEqualFold(a, b string) bool {
	return len(a) == len(b) && asciiLower(a) == asciiLower(b)
}

func cutSuffix(s, suffix string) (string, bool) { return strings.CutSuffix(s, suffix) }

// rustLines is `str::lines`: split on `\n`, drop one trailing `\r` per line, and no final
// empty line after a trailing newline.
func rustLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// parseUint is Rust's `str::parse::<uN>()`: optional `+`, ASCII digits only, no overflow.
func parseUint(s string, bits int) (uint64, bool) {
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, bits)
	return n, err == nil
}

func parseU32(s string) (uint32, bool) {
	n, ok := parseUint(s, 32)
	return uint32(n), ok
}

func parseU8(s string) (uint8, bool) {
	n, ok := parseUint(s, 8)
	return uint8(n), ok
}

func parseUsize(s string) (int, bool) {
	n, ok := parseUint(s, 63)
	return int(n), ok
}

// parseF64 is Rust's `str::parse::<f64>()`: decimal digits with optional sign, fraction and
// exponent, or inf/infinity/nan; no hex floats or digit separators (which Go accepts).
func parseF64(s string) (float64, bool) {
	body := strings.TrimLeft(s, "+-")
	if len(s)-len(body) > 1 || body == "" {
		return 0, false
	}
	switch asciiLower(body) {
	case "inf", "infinity", "nan":
	default:
		for i := 0; i < len(body); i++ {
			c := body[i]
			if (c < '0' || c > '9') && c != '.' && c != 'e' && c != 'E' && c != '+' && c != '-' {
				return 0, false
			}
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// Rust saturates out-of-range values to infinity / zero rather than failing.
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f, true
		}
		return 0, false
	}
	return f, true
}

// fract is `f64::fract`.
func fract(v float64) float64 { return v - math.Trunc(v) }

// isWordRune is Rust regex's Unicode `\w`: Alphabetic, marks, decimal digits, connector
// punctuation and Join_Control.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Other_Alphabetic, r) ||
		unicode.IsMark(r) || unicode.Is(unicode.Nd, r) || unicode.Is(unicode.Pc, r) ||
		unicode.Is(unicode.Join_Control, r)
}

// wordBefore reports whether the rune ending at byte i of s is a word character.
func wordBefore(s string, i int) bool {
	if i <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return isWordRune(r)
}

// wordAt reports whether the rune starting at byte i of s is a word character.
func wordAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isWordRune(r)
}

// replaceAllSubmatchFunc is `Regex::replace_all` with a closure over the captures
// (`idx` holds submatch byte offsets, -1 for groups that did not participate).
func replaceAllSubmatchFunc(re interface {
	FindAllStringSubmatchIndex(string, int) [][]int
}, s string, f func(idx []int) string) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(s[last:m[0]])
		b.WriteString(f(m))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// group returns submatch n of s for an index slice, and whether it participated.
func group(s string, idx []int, n int) (string, bool) {
	if 2*n+1 >= len(idx) || idx[2*n] < 0 {
		return "", false
	}
	return s[idx[2*n]:idx[2*n+1]], true
}

// formatF64Display is Rust's `{}` for f64: shortest round-trip digits, never an exponent.
func formatF64Display(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	}
	if v == 0 && math.Signbit(v) {
		return "-0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// f64ToI64 is Rust's saturating `as i64` cast.
func f64ToI64(v float64) int64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= math.MaxInt64:
		return math.MaxInt64
	case v <= math.MinInt64:
		return math.MinInt64
	}
	return int64(v)
}

func formatI64(v int64) string { return strconv.FormatInt(v, 10) }

func isSpaceRune(r rune) bool { return unicode.IsSpace(r) }
