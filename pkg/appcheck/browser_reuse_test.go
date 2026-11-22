//go:build !js

package appcheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// TestRepeatedChecksReuseTheBrowser is what keeps a long-lived process (the
// agent's REPL, which runs a check whenever it likes) from breaking after its
// first check: the production browser drives a process-lifetime renderer, so
// a check must never close it. This drives two real sessions in a row — a
// closed renderer answers the second with "browser renderer has been closed".
// Skipped when no browser can run here.
func TestRepeatedChecksReuseTheBrowser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<h1>home</h1>"))
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for attempt := 1; attempt <= 2; attempt++ {
		session, err := NewWebcontentBrowser().OpenSession(ctx)
		if err != nil {
			if errors.Is(err, webcontent.ErrBrowserUnavailable) {
				t.Skipf("no browser available: %v", err)
			}
			t.Fatalf("attempt %d: OpenSession: %v", attempt, err)
		}
		session.Page().Navigate(srv.URL)
		if err := session.Page().WaitLoad(); err != nil {
			session.Close()
			t.Fatalf("attempt %d: WaitLoad: %v", attempt, err)
		}
		text, err := session.Page().Element("h1")
		if err != nil {
			session.Close()
			t.Fatalf("attempt %d: find h1: %v", attempt, err)
		}
		if got, err := text.Text(); err != nil || got != "home" {
			session.Close()
			t.Fatalf("attempt %d: h1 = %q (err %v), want \"home\"", attempt, got, err)
		}
		session.Close()
	}
}
