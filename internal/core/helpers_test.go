package core

import "pgregory.net/rapid"

// anyRune draws digits and punctuation the parser cares about often, any rune otherwise.
func anyRune() *rapid.Generator[rune] {
	return rapid.OneOf(rapid.RuneFrom([]rune("0123456789,.:+-%#(){}\" \r\n٣")), rapid.Rune())
}
