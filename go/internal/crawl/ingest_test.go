package crawl

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestStripHTML(t *testing.T) {
	cases := map[string]string{
		"<p>Hello <b>world</b></p>":     "Hello world",
		"line<br>break<br/>end":         "line break end",
		"a > b and c":                   "a > b and c",
		"keep <unclosed tail":           "keep <unclosed tail",
		"x<y>z <tail":                   "x z <tail",
		"<div>宕机 <span>危机</span></div>": "宕机 危机",
	}
	for in, want := range cases {
		if got := stripHTML(in); got != want {
			t.Errorf("stripHTML(%q)=%q want %q", in, got, want)
		}
	}
}

func TestStripHTMLIsLinear(t *testing.T) {
	in := strings.Repeat("<span>word</span> ", 50000) // ~900KB, 100k tags
	start := time.Now()
	out := stripHTML(in)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("stripHTML took %v on %d bytes", elapsed, len(in))
	}
	if strings.Contains(out, "<") || !strings.HasPrefix(out, "word word") {
		t.Fatalf("unexpected output prefix %q", out[:20])
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	got := truncate("  热搜标题  ", 4)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
}
