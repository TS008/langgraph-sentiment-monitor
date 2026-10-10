package llm

import (
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	maxLLMRetries    = 2
	defaultRetryBase = 500 * time.Millisecond
	maxRetryDelay    = 10 * time.Second
)

// sharedLLMTransport keeps more idle connections per host than DefaultTransport (2),
// so concurrent runs reuse TLS connections to the LLM gateway instead of redialing.
var sharedLLMTransport http.RoundTripper = func() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 64
	t.MaxIdleConnsPerHost = 16
	t.IdleConnTimeout = 90 * time.Second
	return t
}()

// opencodeTransport injects OpenCode Go routing headers and retries 429/5xx with backoff.
type opencodeTransport struct {
	base      http.RoundTripper
	retryBase time.Duration
}

func newOpenCodeHTTPClient() *http.Client {
	return &http.Client{
		Transport: &opencodeTransport{base: sharedLLMTransport, retryBase: defaultRetryBase},
	}
}

func (t *opencodeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	replayable := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	for attempt := 0; ; attempt++ {
		r := req.Clone(req.Context())
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			r.Body = body
		}
		r.Header.Set("User-Agent", userAgentValue)
		if sid := SessionFromContext(req.Context()); sid != "" {
			r.Header.Set(opencodeSessionHeader, sid)
		}

		resp, err := t.base.RoundTrip(r)
		if err != nil || !retryableStatus(resp.StatusCode) || attempt >= maxLLMRetries || !replayable {
			return resp, err
		}

		delay := retryDelay(t.retryBase, attempt, resp.Header.Get("Retry-After"))
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()

		timer := time.NewTimer(delay)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func retryDelay(base time.Duration, attempt int, retryAfter string) time.Duration {
	d := base << attempt
	if secs, err := strconv.Atoi(retryAfter); err == nil && secs >= 0 {
		d = time.Duration(secs) * time.Second
	}
	return min(d, maxRetryDelay)
}
