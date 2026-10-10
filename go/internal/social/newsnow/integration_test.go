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
	ids := []string{"weibo", "zhihu", "douyin", "baidu", "toutiao", "thepaper"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, id := range ids {
		sigs, err := c.fetchOne(ctx, id)
		if err != nil {
			t.Logf("id %s: FAIL %v", id, err)
			continue
		}
		t.Logf("id %s: OK %d items", id, len(sigs))
	}
}
