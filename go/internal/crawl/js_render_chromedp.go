package crawl

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// MaxJSRenderTabs bounds concurrent headless tabs; each costs tens of MB.
const MaxJSRenderTabs = 2

// One shared headless browser: starting Chrome per request costs ~100–300MB and
// seconds of startup. Pages run as tabs, limited by renderSem.
var (
	browserMu     sync.Mutex
	browserCtx    context.Context
	browserCancel context.CancelFunc
	renderSem     = make(chan struct{}, MaxJSRenderTabs)
)

func sharedBrowser() (context.Context, error) {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserCtx != nil && browserCtx.Err() == nil {
		return browserCtx, nil
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		allocCancel()
		return nil, err
	}
	browserCtx = ctx
	browserCancel = func() { cancel(); allocCancel() }
	return browserCtx, nil
}

// CloseBrowser shuts down the shared headless browser, if started.
func CloseBrowser() {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserCancel != nil {
		browserCancel()
		browserCtx, browserCancel = nil, nil
	}
}

// FetchHTMLJS renders JS-heavy pages with headless Chrome (chromedp).
func FetchHTMLJS(ctx context.Context, pageURL string) (Signal, error) {
	if err := ctx.Err(); err != nil {
		return Signal{}, err
	}
	select {
	case renderSem <- struct{}{}:
	case <-ctx.Done():
		return Signal{}, ctx.Err()
	}
	defer func() { <-renderSem }()

	browser, err := sharedBrowser()
	if err != nil {
		return Signal{}, err
	}
	tabCtx, cancelTab := chromedp.NewContext(browser)
	defer cancelTab()
	tabCtx, cancelTimeout := context.WithTimeout(tabCtx, 45*time.Second)
	defer cancelTimeout()

	// The tab derives from the browser, not the caller; propagate caller cancellation.
	stop := context.AfterFunc(ctx, cancelTab)
	defer stop()

	var title, body string
	tasks := chromedp.Tasks{
		chromedp.Navigate(pageURL),
		chromedp.Title(&title),
		chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &body),
	}
	if err := chromedp.Run(tabCtx, tasks); err != nil {
		if ctx.Err() != nil {
			return Signal{}, ctx.Err()
		}
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
