package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
