package webcontent

import (
	"context"
	"errors"
)

// BrowserRenderer renders HTML pages using a headless browser.
// Implementations may require external dependencies (e.g., rod/Chromium)
// and are loaded via build tags.
type BrowserRenderer interface {
	// RenderPage navigates to the given URL using a headless browser,
	// waits for JavaScript to execute, and returns the fully rendered HTML.
	RenderPage(ctx context.Context, url string) (string, error)

	// Screenshot captures a screenshot of the given URL and writes it to outputPath.
	// viewportWidth and viewportHeight set the browser viewport dimensions (0 = use defaults 1280x720).
	// userAgent overrides the browser user-agent string ("" = use default).
	Screenshot(ctx context.Context, url string, outputPath string, viewportWidth, viewportHeight int, userAgent string) error

	// CaptureDOM returns the rendered HTML of the page (similar to RenderPage but specifically
	// for capturing the DOM state after JS execution). Use this when you need the full HTML
	// rather than text-extracted content.
	CaptureDOM(ctx context.Context, url string, viewportWidth, viewportHeight int, userAgent string) (string, error)

	// Run executes an interactive browser workflow against the given URL and returns
	// a structured result suitable for debugging, testing, and JS-rendered scraping.
	Run(ctx context.Context, url string, opts BrowseOptions) (*BrowseResult, error)

	// Close releases any resources held by the renderer (browsers, pages, etc.)
	Close()
}

// BrowseStep describes a single browser interaction step.
type BrowseStep struct {
	Action   string `json:"action"`
	Selector string `json:"selector,omitempty"`
	Value    string `json:"value,omitempty"`
	Key      string `json:"key,omitempty"`
	Millis   int    `json:"millis,omitempty"`
	Script   string `json:"script,omitempty"`
	Expect   string `json:"expect,omitempty"`
}

type ElementBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// SelectorCapture describes the captured state of a selector after page interactions.
type SelectorCapture struct {
	Selector    string            `json:"selector"`
	Found       bool              `json:"found"`
	Count       int               `json:"count"`
	Visible     bool              `json:"visible,omitempty"`
	Enabled     bool              `json:"enabled,omitempty"`
	BoundingBox *ElementBox       `json:"bounding_box,omitempty"`
	Text        string            `json:"text,omitempty"`
	HTML        string            `json:"html,omitempty"`
	Value       string            `json:"value,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
}

// EvalResult captures the result of a script evaluation step.
type EvalResult struct {
	Script string `json:"script"`
	Value  string `json:"value,omitempty"`
	Error  string `json:"error,omitempty"`
}

type NetworkRequest struct {
	Type        string `json:"type,omitempty"`
	URL         string `json:"url,omitempty"`
	Method      string `json:"method,omitempty"`
	Status      int    `json:"status,omitempty"`
	OK          bool   `json:"ok,omitempty"`
	Initiator   string `json:"initiator,omitempty"`
	Error       string `json:"error,omitempty"`
	CORSBlocked bool   `json:"cors_blocked,omitempty"`
}

// BrowseResult contains structured browser inspection output.
type BrowseResult struct {
	SessionID        string            `json:"session_id,omitempty"`
	FinalURL         string            `json:"final_url"`
	Title            string            `json:"title,omitempty"`
	ReadyState       string            `json:"ready_state,omitempty"`
	VisibleText      string            `json:"visible_text,omitempty"`
	DOM              string            `json:"dom,omitempty"`
	ScreenshotPath   string            `json:"screenshot_path,omitempty"`
	SelectorCaptures []SelectorCapture `json:"selector_captures,omitempty"`
	ConsoleMessages  []string          `json:"console_messages,omitempty"`
	PageErrors       []string          `json:"page_errors,omitempty"`
	NetworkRequests  []NetworkRequest  `json:"network_requests,omitempty"`
	CORSIssues       []string          `json:"cors_issues,omitempty"`
	Cookies          map[string]string `json:"cookies,omitempty"`
	LocalStorage     map[string]string `json:"local_storage,omitempty"`
	SessionStorage   map[string]string `json:"session_storage,omitempty"`
	EvalResults      []EvalResult      `json:"eval_results,omitempty"`
	Actions          []string          `json:"actions,omitempty"`
}

// BrowseOptions configures browser-based URL browsing.
type BrowseOptions struct {
	// Ctx carries a context for cancellation/deadlines; if nil, context.Background() is used.
	Ctx context.Context
	// ViewportWidth sets the browser viewport width in pixels (0 = default 1280)
	ViewportWidth int
	// ViewportHeight sets the browser viewport height in pixels (0 = default 720)
	ViewportHeight int
	// UserAgent overrides the browser user-agent string
	UserAgent string
	// Action determines what to do: "screenshot", "dom", "text", or "inspect" (default: "text")
	Action string
	// ScreenshotPath is the file path for screenshot output (required for action="screenshot")
	ScreenshotPath string
	// SessionID reuses or names a persistent built-in browser session for iterative debugging.
	SessionID string
	// PersistSession keeps the browser page alive after this call and returns a session_id in the result.
	PersistSession bool
	// CloseSession closes the referenced persistent session after this call completes.
	CloseSession bool
	// WaitForSelector waits for a selector to appear before capturing output or running steps.
	WaitForSelector string
	// WaitTimeoutMs overrides the wait timeout for selector-based operations (default: 10000).
	WaitTimeoutMs int
	// Steps applies a series of browser interactions after navigation.
	Steps []BrowseStep
	// CaptureSelectors captures selector state after interactions.
	CaptureSelectors []string
	// CaptureDOM includes rendered DOM in inspect results.
	CaptureDOM bool
	// CaptureText includes visible text in inspect results.
	CaptureText bool
	// IncludeConsole captures browser console messages and page errors in inspect results.
	IncludeConsole bool
	// CaptureNetwork includes fetch/XHR diagnostics in inspect results.
	CaptureNetwork bool
	// CaptureStorage includes localStorage/sessionStorage snapshots in inspect results.
	CaptureStorage bool
	// CaptureCookies includes document.cookie-visible cookies in inspect results.
	CaptureCookies bool
	// ResponseMaxChars bounds large string fields in structured inspect results (0 = defaults).
	ResponseMaxChars int
}

// nopRenderer is a no-op implementation that always returns an error,
// used when no headless browser is available (i.e., without the browser build tag).
type nopRenderer struct{}

// Compile-time interface satisfaction check.
var _ BrowserRenderer = (*nopRenderer)(nil)

var nop = &nopRenderer{}

func (n *nopRenderer) RenderPage(_ context.Context, _ string) (string, error) {
	return "", errors.New("browser rendering not available")
}

func (n *nopRenderer) Screenshot(_ context.Context, _ string, _ string, _, _ int, _ string) error {
	return errors.New("browser rendering not available")
}

func (n *nopRenderer) CaptureDOM(_ context.Context, _ string, _, _ int, _ string) (string, error) {
	return "", errors.New("browser rendering not available")
}

func (n *nopRenderer) Run(_ context.Context, _ string, _ BrowseOptions) (*BrowseResult, error) {
	return nil, errors.New("browser rendering not available")
}

func (n *nopRenderer) Close() {}
