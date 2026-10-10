package social

import (
	"time"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/config"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/social/newsnow"
)

// DefaultRegistry builds production ingest sources from config.
func DefaultRegistry(cfg config.Config, ing *crawl.Ingestor) *Registry {
	sources := []Source{
		&RSSSource{NameLabel: "rss", Feeds: cfg.RSSFeeds, Ingest: ing},
	}
	if ids := cfg.NewsNowIDs; len(ids) > 0 {
		client := newsnow.NewClient(cfg.NewsNowBaseURL, cfg.NewsNowMinInterval)
		sources = append(sources, &newsnow.Source{
			IDs:    ids,
			Client: client,
		})
	}
	sources = append(sources,
		&StubSource{Platform: "x", EnvVars: []string{"X_API_BEARER_TOKEN", "X_API_KEY"}, Description: "Twitter/X API v2"},
		&StubSource{Platform: "weibo", EnvVars: []string{"WEIBO_APP_KEY", "WEIBO_APP_SECRET"}, Description: "Weibo Open API (OAuth); hot lists via NewsNow when enabled"},
	)
	return &Registry{Sources: sources}
}

// NewNewsNowSourceForTest exposes a NewsNow source with a custom client (httptest).
func NewNewsNowSourceForTest(ids []string, client *newsnow.Client) Source {
	return &newsnow.Source{IDs: ids, Client: client}
}

// DefaultNewsNowInterval is the polite minimum between fetches of the same list id.
const DefaultNewsNowInterval = 5 * time.Minute
