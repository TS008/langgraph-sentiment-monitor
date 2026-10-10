package netguard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/netguard/netguardtest"
)

func TestCheckIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "0.0.0.0", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1",
		"::ffff:169.254.169.254", "224.0.0.1", "255.255.255.255", "64:ff9b::a9fe:a9fe",
	}
	allowed := []string{"1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"}
	var p Policy
	for _, s := range blocked {
		if err := p.CheckIP(netip.MustParseAddr(s)); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s should be blocked, got %v", s, err)
		}
	}
	for _, s := range allowed {
		if err := p.CheckIP(netip.MustParseAddr(s)); err != nil {
			t.Errorf("%s should be allowed, got %v", s, err)
		}
	}
	if err := (Policy{AllowPrivate: true}).CheckIP(netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Errorf("AllowPrivate should permit loopback: %v", err)
	}
}

func TestAllowCIDRs(t *testing.T) {
	allow, err := ParseCIDRs([]string{"127.0.0.1", "10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{AllowCIDRs: allow}
	for _, s := range []string{"127.0.0.1", "10.9.9.9"} {
		if err := p.CheckIP(netip.MustParseAddr(s)); err != nil {
			t.Errorf("%s should be allowed: %v", s, err)
		}
	}
	for _, s := range []string{"127.0.0.2", "169.254.169.254"} {
		if err := p.CheckIP(netip.MustParseAddr(s)); err == nil {
			t.Errorf("%s should still be blocked", s)
		}
	}
	if _, err := ParseCIDRs([]string{"nope"}); err == nil {
		t.Error("invalid entry should error")
	}
}

func TestValidateURL(t *testing.T) {
	var p Policy
	ctx := context.Background()
	bad := []string{
		"file:///etc/passwd", "gopher://x", "http://127.0.0.1/", "http://[::1]:8080/",
		"http://169.254.169.254/latest/meta-data/", "http://localhost/", "http://user:pw@1.1.1.1/",
		"http:///nohost",
	}
	for _, raw := range bad {
		if _, err := p.ValidateURL(ctx, raw); err == nil {
			t.Errorf("%s should be rejected", raw)
		}
	}
	if _, err := p.ValidateURL(ctx, "https://1.1.1.1/status"); err != nil {
		t.Errorf("public IP url rejected: %v", err)
	}
}

func TestTransportBlocksPrivateDestinationsAtDialTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	client := &http.Client{Transport: Policy{}.Transport()}
	if _, err := client.Get(srv.URL); !errors.Is(err, ErrBlocked) {
		t.Fatalf("loopback dial should be blocked, got %v", err)
	}

	open := &http.Client{Transport: Policy{AllowPrivate: true}.Transport()}
	resp, err := open.Get(srv.URL)
	if err != nil {
		t.Fatalf("AllowPrivate dial failed: %v", err)
	}
	resp.Body.Close()
}

func TestTransportBlocksRedirectToInternalHost(t *testing.T) {
	internal := netguardtest.NewLoopbackServer(t, "127.0.0.2", "secret")
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal, http.StatusFound)
	}))
	defer redirector.Close()

	p := Policy{AllowCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}
	client := &http.Client{Transport: p.Transport()}
	if _, err := client.Get(redirector.URL); !errors.Is(err, ErrBlocked) {
		t.Fatalf("redirect to 127.0.0.2 should be blocked, got %v", err)
	}
}
