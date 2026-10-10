//go:build integration

package newsnow

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
)

func TestNewsNowLiveIDs(t *testing.T) {
	if os.Getenv("AEGIS_INTEGRATION") != "1" {
		t.Skip("set AEGIS_INTEGRATION=1")
	}
	cfg := config.Load()
	c := NewClient(cfg.NewsNowBaseURL, 0)
	ids := cfg.NewsNowIDs
	if len(ids) == 0 {
		ids = []string{"weibo", "zhihu", "douyin", "baidu", "toutiao", "thepaper"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var ok, fail int
	for _, id := range ids {
		sigs, err := c.fetchOne(ctx, id)
		if err != nil {
			fail++
			t.Logf("FAIL %s: %v", id, err)
			continue
		}
		ok++
		t.Logf("OK   %s: %d items", id, len(sigs))
	}
	t.Logf("NewsNow summary at %s: %d/%d ids succeeded", cfg.NewsNowBaseURL, ok, len(ids))
	if ok == 0 {
		t.Fatalf("no newsnow ids succeeded (start self-hosted: docker compose up -d newsnow)")
	}
}
