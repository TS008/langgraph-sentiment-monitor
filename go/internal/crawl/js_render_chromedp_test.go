package crawl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestFetchHTMLJSSharesBrowserAcrossConcurrentCalls(t *testing.T) {
	if _, err := exec.LookPath("google-chrome"); err != nil {
		if _, err := exec.LookPath("chromium"); err != nil {
			t.Skip("no Chrome/Chromium on PATH")
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><head><title>page %s</title></head><body><script>document.body.innerText="rendered %s"</script></body></html>`, r.URL.Path, r.URL.Path)
	}))
	defer srv.Close()
	defer CloseBrowser()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	sigs := make([]Signal, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sigs[i], errs[i] = FetchHTMLJS(ctx, fmt.Sprintf("%s/p%d", srv.URL, i))
		}(i)
	}
	wg.Wait()

	first := browserCtx
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("fetch %d: %v", i, errs[i])
		}
		if want := fmt.Sprintf("rendered /p%d", i); sigs[i].Snippet != want {
			t.Fatalf("fetch %d snippet=%q want %q", i, sigs[i].Snippet, want)
		}
	}
	if _, err := FetchHTMLJS(ctx, srv.URL+"/again"); err != nil {
		t.Fatal(err)
	}
	if browserCtx != first {
		t.Fatal("expected the browser to be reused, not restarted")
	}
}

func TestFetchHTMLJSHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchHTMLJS(ctx, "http://127.0.0.1:1/"); err == nil {
		t.Fatal("expected cancellation error")
	}
}
