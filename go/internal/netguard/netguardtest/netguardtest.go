// Package netguardtest provides SSRF test fixtures.
package netguardtest

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
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

// RebindingDNS is a fake DNS server that answers A queries for one host with
// First for the first Switch lookups and Then afterwards, simulating rebinding.
type RebindingDNS struct {
	Host        string
	First, Then netip.Addr
	Switch      int64

	lookups atomic.Int64
}

// Lookups is the number of A queries answered for Host so far.
func (d *RebindingDNS) Lookups() int64 { return d.lookups.Load() }

// Resolver starts the server and returns a pure-Go resolver that only talks to it.
func (d *RebindingDNS) Resolver(t testing.TB) *net.Resolver {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go d.serve(pc)
	addr := pc.LocalAddr().String()
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dl net.Dialer
			return dl.DialContext(ctx, "udp", addr)
		},
	}
}

func (d *RebindingDNS) serve(pc net.PacketConn) {
	buf := make([]byte, 1500)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		if resp, ok := d.answer(buf[:n]); ok {
			_, _ = pc.WriteTo(resp, from)
		}
	}
}

func (d *RebindingDNS) answer(query []byte) ([]byte, bool) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil, false
	}
	q, err := p.Question()
	if err != nil {
		return nil, false
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true})
	b.EnableCompression()
	name := strings.TrimSuffix(q.Name.String(), ".")
	if !strings.EqualFold(name, d.Host) {
		b = dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, RCode: dnsmessage.RCodeNameError})
	}
	_ = b.StartQuestions()
	_ = b.Question(q)
	_ = b.StartAnswers()
	if strings.EqualFold(name, d.Host) && q.Type == dnsmessage.TypeA {
		ip := d.Then
		if d.lookups.Add(1) <= d.Switch {
			ip = d.First
		}
		hdr := dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 0}
		_ = b.AResource(hdr, dnsmessage.AResource{A: ip.As4()})
	}
	resp, err := b.Finish()
	return resp, err == nil
}
