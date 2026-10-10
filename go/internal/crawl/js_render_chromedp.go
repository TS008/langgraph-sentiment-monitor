package crawl

import (
	"context"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// FetchHTMLJS renders JS-heavy pages with headless Chrome (chromedp).
func FetchHTMLJS(ctx context.Context, pageURL string) (Signal, error) {
	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var title, body string
	tasks := chromedp.Tasks{
		chromedp.Navigate(pageURL),
		chromedp.Title(&title),
		chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &body),
	}
	if err := chromedp.Run(ctx, tasks); err != nil {
		return Signal{}, err
	}
	body = strings.Join(strings.Fields(body), " ")
	return Signal{
		Source:  "html:chromedp",
		URL:     pageURL,
		Title:   strings.TrimSpace(title),
		Snippet: truncate(body, 500),
		Fetched: time.Now().UTC(),
	}, nil
}
