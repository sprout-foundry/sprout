//go:build !js

// `sprout capture <url>` serializes a running page as a standalone HTML
// snapshot: a copy of the page (its DOM, styles and media inlined) plus a PNG
// screenshot, on the same managed Chromium browse_url drives. It is released
// for the platform to call (change preview): the platform runs the CLI on its
// fleet machines and stores the snapshot.
package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/capture"
)

type captureFlags struct {
	viewport string
	headers  []string
	cookies  []string
	waitMs   int
	selector string
	maxBytes int
	out      string
}

var captureFlagState captureFlags

var captureCmd = &cobra.Command{
	Use:   "capture <url>",
	Short: "Capture a page as a standalone HTML snapshot plus a PNG screenshot",
	Long: `Capture a page as a standalone HTML snapshot plus a PNG screenshot.

Loads the URL in the managed Chromium, waits for network idle plus the
--wait-ms grace period, then serializes the rendered page: the DOM with its
styles inlined (custom properties, kept CSS rules, fonts as data URLs,
images as data URLs, canvases snapshotted, iframes neutralized) and prints
the result as JSON (or writes snapshot.html, screenshot.png and capture.json
to --output).

Examples:
  sprout capture https://example.com
  sprout capture https://example.com --viewport phone --wait-ms 1500
  sprout capture https://example.com --selector '#main' --max-bytes 200000
  sprout capture https://example.com --output /tmp/capture`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCapture(cmd, captureFlagState, args[0], cmd.OutOrStdout())
	},
}

func init() {
	captureCmd.Flags().StringVar(&captureFlagState.viewport, "viewport", "laptop", "Viewport: phone (390x844 @3x) or laptop (1440x900)")
	captureCmd.Flags().StringArrayVar(&captureFlagState.headers, "header", nil, "Extra request header 'Name: value' (repeatable)")
	captureCmd.Flags().StringArrayVar(&captureFlagState.cookies, "cookie", nil, "Extra cookie 'name=value; domain' (repeatable)")
	captureCmd.Flags().IntVar(&captureFlagState.waitMs, "wait-ms", 800, "Extra settle time after network idle (ms)")
	captureCmd.Flags().StringVar(&captureFlagState.selector, "selector", "", "Capture only this element and what it needs")
	captureCmd.Flags().IntVar(&captureFlagState.maxBytes, "max-bytes", 400000, "Max snapshot size (drops fonts, then downscales images, then trims)")
	captureCmd.Flags().StringVar(&captureFlagState.out, "output", "", "Write snapshot.html, screenshot.png and capture.json here instead of printing JSON")
	captureCmd.Flags().StringVar(&captureFlagState.out, "out", "", "Alias for --output")
	_ = captureCmd.Flags().MarkHidden("out")
	rootCmd.AddCommand(captureCmd)
}

var errCaptureFailed = errors.New("capture failed")

// RunCapture is the entry point tests drive. It returns the exit code (0 on
// success, 1 on failure); the one-line reason is printed by the caller.
func RunCapture(cmd *cobra.Command, flags captureFlags, url string, out io.Writer) (int, error) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := capture.Run(ctx, capture.Options{
		URL:      url,
		Viewport: flags.viewport,
		Headers:  flags.headers,
		Cookies:  flags.cookies,
		WaitMs:   flags.waitMs,
		Selector: flags.selector,
		MaxBytes: flags.maxBytes,
	})
	if err != nil {
		_ = writeJSONTo(out, captureErrorJSON(url, err))
		return 1, nil
	}
	if flags.out != "" {
		if err := writeCaptureFiles(flags.out, res); err != nil {
			_ = writeJSONTo(out, captureErrorJSON(url, err))
			return 1, nil
		}
		_, _ = fmt.Fprintf(out, "Captured %s (%s) → %s\n", res.FinalURL, res.Viewport, flags.out)
		return 0, nil
	}
	if err := writeJSONTo(out, res); err != nil {
		return 1, err
	}
	return 0, nil
}

// runCapture is the cobra body.
func runCapture(cmd *cobra.Command, flags captureFlags, url string, out io.Writer) error {
	code, err := RunCapture(cmd, flags, url, out)
	if err != nil {
		return err
	}
	if code != 0 {
		return errCaptureFailed
	}
	return nil
}

func writeJSONTo(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", " ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func captureErrorJSON(url string, err error) map[string]any {
	return map[string]any{
		"url":   url,
		"error": oneLine(err.Error()),
	}
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

func writeCaptureFiles(dir string, res *capture.Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.html"), []byte(res.HTML), 0o644); err != nil {
		return fmt.Errorf("write snapshot.html: %w", err)
	}
	shot, err := base64.StdEncoding.DecodeString(res.ScreenshotPNGBase64)
	if err != nil {
		return fmt.Errorf("decode screenshot: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "screenshot.png"), shot, 0o644); err != nil {
		return fmt.Errorf("write screenshot.png: %w", err)
	}
	meta := struct {
		URL        string `json:"url"`
		FinalURL   string `json:"final_url"`
		Title      string `json:"title"`
		Viewport   string `json:"viewport"`
		HTMLBytes  int    `json:"html_bytes"`
		Trimmed    bool   `json:"trimmed"`
		DurationMs int64  `json:"duration_ms"`
	}{
		URL:        res.URL,
		FinalURL:   res.FinalURL,
		Title:      res.Title,
		Viewport:   res.Viewport,
		HTMLBytes:  res.HTMLBytes,
		Trimmed:    res.Trimmed,
		DurationMs: res.DurationMs,
	}
	if err := writeJSONFile(filepath.Join(dir, "capture.json"), meta); err != nil {
		return fmt.Errorf("write capture.json: %w", err)
	}
	return nil
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
