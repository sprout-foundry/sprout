//go:build js

package webcontent

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"syscall/js"
	"time"
)

// pageRenderTimeout bounds one in-page render: building the document,
// letting its runtime script settle, and rasterizing.
const pageRenderTimeout = 60 * time.Second

// pageRenderer screenshots workspace files by asking the host page to render
// them (globalThis.__sproutRender). The browser build has no headless
// browser, but it runs inside one: the page loads the file in an offscreen
// iframe and rasterizes it. Everything else stays unavailable.
type pageRenderer struct{ nopRenderer }

var _ BrowserRenderer = (*pageRenderer)(nil)

func (p *pageRenderer) Screenshot(ctx context.Context, rawURL, outputPath string, viewportWidth, viewportHeight int, _ string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "file" {
		return fmt.Errorf("browser build renders workspace files only, got %q", rawURL)
	}
	bridge := js.Global().Get("__sproutRender")
	if !bridge.Truthy() {
		return fmt.Errorf("page renderer not registered")
	}

	req := js.Global().Get("Object").New()
	req.Set("path", parsed.Path)
	req.Set("width", viewportWidth)
	req.Set("height", viewportHeight)

	png, err := awaitBytes(ctx, bridge.Invoke(req))
	if err != nil {
		return fmt.Errorf("page render failed: %w", err)
	}
	return os.WriteFile(outputPath, png, 0o644)
}

// awaitBytes blocks on a JS Promise<Uint8Array>.
func awaitBytes(ctx context.Context, promise js.Value) ([]byte, error) {
	type outcome struct {
		data []byte
		err  error
	}
	done := make(chan outcome, 1)
	then := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		if len(args) == 0 || !args[0].InstanceOf(js.Global().Get("Uint8Array")) {
			done <- outcome{err: fmt.Errorf("renderer returned no image bytes")}
			return nil
		}
		data := make([]byte, args[0].Get("length").Int())
		js.CopyBytesToGo(data, args[0])
		done <- outcome{data: data}
		return nil
	})
	catch := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		msg := "unknown error"
		if len(args) > 0 {
			if m := args[0].Get("message"); m.Truthy() {
				msg = m.String()
			} else {
				msg = args[0].String()
			}
		}
		done <- outcome{err: fmt.Errorf("%s", strings.TrimSpace(msg))}
		return nil
	})
	defer then.Release()
	defer catch.Release()
	promise.Call("then", then, catch)

	select {
	case o := <-done:
		return o.data, o.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(pageRenderTimeout):
		return nil, fmt.Errorf("timed out after %s", pageRenderTimeout)
	}
}
