package newsnow

import (
	"bytes"
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
)

const DefaultUserAgent = "aegis-monitor/1.0 (+https://github.com/ts008/langgraph-sentiment-monitor)"

// Client fetches hot lists from a NewsNow-compatible API.
type Client struct {
	BaseURL     string
	HTTPClient  *http.Client
	UserAgent   string
	MinInterval time.Duration
	Logger      *slog.Logger

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

// FetchIDs loads multiple list ids with per-id rate limiting and URL dedupe.
func (c *Client) FetchIDs(ctx context.Context, ids []string) ([]crawl.Signal, []error) {
	seen := make(map[string]struct{})
	var out []crawl.Signal
	var errs []error
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		sigs, err := c.fetchOne(ctx, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		for _, s := range sigs {
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

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		msg := summarizeResponse(resp.StatusCode, resp.Header.Get("Content-Type"), body)
		c.logWarn(id, msg)
		return nil, fmt.Errorf("%s", msg)
	}

	var payload sourceResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&payload); err != nil {
		msg := summarizeResponse(0, resp.Header.Get("Content-Type"), body)
		c.logWarn(id, msg)
		return nil, fmt.Errorf("%s", msg)
	}
	if len(payload.Items) == 0 {
		c.logWarn(id, "empty items")
		return nil, fmt.Errorf("empty items")
	}

	now := time.Now().UTC()
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
	var signals []crawl.Signal
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
