// Package netguard blocks server-side fetches of user-supplied URLs from reaching
// internal networks (SSRF): loopback, private ranges, link-local/cloud metadata, etc.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// ErrBlocked is returned when a URL or address targets a disallowed network.
var ErrBlocked = errors.New("netguard: destination not allowed")

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can map to internal IPv4
	netip.MustParsePrefix("2001:db8::/32"),
}

// Policy decides which destinations user-supplied URLs may reach.
type Policy struct {
	// AllowPrivate disables IP checks (local development against localhost pages).
	AllowPrivate bool
	// AllowCIDRs explicitly permits ranges that would otherwise be blocked
	// (e.g. one internal status page).
	AllowCIDRs []netip.Prefix
	// Resolver is used by ValidateURL; nil means net.DefaultResolver.
	Resolver *net.Resolver
}

// ParseCIDRs parses a comma-separated list of CIDRs or bare IPs.
func ParseCIDRs(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range list {
		if s == "" {
			continue
		}
		if pfx, err := netip.ParsePrefix(s); err == nil {
			out = append(out, pfx.Masked())
			continue
		}
		ip, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR or IP %q", s)
		}
		out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
	}
	return out, nil
}

// CheckIP reports whether ip is a public unicast address or explicitly allowed.
func (p Policy) CheckIP(ip netip.Addr) error {
	if p.AllowPrivate {
		return nil
	}
	ip = ip.Unmap()
	for _, pfx := range p.AllowCIDRs {
		if pfx.Contains(ip) {
			return nil
		}
	}
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return fmt.Errorf("%w: %s", ErrBlocked, ip)
	}
	for _, pfx := range blockedPrefixes {
		if pfx.Contains(ip) {
			return fmt.Errorf("%w: %s", ErrBlocked, ip)
		}
	}
	return nil
}

// ValidateURL checks scheme and that every address the host resolves to is allowed.
// It gives callers an early, descriptive error; dial-time checks remain authoritative.
func (p Policy) ValidateURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: scheme %q (only http/https)", ErrBlocked, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("invalid url: missing host")
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: credentials in url", ErrBlocked)
	}
	if p.AllowPrivate {
		return u, nil
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return u, p.CheckIP(ip)
	}
	res := p.Resolver
	if res == nil {
		res = net.DefaultResolver
	}
	addrs, err := res.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	for _, a := range addrs {
		if err := p.CheckIP(a); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// Dialer returns a dialer that re-checks the resolved address at connect time,
// which also covers redirects and DNS rebinding.
func (p Policy) Dialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("%w: unparsable address %q", ErrBlocked, host)
			}
			return p.CheckIP(ip)
		},
	}
}

// Transport is an http.Transport whose connections pass the policy. Proxies are
// disabled because a proxy would make the dial-time check meaningless.
func (p Policy) Transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = p.Dialer().DialContext
	return t
}
