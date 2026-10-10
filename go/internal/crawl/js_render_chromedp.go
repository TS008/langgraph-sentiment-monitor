package crawl

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/ts008/langgraph-sentiment-monitor/go/internal/netguard"
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

// FetchHTMLJS renders JS-heavy pages with headless Chrome (chromedp). Every request
// the tab makes (navigation, redirects, subresources, fetch/XHR) is checked against
// policy. Chrome resolves DNS itself, so a rebinding race between our lookup and
// Chrome's remains possible; the colly path is not affected by this.
func FetchHTMLJS(ctx context.Context, pageURL string, policy netguard.Policy) (Signal, error) {
	if err := ctx.Err(); err != nil {
		return Signal{}, err
	}
	if _, err := policy.ValidateURL(ctx, pageURL); err != nil {
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

	var tasks chromedp.Tasks
	if !policy.AllowPrivate {
		guardRequests(tabCtx, policy)
		tasks = append(tasks, fetch.Enable())
	}
	var title, body string
	tasks = append(tasks,
		chromedp.Navigate(pageURL),
		chromedp.Title(&title),
		chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &body),
	)
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

// guardRequests pauses each request via the Fetch domain and fails those whose
// host resolves to a blocked network. Non-network schemes (data:, blob:) pass.
func guardRequests(tabCtx context.Context, policy netguard.Policy) {
	chromedp.ListenTarget(tabCtx, func(ev interface{}) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		// Handlers must not block the event loop; CDP calls go on their own goroutine.
		go func() {
			execCtx := cdp.WithExecutor(tabCtx, chromedp.FromContext(tabCtx).Target)
			if requestAllowed(tabCtx, policy, e.Request.URL) {
				_ = fetch.ContinueRequest(e.RequestID).Do(execCtx)
				return
			}
			_ = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient).Do(execCtx)
		}()
	})
}

func requestAllowed(ctx context.Context, policy netguard.Policy, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "data", "blob", "about":
		return true
	}
	_, err = policy.ValidateURL(ctx, raw)
	return err == nil
}
