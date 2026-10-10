package crawl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/netguard"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/netguard/netguardtest"
)

// loopbackOnly lets tests reach httptest servers on 127.0.0.1 while every other
// private address (127.0.0.2 included) stays blocked.
var loopbackOnly = netguard.Policy{AllowCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}

func requireChrome(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("google-chrome"); err != nil {
		if _, err := exec.LookPath("chromium"); err != nil {
			t.Skip("no Chrome/Chromium on PATH")
		}
	}
}

func TestFetchPageBlocksPrivateURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<title>secret</title>"))
	}))
	defer srv.Close()

	ing := NewIngestor(1, netguard.Policy{})
	for _, js := range []bool{false, true} {
		if _, err := ing.FetchPage(context.Background(), srv.URL, js); !errors.Is(err, netguard.ErrBlocked) {
			t.Fatalf("js=%v: loopback page should be blocked, got %v", js, err)
		}
	}
	if _, err := ing.FetchPage(context.Background(), "file:///etc/passwd", false); !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("file scheme should be blocked, got %v", err)
	}
}

func TestFetchHTMLBlocksRedirectToInternalHost(t *testing.T) {
	internal := netguardtest.NewLoopbackServer(t, "127.0.0.2", "secret")
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal, http.StatusFound)
	}))
	defer redirector.Close()

	sig, err := NewIngestor(1, loopbackOnly).FetchPage(context.Background(), redirector.URL, false)
	if err == nil {
		t.Fatalf("redirect to 127.0.0.2 should fail, got signal %+v", sig)
	}
	if strings.Contains(sig.Title+sig.Snippet, "secret") {
		t.Fatal("internal content leaked")
	}
}

func TestFetchHTMLJSBlocksRedirectToInternalHost(t *testing.T) {
	requireChrome(t)
	defer CloseBrowser()
	internal := netguardtest.NewLoopbackServer(t, "127.0.0.2", "secret")
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal, http.StatusFound)
	}))
	defer redirector.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sig, err := FetchHTMLJS(ctx, redirector.URL, loopbackOnly)
	if strings.Contains(sig.Title+sig.Snippet, "secret") {
		t.Fatalf("internal content leaked via redirect: %+v", sig)
	}
	if err == nil {
		t.Fatal("expected navigation to be blocked")
	}
}

// rebind.test passes the pre-flight and Fetch-guard lookups as 127.0.0.1, then
// resolves to 127.0.0.2 when the egress proxy dials. Chrome cannot resolve the
// name on its own, so any successful load would have to come through the proxy.
func TestFetchHTMLJSBlocksDNSRebinding(t *testing.T) {
	requireChrome(t)
	defer CloseBrowser()
	internal := netguardtest.NewLoopbackServer(t, "127.0.0.2", "secret")
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(internal, "http://"))
	dns := &netguardtest.RebindingDNS{
		Host:   "rebind.test",
		First:  netip.MustParseAddr("127.0.0.1"),
		Then:   netip.MustParseAddr("127.0.0.2"),
		Switch: 2,
	}
	p := loopbackOnly
	p.Resolver = dns.Resolver(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sig, err := FetchHTMLJS(ctx, "http://rebind.test:"+port+"/", p)
	if strings.Contains(sig.Title+sig.Snippet, "secret") {
		t.Fatalf("internal content leaked via rebinding: %+v", sig)
	}
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
	if dns.Lookups() < 3 {
		t.Fatalf("egress proxy did not resolve at dial time (lookups=%d)", dns.Lookups())
	}
}

// Control for the rebinding test: a stable name served on the allowlisted address
// renders through the proxy, proving Chrome's traffic really goes through it.
func TestFetchHTMLJSResolvesThroughEgressProxy(t *testing.T) {
	requireChrome(t)
	defer CloseBrowser()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<html><head><title>via proxy</title></head><body>ok</body></html>"))
	}))
	defer page.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(page.URL, "http://"))
	ip := netip.MustParseAddr("127.0.0.1")
	dns := &netguardtest.RebindingDNS{Host: "stable.test", First: ip, Then: ip}
	p := loopbackOnly
	p.Resolver = dns.Resolver(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sig, err := FetchHTMLJS(ctx, "http://stable.test:"+port+"/", p)
	if err != nil {
		t.Fatal(err)
	}
	if sig.Title != "via proxy" {
		t.Fatalf("title=%q", sig.Title)
	}
}

func TestFetchHTMLJSBlocksSubresourceToInternalHost(t *testing.T) {
	requireChrome(t)
	defer CloseBrowser()

	ln, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("cannot listen on 127.0.0.2: %v", err)
	}
	internal := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write([]byte("secret"))
	}))
	internal.Listener = ln
	internal.Start()
	defer internal.Close()

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Synchronous XHR so the result is in the DOM before FetchHTMLJS reads it.
		fmt.Fprintf(w, `<html><head><title>page</title></head><body><script>
var out = "blocked";
try { var x = new XMLHttpRequest(); x.open("GET", %q, false); x.send(); out = "leaked:" + x.responseText; } catch (e) {}
document.body.innerText = out;
</script></body></html>`, internal.URL)
	}))
	defer page.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sig, err := FetchHTMLJS(ctx, page.URL, loopbackOnly)
	if err != nil {
		t.Fatal(err)
	}
	if sig.Snippet != "blocked" {
		t.Fatalf("subresource to internal host should be blocked, page saw %q", sig.Snippet)
	}

	// Sanity check: with private access allowed, the same page does reach it,
	// so the assertion above is testing the guard and not CORS or PNA.
	sig, err = FetchHTMLJS(ctx, page.URL, netguard.Policy{AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	if sig.Snippet != "leaked:secret" {
		t.Fatalf("control run expected leaked:secret, got %q", sig.Snippet)
	}
}
