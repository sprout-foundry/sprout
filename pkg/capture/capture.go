//go:build !js

// Package capture serializes a running page as a standalone HTML snapshot:
// the rendered DOM with its styles inlined (custom properties, kept CSS rules,
// fonts as data URLs, images as data URLs, canvases snapshotted) plus a PNG
// screenshot, on the same managed Chromium browse_url drives (pkg/webcontent).
package capture

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

const (
	// defaultViewportWidth/Height is the fallback when --viewport is unset.
	viewportLaptop   = "laptop"
	viewportPhone    = "phone"
	laptopWidth      = 1440
	laptopHeight     = 900
	phoneWidth       = 390
	phoneHeight      = 844
	phoneDeviceScale = 3
)

// serializerJS is the embedded page-side serializer.
//
//go:embed serializer.js
var serializerJS string

// Viewports holds the two captured viewports.
type viewport struct {
	Name   string
	W, H   int
	DPR    float64
	Mobile bool
}

func viewportFor(name string) (viewport, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", viewportLaptop:
		return viewport{viewportLaptop, laptopWidth, laptopHeight, 1, false}, nil
	case viewportPhone:
		return viewport{viewportPhone, phoneWidth, phoneHeight, float64(phoneDeviceScale), true}, nil
	}
	return viewport{}, fmt.Errorf("unknown --viewport %q (want phone|laptop)", name)
}

// Options carries the capture flags.
type Options struct {
	URL      string
	Viewport string
	Headers  []string
	Cookies  []string
	WaitMs   int
	Selector string
	MaxBytes int
}

// Result is one captured page.
type Result struct {
	URL                 string `json:"url"`
	FinalURL            string `json:"final_url"`
	Title               string `json:"title"`
	Viewport            string `json:"viewport"`
	HTML                string `json:"html"`
	HTMLBytes           int    `json:"html_bytes"`
	Trimmed             bool   `json:"trimmed"`
	ScreenshotPNGBase64 string `json:"screenshot_png_base64"`
	DurationMs          int64  `json:"duration_ms"`
}

// serializerResult is the JSON the page-side serializer returns.
type serializerResult struct {
	HTML     string `json:"html"`
	Trimmed  bool   `json:"trimmed"`
	Title    string `json:"title"`
	FinalURL string `json:"finalUrl"`
	Error    string `json:"error"`
}

// Run captures the page at opts.URL and returns the result. The caller owns
// anything it writes: nothing here touches the filesystem.
func Run(ctx context.Context, opts Options) (*Result, error) {
	start := time.Now()
	vp, err := viewportFor(opts.Viewport)
	if err != nil {
		return nil, err
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 400000
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	renderer := webcontent.GetGlobalBrowser()
	session, err := renderer.OpenRodSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("open browser session: %w", err)
	}
	defer session.Close()
	page := session.Page

	if opts.Headers != nil || len(opts.Headers) > 0 {
		pairs := headerPairs(opts.Headers)
		if len(pairs) > 0 {
			reset, err := page.SetExtraHeaders(pairs)
			if err != nil {
				return nil, fmt.Errorf("set extra headers: %w", err)
			}
			defer reset()
		}
	}

	if len(opts.Cookies) > 0 {
		params := parseCookies(opts.Cookies, opts.URL)
		if err := page.SetCookies(params); err != nil {
			return nil, fmt.Errorf("set cookies: %w", err)
		}
	}

	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             vp.W,
		Height:            vp.H,
		DeviceScaleFactor: vp.DPR,
		Mobile:            vp.Mobile,
	}); err != nil {
		return nil, fmt.Errorf("set viewport: %w", err)
	}

	if err := page.Timeout(30 * time.Second).Navigate(opts.URL); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		// WaitLoad may error on navigation failures the caller already sees;
		// proceed with whatever is rendered.
		_ = err
	}

	// Network idle, bounded: a page that never goes idle proceeds.
	if err := page.WaitIdle(5 * time.Second); err != nil {
		_ = err
	}
	if opts.WaitMs > 0 {
		time.Sleep(time.Millisecond * time.Duration(opts.WaitMs))
	}

	// Screenshot first: the live page is untouched by the serializer's temp
	// <style> injection (which it removes), but capturing beforehand removes
	// the question entirely.
	shotBytes, err := page.Screenshot(false, nil)
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}

	result, err := serializePage(ctx, page, opts, vp)
	if err != nil {
		return nil, err
	}

	return &Result{
		URL:                 opts.URL,
		FinalURL:            result.FinalURL,
		Title:               result.Title,
		Viewport:            vp.Name,
		HTML:                result.HTML,
		HTMLBytes:           len(result.HTML),
		Trimmed:             result.Trimmed,
		ScreenshotPNGBase64: base64.StdEncoding.EncodeToString(shotBytes),
		DurationMs:          time.Since(start).Milliseconds(),
	}, nil
}

// serializePage runs the embedded serializer in the page. The level is lifted
// by the serializer's built-in escalation loop (fonts dropped, then images
// downscaled, then trimmed to the largest visible section).
func serializePage(ctx context.Context, page *rod.Page, opts Options, vp viewport) (*serializerResult, error) {
	optsJSON, err := json.Marshal(map[string]any{
		"selector":       opts.Selector,
		"maxBytes":       opts.MaxBytes,
		"level":          0,
		"viewportWidth":  vp.W,
		"viewportHeight": vp.H,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal serializer options: %w", err)
	}
	script := strings.Replace(serializerJS, "__OPTS__", string(optsJSON), 1)

	res, err := page.Eval(webcontent.EvalStatement(script))
	if err != nil {
		return nil, fmt.Errorf("run serializer: %w", err)
	}
	raw := res.Value.JSON("", "")
	var parsed serializerResult
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		// res.Value.JSON for a string produces "..." (quoted). Unwrap it.
		var inner string
		if err2 := json.Unmarshal([]byte(raw), &inner); err2 != nil {
			return nil, fmt.Errorf("parse serializer result: %w (raw: %.200s)", err, raw)
		}
		if err3 := json.Unmarshal([]byte(inner), &parsed); err3 != nil {
			return nil, fmt.Errorf("parse serializer result: %w (raw: %.200s)", err3, raw)
		}
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("%s", parsed.Error)
	}
	if parsed.HTML == "" {
		return nil, fmt.Errorf("serializer returned no html")
	}
	return &parsed, nil
}

func headerPairs(headers []string) []string {
	var out []string
	for _, h := range headers {
		name, value, found := strings.Cut(h, ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" {
			continue
		}
		out = append(out, name, value)
	}
	return out
}

func parseCookies(cookies []string, url string) []*proto.NetworkCookieParam {
	var out []*proto.NetworkCookieParam
	for _, c := range cookies {
		parts := strings.Split(c, ";")
		if len(parts) == 0 {
			continue
		}
		name, value, found := strings.Cut(strings.TrimSpace(parts[0]), "=")
		if !found || name == "" {
			continue
		}
		param := &proto.NetworkCookieParam{
			Name:  strings.TrimSpace(name),
			Value: strings.TrimSpace(value),
			URL:   url,
		}
		for _, extra := range parts[1:] {
			extra = strings.TrimSpace(extra)
			if strings.HasPrefix(strings.ToLower(extra), "domain=") {
				param.Domain = strings.TrimSpace(extra[len("domain="):])
			} else if extra != "" {
				// Bare segment: treat as domain.
				param.Domain = extra
			}
		}
		out = append(out, param)
	}
	return out
}
