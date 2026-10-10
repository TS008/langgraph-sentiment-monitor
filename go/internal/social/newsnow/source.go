package newsnow

import (
	"context"
	"fmt"
	"strings"

	"github.com/ts008/langgraph-sentiment-monitor/go/internal/crawl"
)

// Source implements social.Source for NewsNow hot lists.
type Source struct {
	IDs    []string
	Client *Client
}

func (s *Source) Name() string { return "newsnow" }

func (s *Source) Ready() bool { return len(s.IDs) > 0 && s.Client != nil }

func (s *Source) Fetch(ctx context.Context) ([]crawl.Signal, error) {
	signals, errs := s.Client.FetchIDs(ctx, s.IDs)
	if len(signals) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	if len(errs) > 0 {
		// graceful degrade: return partial data
		return signals, fmt.Errorf("partial newsnow: %v", joinErrors(errs))
	}
	return signals, nil
}

func joinErrors(errs []error) string {
	var parts []string
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}
