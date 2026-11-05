//go:build !js

package webcontent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A page that never goes network-idle — a polling loop, an analytics beacon, a
// chat widget — used to fail the whole capture, discarding content that had
// already rendered. The wait is now bounded and the DOM is taken as it stands.
func TestCaptureDOMReturnsContentFromANeverIdlePage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h1>content is here</h1><script>
		setInterval(() => fetch('/ping'), 200);
		</script></body></html>`)
	})
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	r := NewBrowserRenderer()
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	html, err := r.CaptureDOM(ctx, srv.URL, 0, 0, "")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("a never-idle page failed the capture: %v", err)
	}
	if !strings.Contains(html, "content is here") {
		t.Errorf("capture did not return the rendered content: %q", html)
	}
	// The wait must be bounded by stableWaitBudget, not the render timeout.
	if elapsed > stableWaitBudget+renderTimeout/2 {
		t.Errorf("capture took %s; the stable wait should be bounded by %s", elapsed, stableWaitBudget)
	}
}

func TestIsTransientNavigationError(t *testing.T) {
	transient := []string{
		"net::ERR_CONNECTION_RESET",
		"net::ERR_TIMEOUT",
		"net::ERR_EMPTY_RESPONSE",
		"net::ERR_HTTP2_PROTOCOL_ERROR",
		"net::ERR_CONNECTION_CLOSED at https://example.com",
	}
	for _, msg := range transient {
		if !IsTransientNavigationError(errors.New(msg)) {
			t.Errorf("%q should be transient", msg)
		}
	}

	deterministic := []string{
		"net::ERR_NAME_NOT_RESOLVED",
		"net::ERR_CONNECTION_REFUSED",
		"net::ERR_INVALID_URL",
		"net::ERR_CERT_AUTHORITY_INVALID",
		"unknown browser session",
	}
	for _, msg := range deterministic {
		if IsTransientNavigationError(errors.New(msg)) {
			t.Errorf("%q should not be transient (retrying only delays the error)", msg)
		}
	}

	if IsTransientNavigationError(nil) {
		t.Error("nil should not be classified as transient")
	}
}
