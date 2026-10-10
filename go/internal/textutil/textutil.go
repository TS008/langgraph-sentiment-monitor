// Package textutil holds UTF-8-safe string helpers shared across ingest and prompts.
package textutil

import "unicode/utf8"

// Truncate shortens s to at most n bytes (plus "…") without splitting a UTF-8
// rune, so CJK text never ends in an invalid byte sequence.
func Truncate(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
