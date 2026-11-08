package core

// Regular-expression helpers. Go's `\d`, `\s` and `\b` are ASCII-only; item text comes from
// game clients in many languages, so the patterns use the Unicode classes below and check
// word boundaries by hand.

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// reDigit is a Unicode decimal digit (`\p{Nd}`).
	reDigit = `\p{Nd}`
	// reSpace is a Unicode White_Space character.
	reSpace = `[\t\n\v\f\r \x{85}\x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}]`
)

// isWordRune reports whether r is a Unicode word character: alphabetic, marks, decimal
// digits, connector punctuation and Join_Control.
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

// replaceAllSubmatchFunc replaces every match of re in s with f's result; f gets the
// submatch byte offsets (-1 for groups that did not participate).
func replaceAllSubmatchFunc(re *regexp.Regexp, s string, f func(idx []int) string) string {
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
