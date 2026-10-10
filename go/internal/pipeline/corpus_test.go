package pipeline

import (
	"strings"
	"testing"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
)

func TestBuildCorpusSamplesAcrossSources(t *testing.T) {
	var signals []crawl.Signal
	for i := 0; i < 30; i++ {
		signals = append(signals, crawl.Signal{Source: "rss:hn", Title: "hn"})
	}
	for i := 0; i < 30; i++ {
		signals = append(signals, crawl.Signal{Source: "newsnow:weibo", Title: "wb"})
	}
	signals = append(signals, crawl.Signal{Source: "html", Title: "page"})

	corpus := buildCorpus(signals)
	lines := strings.Split(strings.TrimSpace(corpus), "\n")
	if len(lines) != maxCorpusSignals {
		t.Fatalf("lines=%d want %d", len(lines), maxCorpusSignals)
	}
	for _, src := range []string{"[rss:hn]", "[newsnow:weibo]", "[html]"} {
		if !strings.Contains(corpus, src) {
			t.Fatalf("corpus missing %s:\n%s", src, corpus)
		}
	}
	if !strings.HasPrefix(lines[0], "- [rss:hn]") || !strings.HasPrefix(lines[1], "- [newsnow:weibo]") {
		t.Fatalf("expected round-robin order, got %q / %q", lines[0], lines[1])
	}
}

func TestBuildCorpusFewSignals(t *testing.T) {
	corpus := buildCorpus([]crawl.Signal{{Source: "a", Title: "1"}, {Source: "a", Title: "2"}})
	if n := strings.Count(corpus, "\n"); n != 2 {
		t.Fatalf("lines=%d", n)
	}
}
