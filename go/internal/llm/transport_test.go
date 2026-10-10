package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenCodeTransportSetsHeaders(t *testing.T) {
	var gotUA, gotSession string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotSession = r.Header.Get("x-opencode-session")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newOpenCodeHTTPClient()
	ctx := WithSession(context.Background(), "550e8400-e29b-41d4-a716-446655440000")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotUA != "aegis-monitor/1.0" {
		t.Fatalf("User-Agent: got %q", gotUA)
	}
	if gotSession != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("session: got %q", gotSession)
	}
}

func TestTransportRetriesRateLimitAndReplaysBody(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"model":"m"}` {
			t.Errorf("attempt %d body=%q", calls.Load()+1, body)
		}
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &opencodeTransport{base: http.DefaultTransport, retryBase: time.Millisecond}}
	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 3 {
		t.Fatalf("status=%d calls=%d", resp.StatusCode, calls.Load())
	}
}

func TestTransportGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &opencodeTransport{base: http.DefaultTransport, retryBase: time.Millisecond}}
	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || int(calls.Load()) != maxLLMRetries+1 {
		t.Fatalf("status=%d calls=%d", resp.StatusCode, calls.Load())
	}
}

func TestTransportDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &opencodeTransport{base: http.DefaultTransport, retryBase: time.Millisecond}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestRetryDelayHonorsRetryAfterWithCap(t *testing.T) {
	if d := retryDelay(time.Second, 0, "3"); d != 3*time.Second {
		t.Fatalf("d=%v", d)
	}
	if d := retryDelay(time.Second, 0, "3600"); d != maxRetryDelay {
		t.Fatalf("d=%v", d)
	}
	if d := retryDelay(time.Second, 2, ""); d != 4*time.Second {
		t.Fatalf("d=%v", d)
	}
}
