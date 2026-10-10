package newsnow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const fixtureWeibo = `{
  "status": "success",
  "id": "weibo",
  "updatedTime": 1700000000000,
  "items": [
    {"id": "1", "title": "热搜一", "url": "https://s.weibo.com/weibo?q=1", "extra": {"info": "999万"}},
    {"id": "2", "title": "热搜二", "url": "https://s.weibo.com/weibo?q=2", "extra": {"info": "500万"}}
  ]
}`

func TestClientFetchIDsHermetic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != DefaultUserAgent {
			t.Errorf("ua: %q", r.Header.Get("User-Agent"))
		}
		id := r.URL.Query().Get("id")
		switch id {
		case "weibo":
			w.Write([]byte(fixtureWeibo))
		case "zhihu":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("<!DOCTYPE html><html>blocked</html>"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 0)
	sigs, errs := c.FetchIDs(context.Background(), []string{"weibo", "zhihu"})
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %v", errs)
	}
	if len(sigs) != 2 {
		t.Fatalf("signals %d", len(sigs))
	}
	if sigs[0].Rank != 1 || sigs[0].Heat != "999万" || sigs[0].Source != "newsnow:weibo" {
		t.Fatalf("first signal: %+v", sigs[0])
	}
}

func TestClientRateLimitUsesCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Write([]byte(fixtureWeibo))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 10*time.Minute)
	ctx := context.Background()
	_, _ = c.FetchIDs(ctx, []string{"weibo"})
	_, _ = c.FetchIDs(ctx, []string{"weibo"})
	if calls != 1 {
		t.Fatalf("expected 1 http call due to cache, got %d", calls)
	}
}

func TestSummarizeResponseNoHTMLDump(t *testing.T) {
	msg := summarizeResponse(403, "text/html", []byte("<!DOCTYPE html><title>blocked</title>"))
	if strings.Contains(msg, "<!DOCTYPE") {
		t.Fatal("should not embed HTML")
	}
	if !strings.Contains(msg, "403") {
		t.Fatal(msg)
	}
}

func TestParseResponse(t *testing.T) {
	sigs, err := ParseResponse("weibo", []byte(fixtureWeibo), time.Unix(0, 0).UTC())
	if err != nil || len(sigs) != 2 {
		t.Fatalf("parse: %v len=%d", err, len(sigs))
	}
}
