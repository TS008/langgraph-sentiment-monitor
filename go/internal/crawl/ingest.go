package crawl

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/mmcdole/gofeed"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/textutil"
	"golang.org/x/time/rate"
)

// Signal is a normalized piece of public web/RSS content for downstream analysis.
type Signal struct {
	Source  string    `json:"source"`
	URL     string    `json:"url"`
	Title   string    `json:"title"`
	Snippet string    `json:"snippet"`
	Rank    int       `json:"rank,omitempty"`
	Heat    string    `json:"heat,omitempty"`
	Fetched time.Time `json:"fetched"`
}

// Ingestor fetches RSS and optional HTML pages with rate limits and parallelism.
type Ingestor struct {
	parallelism int
	limiter     *rate.Limiter
	client      *http.Client
}

func NewIngestor(parallelism int) *Ingestor {
	if parallelism < 1 {
		parallelism = 1
	}
	return &Ingestor{
		parallelism: parallelism,
		limiter:     rate.NewLimiter(rate.Every(200*time.Millisecond), parallelism),
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchRSS loads items from feed URLs concurrently.
func (i *Ingestor) FetchRSS(ctx context.Context, feedURLs []string) ([]Signal, error) {
	var (
		mu      sync.Mutex
		signals []Signal
		wg      sync.WaitGroup
		errs    []error
	)

	fp := gofeed.NewParser()
	fp.Client = i.client

	sem := make(chan struct{}, i.parallelism)
	for _, url := range feedURLs {
		wg.Add(1)
		go func(feedURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := i.limiter.Wait(ctx); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}

			feed, err := fp.ParseURLWithContext(feedURL, ctx)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("rss %s: %w", feedURL, err))
				mu.Unlock()
				return
			}
			limit := 15
			if len(feed.Items) < limit {
				limit = len(feed.Items)
			}
			for j := 0; j < limit; j++ {
				item := feed.Items[j]
				snippet := item.Description
				if snippet == "" {
					snippet = item.Content
				}
				snippet = truncate(stripHTML(snippet), 500)
				mu.Lock()
				signals = append(signals, Signal{
					Source:  "rss:" + feedURL,
					URL:     item.Link,
					Title:   item.Title,
					Snippet: snippet,
					Fetched: time.Now().UTC(),
				})
				mu.Unlock()
			}
		}(url)
	}
	wg.Wait()
	if len(signals) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	return signals, nil
}

// FetchPage fetches HTML with colly or chromedp when useJS is true.
func (i *Ingestor) FetchPage(ctx context.Context, pageURL string, useJS bool) (Signal, error) {
	if useJS {
		return FetchHTMLJS(ctx, pageURL)
	}
	return i.FetchHTML(ctx, pageURL)
}

// FetchHTML fetches a single page and extracts title + visible text snippet (no JS).
func (i *Ingestor) FetchHTML(ctx context.Context, pageURL string) (Signal, error) {
	if err := i.limiter.Wait(ctx); err != nil {
		return Signal{}, err
	}

	var (
		title    string
		snippet  strings.Builder
		errMu    sync.Mutex
		fetchErr error
	)

	c := colly.NewCollector(
		colly.UserAgent("AEGIS-Monitor/1.0 (+https://github.com/ts008/langgraph-sentiment-monitor)"),
	)
	c.SetRequestTimeout(25 * time.Second)
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 1,
		Delay:       500 * time.Millisecond,
	})

	c.OnHTML("title", func(e *colly.HTMLElement) {
		title = strings.TrimSpace(e.Text)
	})
	c.OnHTML("p", func(e *colly.HTMLElement) {
		if snippet.Len() > 800 {
			return
		}
		t := strings.TrimSpace(e.Text)
		if t != "" {
			snippet.WriteString(t)
			snippet.WriteString(" ")
		}
	})
	c.OnError(func(r *colly.Response, err error) {
		errMu.Lock()
		fetchErr = err
		errMu.Unlock()
	})

	if err := c.Visit(pageURL); err != nil {
		return Signal{}, err
	}
	c.Wait()
	if fetchErr != nil {
		return Signal{}, fetchErr
	}

	return Signal{
		Source:  "html",
		URL:     pageURL,
		Title:   title,
		Snippet: truncate(snippet.String(), 500),
		Fetched: time.Now().UTC(),
	}, nil
}

// stripHTML replaces tags with spaces in a single pass and collapses whitespace.
// A '<' with no later '>' is kept as literal text.
func stripHTML(s string) string {
	lastClose := strings.LastIndexByte(s, '>')
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inTag:
			if c == '>' {
				inTag = false
			}
		case c == '<' && i < lastClose:
			inTag = true
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func truncate(s string, n int) string {
	return textutil.Truncate(strings.TrimSpace(s), n)
}
