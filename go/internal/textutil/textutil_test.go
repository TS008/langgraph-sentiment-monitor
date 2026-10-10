package textutil

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateKeepsValidUTF8(t *testing.T) {
	s := "微博热搜：某品牌宕机" // 3 bytes per rune
	for n := 0; n <= len(s); n++ {
		got := Truncate(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("n=%d produced invalid UTF-8: %q", n, got)
		}
		if body := len(got) - len("…"); n < len(s) && body > n {
			t.Fatalf("n=%d exceeded byte budget: %d", n, body)
		}
	}
	if got := Truncate("short", 10); got != "short" {
		t.Fatalf("got %q", got)
	}
	if got := Truncate("abcdef", 3); got != "abc…" {
		t.Fatalf("got %q", got)
	}
}
