package netguard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"
)

// BlockedHeader marks proxy responses for requests the policy refused, so callers
// can tell a blocked plain-HTTP navigation from a real upstream page.
const BlockedHeader = "X-Netguard-Blocked"

// Proxy is a loopback HTTP forward proxy (plain HTTP and CONNECT) whose upstream
// connections are made with Policy.Dialer. Pointing a browser at it moves DNS
// resolution and the IP check to connect time, closing the rebinding window that
// exists when the browser resolves names itself.
type Proxy struct {
	policy Policy
	dialer *net.Dialer
	ln     net.Listener
	srv    *http.Server
	fwd    *httputil.ReverseProxy

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex // guards closed and wg.Add against Close's wg.Wait
	closed bool
	wg     sync.WaitGroup
}

// StartProxy listens on an ephemeral 127.0.0.1 port.
func StartProxy(p Policy) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	x := &Proxy{policy: p, dialer: p.Dialer(), ln: ln, ctx: ctx, cancel: cancel}
	t := p.Transport()
	t.MaxIdleConnsPerHost = 4
	x.fwd = &httputil.ReverseProxy{
		// Forward-proxy requests already carry the absolute upstream URL.
		Rewrite:      func(*httputil.ProxyRequest) {},
		Transport:    t,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) { writeProxyError(w, err) },
	}
	x.srv = &http.Server{
		Handler:           x,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() { _ = x.srv.Serve(ln) }()
	return x, nil
}

// URL is the proxy address in the form Chrome's proxy settings expect.
func (x *Proxy) URL() string { return "http://" + x.ln.Addr().String() }

// Close stops the listener and tears down open tunnels.
func (x *Proxy) Close() error {
	x.mu.Lock()
	x.closed = true
	x.mu.Unlock()
	x.cancel()
	err := x.srv.Close()
	x.wg.Wait()
	return err
}

func (x *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		x.tunnel(w, r)
		return
	}
	if !r.URL.IsAbs() || r.URL.Scheme != "http" || r.URL.User != nil {
		http.Error(w, "netguard proxy: absolute http:// URL required", http.StatusBadRequest)
		return
	}
	x.fwd.ServeHTTP(w, r)
}

func (x *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	if _, port, err := net.SplitHostPort(r.Host); err != nil || port == "" {
		http.Error(w, "netguard proxy: CONNECT needs host:port", http.StatusBadRequest)
		return
	}
	upstream, err := x.dialer.DialContext(r.Context(), "tcp", r.Host)
	if err != nil {
		writeProxyError(w, err)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "netguard proxy: hijack unsupported", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	x.mu.Lock()
	if x.closed {
		x.mu.Unlock()
		client.Close()
		upstream.Close()
		return
	}
	x.wg.Add(1)
	x.mu.Unlock()
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		x.wg.Done()
		client.Close()
		upstream.Close()
		return
	}

	go func() {
		defer x.wg.Done()
		stop := context.AfterFunc(x.ctx, func() { client.Close(); upstream.Close() })
		defer stop()
		done := make(chan struct{}, 2)
		go func() {
			// Bytes the client sent after the CONNECT headers are already buffered.
			if n := buf.Reader.Buffered(); n > 0 {
				b, _ := buf.Reader.Peek(n)
				_, _ = upstream.Write(b)
			}
			_, _ = io.Copy(upstream, client)
			closeWrite(upstream)
			done <- struct{}{}
		}()
		go func() {
			_, _ = io.Copy(client, upstream)
			closeWrite(client)
			done <- struct{}{}
		}()
		<-done
		<-done
		client.Close()
		upstream.Close()
	}()
}

func closeWrite(c net.Conn) {
	if tc, ok := c.(interface{ CloseWrite() error }); ok {
		_ = tc.CloseWrite()
	}
}

func writeProxyError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrBlocked) {
		w.Header().Set(BlockedHeader, "1")
		http.Error(w, ErrBlocked.Error(), http.StatusForbidden)
		return
	}
	http.Error(w, "netguard proxy: upstream unreachable", http.StatusBadGateway)
}
