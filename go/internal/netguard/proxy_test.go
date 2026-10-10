package netguard

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/netguard/netguardtest"
)

var loopbackOnly = Policy{AllowCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}

func startProxy(t *testing.T, p Policy) *url.URL {
	t.Helper()
	x, err := StartProxy(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })
	u, _ := url.Parse(x.URL())
	return u
}

func proxiedClient(proxy *url.URL) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxy),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func TestProxyForwardsAllowedHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("public"))
	}))
	defer srv.Close()

	resp, err := proxiedClient(startProxy(t, loopbackOnly)).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(b) != "public" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, b)
	}
}

func TestProxyBlocksInternalHTTP(t *testing.T) {
	internal := netguardtest.NewLoopbackServer(t, "127.0.0.2", "secret")
	resp, err := proxiedClient(startProxy(t, loopbackOnly)).Get(internal)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get(BlockedHeader) == "" || strings.Contains(string(b), "secret") {
		t.Fatalf("status=%d blocked=%q body=%q", resp.StatusCode, resp.Header.Get(BlockedHeader), b)
	}
}

func TestProxyTunnelsAllowedConnectAndBlocksInternal(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("tls-public"))
	}))
	defer srv.Close()
	proxy := startProxy(t, loopbackOnly)

	resp, err := proxiedClient(proxy).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "tls-public" {
		t.Fatalf("tunnel body=%q", b)
	}

	ln, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("cannot listen on 127.0.0.2: %v", err)
	}
	internal := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("secret"))
	}))
	internal.Listener = ln
	internal.StartTLS()
	defer internal.Close()

	if resp, err := proxiedClient(proxy).Get(internal.URL); err == nil {
		resp.Body.Close()
		t.Fatalf("CONNECT to 127.0.0.2 should fail, got %d", resp.StatusCode)
	}
}

// The name passes ValidateURL (127.0.0.1), then rebinds to 127.0.0.2 before the
// proxy connects. The dial-time check must catch it.
func TestProxyBlocksDNSRebinding(t *testing.T) {
	internal := netguardtest.NewLoopbackServer(t, "127.0.0.2", "secret")
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(internal, "http://"))
	dns := &netguardtest.RebindingDNS{
		Host:   "rebind.test",
		First:  netip.MustParseAddr("127.0.0.1"),
		Then:   netip.MustParseAddr("127.0.0.2"),
		Switch: 1,
	}
	p := loopbackOnly
	p.Resolver = dns.Resolver(t)
	target := "http://rebind.test:" + port + "/"

	if _, err := p.ValidateURL(context.Background(), target); err != nil {
		t.Fatalf("first lookup should look allowed: %v", err)
	}
	resp, err := proxiedClient(startProxy(t, p)).Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || strings.Contains(string(b), "secret") {
		t.Fatalf("rebinding not blocked: status=%d body=%q", resp.StatusCode, b)
	}
	if dns.Lookups() < 2 {
		t.Fatalf("proxy did not re-resolve at dial time (lookups=%d)", dns.Lookups())
	}
}

func TestProxyCloseTearsDownTunnels(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		c, err := upstream.Accept()
		if err == nil {
			defer c.Close()
			io.Copy(io.Discard, c)
		}
	}()

	x, err := StartProxy(loopbackOnly)
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", strings.TrimPrefix(x.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	addr := upstream.Addr().String()
	io.WriteString(c, "CONNECT "+addr+" HTTP/1.1\r\nHost: "+addr+"\r\n\r\n")
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	if n, err := c.Read(buf); err != nil || !strings.Contains(string(buf[:n]), "200") {
		t.Fatalf("CONNECT failed: %q %v", buf[:n], err)
	}

	done := make(chan struct{})
	go func() { x.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung on an open tunnel")
	}
	if _, err := c.Read(buf); err == nil {
		t.Fatal("tunnel should be closed")
	}
}
