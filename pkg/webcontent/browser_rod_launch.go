//go:build !js

// browser_rod_launch.go provides browser launch/lifecycle management, GPU probing,
// session management, and shared constants for the go-rod based BrowserRenderer.

package webcontent

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// renderTimeout is the maximum time allowed for a single RenderPage call
// (navigation + JS execution + HTML extraction).
const renderTimeout = 30 * time.Second

// stableDuration is how long WaitStable watches for network/DOM quiescence.
const stableDuration = 500 * time.Millisecond

// defaultViewportWidth is the fallback viewport width when 0 is passed.
const defaultViewportWidth = 1280

// defaultViewportHeight is the fallback viewport height when 0 is passed.
const defaultViewportHeight = 720

const defaultWaitTimeout = 10 * time.Second

// waitStableTolerant waits for network/DOM quiescence and reports whether it
// settled. A page that never goes idle — an analytics beacon, a chat widget, a
// polling loop — keeps WaitStable from converging, and treating that as fatal
// threw away a page whose content had already rendered. The wait is bounded by
// stableWaitBudget so a busy page costs a fixed delay instead of the whole
// render timeout; the caller proceeds with whatever the DOM holds.
func waitStableTolerant(page *rod.Page) bool {
	// Cancel the wait on timeout instead of abandoning it: WaitStable blocks
	// on CDP round-trips, and a page closed by the caller while it is still
	// polling would otherwise race the close.
	ctx, cancel := context.WithTimeout(context.Background(), stableWaitBudget)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- page.Context(ctx).WaitStable(stableDuration)
	}()

	select {
	case err := <-done:
		return err == nil
	case <-ctx.Done():
		return false
	}
}

// stableWaitBudget bounds how long a capture waits for quiescence before
// taking the DOM as it stands.
const stableWaitBudget = 5 * time.Second

// navigateWithRetry navigates to url, retrying once on a transient network
// failure. A connection reset, timeout, or aborted request is a property of
// the moment, not of the URL, so a single retry recovers most of them; a DNS
// failure or a bad scheme is deterministic and surfaces immediately.
func navigateWithRetry(page *rod.Page, url string) error {
	err := page.Timeout(getNavigationTimeout(url)).Navigate(url)
	if err == nil || !IsTransientNavigationError(err) {
		return err
	}
	// Back off briefly so an overloaded or rate-limiting origin gets room,
	// then retry once on the same page.
	time.Sleep(navigationRetryBackoff)
	return page.Timeout(getNavigationTimeout(url)).Navigate(url)
}

// navigationRetryBackoff is the pause before a single navigation retry.
const navigationRetryBackoff = 500 * time.Millisecond

// localhostTimeout is the navigation timeout for localhost URLs (fast fail).
const localhostTimeout = 10 * time.Second

// remoteTimeout is the navigation timeout for all other URLs.
const remoteTimeout = 30 * time.Second

// getNavigationTimeout returns the appropriate navigation timeout for the given URL.
// Localhost URLs get a shorter timeout (10s) to fail fast when nothing is listening.
// Remote URLs get a longer timeout (30s) to accommodate slower network conditions.
func getNavigationTimeout(url string) time.Duration {
	lowerURL := strings.ToLower(url)
	// Match localhost-style URLs: with optional port, path, query, fragment.
	if strings.HasPrefix(lowerURL, "http://localhost") ||
		strings.HasPrefix(lowerURL, "http://127.0.0.1") ||
		strings.HasPrefix(lowerURL, "http://[::1]") ||
		strings.HasPrefix(lowerURL, "https://localhost") ||
		strings.HasPrefix(lowerURL, "https://127.0.0.1") ||
		strings.HasPrefix(lowerURL, "https://[::1]") {
		return localhostTimeout
	}
	return remoteTimeout
}

const browserInstrumentationScript = `
(() => {
  if (window.__sproutBrowserCaptureInstalled) return;
  window.__sproutBrowserCaptureInstalled = true;
  window.__sproutBrowserCapture = { console: [], errors: [], network: [] };
  const limitPush = (list, value) => {
    list.push(value);
    if (list.length > 100) list.shift();
  };
  const stringify = (value) => {
    try {
      if (typeof value === 'string') return value;
      return JSON.stringify(value);
    } catch (_err) {
      return String(value);
    }
  };
  for (const level of ['log', 'info', 'warn', 'error']) {
    const original = console[level];
    console[level] = function (...args) {
      try {
        limitPush(window.__sproutBrowserCapture.console, '[' + level + '] ' + args.map(stringify).join(' '));
      } catch (_err) {}
      return original.apply(this, args);
    };
  }
  window.addEventListener('error', (event) => {
    try {
      const location = event.filename ? ' @ ' + event.filename + ':' + event.lineno + ':' + event.colno : '';
      limitPush(window.__sproutBrowserCapture.errors, String(event.message || 'error') + location);
    } catch (_err) {}
  });
  window.addEventListener('unhandledrejection', (event) => {
    try {
      limitPush(window.__sproutBrowserCapture.errors, 'Unhandled rejection: ' + stringify(event.reason));
    } catch (_err) {}
  });
  const recordNetwork = (value) => {
    try {
      limitPush(window.__sproutBrowserCapture.network, value);
    } catch (_err) {}
  };
  if (typeof window.fetch === 'function') {
    const originalFetch = window.fetch.bind(window);
    window.fetch = async function(input, init) {
      const method = (init && init.method) || (input && input.method) || 'GET';
      const url = typeof input === 'string' ? input : ((input && input.url) || '');
      try {
        const response = await originalFetch(input, init);
        recordNetwork({ type: 'fetch', method, url, status: response.status, ok: !!response.ok, initiator: 'fetch' });
        return response;
      } catch (err) {
        recordNetwork({ type: 'fetch', method, url, error: String(err), initiator: 'fetch' });
        throw err;
      }
    };
  }
  if (typeof window.XMLHttpRequest === 'function') {
    const OriginalXHR = window.XMLHttpRequest;
    function WrappedXHR() {
      const xhr = new OriginalXHR();
      let method = 'GET';
      let url = '';
      const originalOpen = xhr.open;
      xhr.open = function(m, u) {
        method = m || 'GET';
        url = u || '';
        return originalOpen.apply(xhr, arguments);
      };
      xhr.addEventListener('loadend', function() {
        recordNetwork({ type: 'xhr', method, url, status: xhr.status || 0, ok: xhr.status >= 200 && xhr.status < 400, initiator: 'xhr' });
      });
      xhr.addEventListener('error', function() {
        recordNetwork({ type: 'xhr', method, url, error: 'network error', initiator: 'xhr' });
      });
      return xhr;
    }
    WrappedXHR.prototype = OriginalXHR.prototype;
    window.XMLHttpRequest = WrappedXHR;
  }
})();
`

type browserSession struct {
	id        string
	incognito *rod.Browser
	page      *rod.Page
	mu        sync.Mutex
	createdAt time.Time
	lastUsed  time.Time
}

// rodRenderer implements BrowserRenderer using go-rod and Chromium.
type rodRenderer struct {
	browser *rod.Browser

	mu       sync.Mutex
	closed   bool
	sessions map[string]*browserSession
}

// Compile-time interface satisfaction check.
var _ BrowserRenderer = (*rodRenderer)(nil)

// NewBrowserRenderer returns a BrowserRenderer backed by go-rod.
// The browser is launched lazily on the first call to RenderPage.
func NewBrowserRenderer() BrowserRenderer {
	r := &rodRenderer{}
	runtime.SetFinalizer(r, (*rodRenderer).Close)
	return r
}

// connect launches Chromium (if not already connected) and returns the browser.
// It is safe to call from multiple goroutines.
func (r *rodRenderer) connect(ctx context.Context) (*rod.Browser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, fmt.Errorf("browser renderer has been closed")
	}
	if r.browser != nil {
		// A cached handle is returned as-is. It is not proof of a live
		// browser — Chromium can crash or be OOM-killed while the process
		// keeps running — but no cheap probe can tell: Version() and
		// Incognito() both answer from rod's cached connection state and
		// report a closed browser as healthy. The reliable signal is a real
		// operation failing, which openIncognitoPage handles by discarding
		// the handle and retrying once on a fresh browser.
		return r.browser, nil
	}

	browser, err := r.launch(ctx)
	if err != nil {
		return nil, err
	}
	r.browser = browser
	return r.browser, nil
}

// launch starts a browser, falling back to --disable-gpu when the GPU probe
// shows screenshot capture is unusable in this environment.
func (r *rodRenderer) launch(ctx context.Context) (*rod.Browser, error) {
	// Attempt launch with retry: first try with GPU enabled; if the GPU probe
	// (a tiny screenshot) hangs, relaunch with --disable-gpu. This way machines
	// with a real GPU keep acceleration, while environments like WSL2 or
	// Docker containers without GPU support fall back gracefully.
	disableGPU := gpuProbeFailed()
	for range 2 {
		browser, needsFallback, err := r.launchAndProbe(ctx, disableGPU)
		if err != nil {
			return nil, err
		}
		if !needsFallback {
			return browser, nil
		}
		// GPU probe hung — close browser and retry with --disable-gpu.
		_ = browser.Close()
		markGPUProbe(false)
		disableGPU = true
	}

	return nil, fmt.Errorf("browser launch failed after GPU fallback")
}

// launchAndProbe launches a browser instance, optionally with --disable-gpu,
// and probes whether screenshot capture works. Returns the browser, whether
// a fallback to --disable-gpu is needed, and any error.
func (r *rodRenderer) launchAndProbe(ctx context.Context, disableGPU bool) (*rod.Browser, bool, error) {
	l := launcher.New().Headless(true).NoSandbox(true).Context(ctx)
	if disableGPU {
		l.Set("disable-gpu")
	}

	// Try common system browser paths before auto-download.
	// This allows running on systems with pre-installed Chromium/Chrome.
	for _, bin := range systemBrowserPaths() {
		if _, err := os.Stat(bin); err == nil {
			l = l.Bin(bin)
			break
		}
	}

	u, err := l.Launch()
	if err != nil {
		return nil, false, fmt.Errorf("browser launch: %w", err)
	}
	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, false, fmt.Errorf("browser connect: %w", err)
	}

	// If GPU support hasn't been probed yet, test whether screenshots work.
	// In environments without a functional GPU (WSL2, some Docker containers),
	// PageCaptureScreenshot hangs indefinitely. A quick probe lets us detect
	// this and fall back to --disable-gpu.
	if !disableGPU && !gpuProbeDone() {
		if probeGPUSupport(ctx, browser) {
			markGPUProbe(true)
		} else {
			return browser, true, nil // needs fallback
		}
	}

	return browser, false, nil
}

// openIncognitoPage creates an incognito browser context and opens a new page.
// The caller MUST defer close on both the incognito browser and the page.
func (r *rodRenderer) openIncognitoPage(ctx context.Context) (*rod.Browser, *rod.Page, error) {
	incognito, page, err := r.tryOpenIncognitoPage(ctx)
	if err == nil {
		return incognito, page, nil
	}
	// A browser can die between the liveness probe and the first real call
	// (crash, OOM kill, or a wedged renderer). Discard the cached handle and
	// try once on a fresh browser rather than failing every later call.
	//
	// The discard does not ask whether the handle looks alive: Version()
	// answers from rod's cached connection state, so it reports a closed
	// browser as healthy. Opening a page is the operation that actually
	// fails, which makes this error the reliable signal.
	if !r.discardBrowser() {
		return nil, nil, err
	}
	return r.tryOpenIncognitoPage(ctx)
}

// discardBrowser drops the cached browser handle so the next connect
// relaunches. It reports whether a handle was discarded; a renderer that is
// already closed has nothing to replace.
func (r *rodRenderer) discardBrowser() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.browser == nil || r.closed {
		return false
	}
	_ = r.browser.Close()
	r.browser = nil
	return true
}

// tryOpenIncognitoPage performs one attempt at opening an incognito page.
func (r *rodRenderer) tryOpenIncognitoPage(ctx context.Context) (*rod.Browser, *rod.Page, error) {
	browser, err := r.connect(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("browser connect: %w", err)
	}

	incognito, err := browser.Incognito()
	if err != nil {
		return nil, nil, fmt.Errorf("incognito context: %w", err)
	}

	page, err := incognito.Page(proto.TargetCreateTarget{})
	if err != nil {
		_ = incognito.Close()
		return nil, nil, fmt.Errorf("open page: %w", err)
	}

	return incognito, page, nil
}

func newBrowserSessionID() string {
	return fmt.Sprintf("browser_%d", time.Now().UnixNano())
}

func (r *rodRenderer) acquireSession(ctx context.Context, requestedID string) (*browserSession, error) {
	sessionID := strings.TrimSpace(requestedID)
	if sessionID == "" {
		sessionID = newBrowserSessionID()
	}

	r.mu.Lock()
	if r.sessions == nil {
		r.sessions = make(map[string]*browserSession)
	}
	if existing, ok := r.sessions[sessionID]; ok {
		r.mu.Unlock()
		existing.mu.Lock()
		existing.lastUsed = time.Now()
		return existing, nil
	}
	r.mu.Unlock()

	incognito, page, err := r.openIncognitoPage(ctx)
	if err != nil {
		return nil, fmt.Errorf("open page: %w", err)
	}
	session := &browserSession{
		id:        sessionID,
		incognito: incognito,
		page:      page,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}

	r.mu.Lock()
	if r.sessions == nil {
		r.sessions = make(map[string]*browserSession)
	}
	if existing, ok := r.sessions[sessionID]; ok {
		r.mu.Unlock()
		_ = page.Close()
		_ = incognito.Close()
		existing.mu.Lock()
		existing.lastUsed = time.Now()
		return existing, nil
	}
	r.sessions[sessionID] = session
	r.mu.Unlock()

	session.mu.Lock()
	return session, nil
}

func (r *rodRenderer) closeSessionByID(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	r.mu.Lock()
	session, ok := r.sessions[sessionID]
	if ok {
		delete(r.sessions, sessionID)
	}
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown browser session %q", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.page != nil {
		_ = session.page.Close()
	}
	if session.incognito != nil {
		_ = session.incognito.Close()
	}
	return nil
}

// applyViewportAndUA sets the viewport dimensions and user-agent on a page,
// falling back to defaults when zero values are provided.
func applyViewportAndUA(page *rod.Page, vw, vh int, ua string) error {
	if vw == 0 {
		vw = defaultViewportWidth
	}
	if vh == 0 {
		vh = defaultViewportHeight
	}

	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  vw,
		Height: vh,
	}); err != nil {
		return fmt.Errorf("set viewport: %w", err)
	}

	if ua != "" {
		if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: ua}); err != nil {
			return fmt.Errorf("set user-agent: %w", err)
		}
	}

	return nil
}

// Close shuts down the browser process.
func (r *rodRenderer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	r.closed = true

	for id, session := range r.sessions {
		session.mu.Lock()
		if session.page != nil {
			_ = session.page.Close()
		}
		if session.incognito != nil {
			_ = session.incognito.Close()
		}
		session.mu.Unlock()
		delete(r.sessions, id)
	}

	if r.browser != nil {
		_ = r.browser.Close()
		r.browser = nil
	}
}
