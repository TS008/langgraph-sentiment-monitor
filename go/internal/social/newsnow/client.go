package newsnow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"golang.org/x/sync/singleflight"
)

const DefaultUserAgent = "aegis-monitor/1.0 (+https://github.com/ts008/langgraph-sentiment-monitor)"

const (
	// MaxBodyBytes caps a single hot-list response; real lists are ~10–50KB.
	MaxBodyBytes = 2 << 20
	// DefaultConcurrency bounds parallel list fetches per FetchIDs call.
	DefaultConcurrency = 4
	errorPeekBytes     = 4096
)

// Client fetches hot lists from a NewsNow-compatible API.
type Client struct {
	BaseURL     string
	HTTPClient  *http.Client
	UserAgent   string
	MinInterval time.Duration
	Concurrency int
	Logger      *slog.Logger

	group     singleflight.Group
	mu        sync.Mutex
	lastFetch map[string]time.Time
	cache     map[string][]crawl.Signal
	lastWarn  map[string]time.Time
}

func NewClient(baseURL string, minInterval time.Duration) *Client {
	if minInterval <= 0 {
		minInterval = 5 * time.Minute
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		HTTPClient:  &http.Client{Timeout: 20 * time.Second},
		UserAgent:   DefaultUserAgent,
		MinInterval: minInterval,
		Logger:      slog.Default(),
		lastFetch:   make(map[string]time.Time),
		cache:       make(map[string][]crawl.Signal),
		lastWarn:    make(map[string]time.Time),
	}
}

type sourceResponse struct {
	Status      string     `json:"status"`
	ID          string     `json:"id"`
	UpdatedTime int64      `json:"updatedTime"`
	Items       []newsItem `json:"items"`
}

type newsItem struct {
	ID    interface{} `json:"id"`
	Title string      `json:"title"`
	URL   string      `json:"url"`
	Extra *struct {
		Info interface{} `json:"info"`
		Diff *int        `json:"diff"`
	} `json:"extra"`
}

// FetchIDs loads multiple list ids concurrently with per-id caching and URL dedupe.
// Output order follows ids, so results are deterministic regardless of completion order.
func (c *Client) FetchIDs(ctx context.Context, ids []string) ([]crawl.Signal, []error) {
	type result struct {
		id   string
		sigs []crawl.Signal
		err  error
	}
	results := make([]result, len(ids))
	limit := c.Concurrency
	if limit < 1 {
		limit = DefaultConcurrency
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, id := range ids {
		id = strings.TrimSpace(id)
		results[i].id = id
		if id == "" {
			continue
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i].err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			results[i].sigs, results[i].err = c.fetchOneShared(ctx, id)
		}(i, id)
	}
	wg.Wait()

	seen := make(map[string]struct{})
	var out []crawl.Signal
	var errs []error
	for _, r := range results {
		if r.id == "" {
			continue
		}
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.id, r.err))
			continue
		}
		for _, s := range r.sigs {
			key := s.URL
			if key == "" {
				key = s.Title
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, s)
		}
	}
	return out, errs
}

// fetchOneShared collapses concurrent fetches of the same id (e.g. overlapping runs) into one request.
func (c *Client) fetchOneShared(ctx context.Context, id string) ([]crawl.Signal, error) {
	v, err, _ := c.group.Do(id, func() (interface{}, error) {
		return c.fetchOne(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return v.([]crawl.Signal), nil
}

func (c *Client) fetchOne(ctx context.Context, id string) ([]crawl.Signal, error) {
	c.mu.Lock()
	if t, ok := c.lastFetch[id]; ok && time.Since(t) < c.MinInterval {
		if cached, hit := c.cache[id]; hit {
			c.mu.Unlock()
			return cached, nil
		}
	}
	c.mu.Unlock()

	u, err := url.Parse(c.BaseURL + "/api/s")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("id", id)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.logWarn(id, "request failed: "+err.Error())
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		head, _ := io.ReadAll(io.LimitReader(resp.Body, errorPeekBytes))
		msg := summarizeResponse(resp.StatusCode, resp.Header.Get("Content-Type"), head)
		c.logWarn(id, msg)
		return nil, fmt.Errorf("%s", msg)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		c.logWarn(id, "read body: "+err.Error())
		return nil, err
	}
	if len(body) > MaxBodyBytes {
		c.logWarn(id, "response exceeds size limit")
		return nil, fmt.Errorf("response exceeds %d bytes", MaxBodyBytes)
	}

	signals, err := ParseResponse(id, body, time.Now().UTC())
	if err != nil {
		head := body
		if len(head) > errorPeekBytes {
			head = head[:errorPeekBytes]
		}
		msg := summarizeResponse(0, resp.Header.Get("Content-Type"), head)
		c.logWarn(id, msg)
		return nil, fmt.Errorf("%s", msg)
	}
	if len(signals) == 0 {
		c.logWarn(id, "empty items")
		return nil, fmt.Errorf("empty items")
	}

	c.mu.Lock()
	c.lastFetch[id] = time.Now()
	c.cache[id] = signals
	c.mu.Unlock()

	return signals, nil
}

func (c *Client) logWarn(id, msg string) {
	c.mu.Lock()
	if t, ok := c.lastWarn[id]; ok && time.Since(t) < c.MinInterval {
		c.mu.Unlock()
		return
	}
	c.lastWarn[id] = time.Now()
	c.mu.Unlock()
	if c.Logger != nil {
		c.Logger.Warn("newsnow fetch", "id", id, "base", c.BaseURL, "detail", msg)
	}
}

// summarizeResponse builds a short error string without dumping HTML bodies.
func summarizeResponse(status int, contentType string, body []byte) string {
	if status == http.StatusForbidden {
		return "http 403 (blocked; run self-hosted NewsNow via docker compose)"
	}
	if status > 0 {
		return fmt.Sprintf("http %d", status)
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/html") || looksLikeHTML(body) {
		return "non-JSON response (HTML error page)"
	}
	return "invalid JSON response"
}

func looksLikeHTML(b []byte) bool {
	s := strings.TrimSpace(string(b))
	return strings.HasPrefix(s, "<!") || strings.Contains(s, "<html")
}

func formatHeat(extra *struct {
	Info interface{} `json:"info"`
	Diff *int        `json:"diff"`
}) string {
	if extra == nil {
		return ""
	}
	switch v := extra.Info.(type) {
	case string:
		if v != "" && v != "false" {
			return v
		}
	case float64:
		return fmt.Sprintf("%.0f", v)
	case nil:
		break
	default:
		s := fmt.Sprint(v)
		if s != "" && s != "false" {
			return s
		}
	}
	if extra.Diff != nil {
		return fmt.Sprintf("diff:%d", *extra.Diff)
	}
	return ""
}

// ParseResponse converts raw JSON (for tests).
func ParseResponse(id string, data []byte, fetched time.Time) ([]crawl.Signal, error) {
	var payload sourceResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if id == "" {
		id = payload.ID
	}
	now := fetched
	if now.IsZero() {
		now = time.Now().UTC()
	}
	signals := make([]crawl.Signal, 0, len(payload.Items))
	for i, item := range payload.Items {
		if item.Title == "" {
			continue
		}
		link := item.URL
		if link == "" {
			link = "newsnow:" + id + "/" + fmt.Sprint(item.ID)
		}
		heat := formatHeat(item.Extra)
		snippet := fmt.Sprintf("rank=%d", i+1)
		if heat != "" {
			snippet = fmt.Sprintf("rank=%d heat=%s", i+1, heat)
		}
		signals = append(signals, crawl.Signal{
			Source:  "newsnow:" + id,
			URL:     link,
			Title:   item.Title,
			Snippet: snippet,
			Rank:    i + 1,
			Heat:    heat,
			Fetched: now,
		})
	}
	return signals, nil
}
