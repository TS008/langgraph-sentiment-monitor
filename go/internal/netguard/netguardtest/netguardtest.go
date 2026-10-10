// Package netguardtest provides SSRF test fixtures.
package netguardtest

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// NewLoopbackServer serves body on ip (e.g. 127.0.0.2) and returns its URL. Tests use
// it as an "internal" target distinct from an allowlisted 127.0.0.1 server.
func NewLoopbackServer(t testing.TB, ip, body string) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Skipf("cannot listen on %s: %v", ip, err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><head><title>" + body + "</title></head><body>" + body + "</body></html>"))
	}))
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}
