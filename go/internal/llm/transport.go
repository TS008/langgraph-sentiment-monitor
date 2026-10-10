package llm

import (
	"net/http"
)

// opencodeTransport injects OpenCode Go required routing headers on every API request.
type opencodeTransport struct {
	base http.RoundTripper
}

func newOpenCodeHTTPClient() *http.Client {
	base := http.DefaultTransport
	if base == nil {
		base = &http.Transport{}
	}
	return &http.Client{
		Transport: &opencodeTransport{base: base},
	}
}

func (t *opencodeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgentValue)
	if sid := SessionFromContext(req.Context()); sid != "" {
		req.Header.Set(opencodeSessionHeader, sid)
	}
	return t.base.RoundTrip(req)
}
