package newsnow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func largeFixture(id string, n int) []byte {
	type item struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		URL   string `json:"url"`
		Extra struct {
			Info string `json:"info"`
		} `json:"extra"`
	}
	items := make([]item, n)
	for i := range items {
		items[i].ID = fmt.Sprint(i)
		items[i].Title = fmt.Sprintf("热搜标题第%d条：某品牌发布会引发网友热议与大量讨论", i)
		items[i].URL = fmt.Sprintf("https://example.com/%s/%d?utm_source=newsnow&ref=hotlist", id, i)
		items[i].Extra.Info = fmt.Sprintf("%d万热度", 1000-i)
	}
	b, _ := json.Marshal(map[string]interface{}{"status": "success", "id": id, "items": items})
	return b
}

func TestFetchHandlesRealisticPayloadSize(t *testing.T) {
	body := largeFixture("zhihu", 120)
	if len(body) < 16<<10 {
		t.Fatalf("fixture too small to exercise limits: %d bytes", len(body))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	sigs, errs := NewClient(srv.URL, 0).FetchIDs(context.Background(), []string{"zhihu"})
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if len(sigs) != 120 {
		t.Fatalf("signals %d, want 120", len(sigs))
	}
}

func TestFetchRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(make([]byte, MaxBodyBytes+10))
	}))
	defer srv.Close()

	_, errs := NewClient(srv.URL, 0).FetchIDs(context.Background(), []string{"x"})
	if len(errs) != 1 {
		t.Fatalf("expected size error, got %v", errs)
	}
}

func TestFetchIDsRunsConcurrentlyAndKeepsOrder(t *testing.T) {
	const delay = 150 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Write(largeFixture(r.URL.Query().Get("id"), 3))
	}))
	defer srv.Close()

	ids := []string{"a", "b", "c", "d"}
	c := NewClient(srv.URL, 0)
	start := time.Now()
	sigs, errs := c.FetchIDs(context.Background(), ids)
	elapsed := time.Since(start)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if elapsed > 3*delay {
		t.Fatalf("fetch took %v; expected parallel (< %v)", elapsed, 3*delay)
	}
	for i, id := range ids {
		if got := sigs[i*3].Source; got != "newsnow:"+id {
			t.Fatalf("order: sigs[%d].Source=%s want newsnow:%s", i*3, got, id)
		}
	}
}

func TestConcurrentCallersShareOneRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(fixtureWeibo))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 10*time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sigs, errs := c.FetchIDs(context.Background(), []string{"weibo"}); len(errs) != 0 || len(sigs) != 2 {
				t.Errorf("sigs=%d errs=%v", len(sigs), errs)
			}
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected 1 upstream call, got %d", n)
	}
}
