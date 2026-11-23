//go:build !js

package appcheck

// The check drives the browser through a narrow seam, the way the verify
// package's page checks do (verify.PageBrowser): the production
// implementation adapts pkg/webcontent's Rod renderer — the same Chromium the
// browse_url tool drives — and tests inject a fake so a run is deterministic
// and needs no browser.

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-rod/rod"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// RodBrowser opens one incognito page for the probe.
type RodBrowser interface {
	// OpenSession opens an incognito browser context and a page in it.
	// errors.Is(err, webcontent.ErrBrowserUnavailable) marks a browser that
	// cannot run here (the check then reports "could not run").
	//
	// A browser holds no resources the check must release: the production
	// browser drives a process-lifetime renderer whose Chromium is closed at
	// shutdown (webcontent.CloseDirectBrowser), and a check closes only the
	// session it opened.
	OpenSession(ctx context.Context) (RodSession, error)
}

// RodSession is an open incognito context with one page.
type RodSession interface {
	// Page is the page the probe drives.
	Page() *rod.Page
	// Close releases the page and the context.
	Close()
}

// webcontentSession adapts a webcontent.RodSession to RodSession. The page
// type is a plain *rod.Page, so the probe needs no conversion.
type webcontentSession struct{ s *webcontent.RodSession }

func (w webcontentSession) Page() *rod.Page { return w.s.Page }
func (w webcontentSession) Close()          { w.s.Close() }

// NewWebcontentBrowser returns the production RodBrowser. It renders through
// pkg/webcontent's proxy-free renderer — the same Rod stack browse_url drives,
// but kept off any configured proxy, because the check serves the app on this
// machine and a proxy answering for loopback would turn every page into a
// proxy error. That renderer is process-lifetime and shared by every check,
// and Chromium launches on the first session, so constructing a browser starts
// nothing and a check closes only its own session.
func NewWebcontentBrowser() RodBrowser {
	return webcontentBrowser{renderer: webcontent.GetDirectBrowser()}
}

type webcontentBrowser struct {
	renderer webcontent.BrowserRenderer
}

// OpenSession implements RodBrowser.
func (b webcontentBrowser) OpenSession(ctx context.Context) (RodSession, error) {
	s, err := b.renderer.OpenRodSession(ctx)
	if err != nil {
		return nil, err
	}
	return webcontentSession{s: s}, nil
}

// BrowserUnavailable reports whether err means no browser can run here.
func BrowserUnavailable(err error) bool {
	return errors.Is(err, webcontent.ErrBrowserUnavailable)
}

// NoBrowser is a RodBrowser for a check that cannot open a page: every
// session fails as "browser not available", the way a machine without Chromium
// does. A report written in another environment (a test server, a review
// renderer) reads "could not run" instead of failing.
type NoBrowser struct{}

// OpenSession implements RodBrowser.
func (NoBrowser) OpenSession(context.Context) (RodSession, error) {
	return nil, fmt.Errorf("%w: the browser is not available in this environment", webcontent.ErrBrowserUnavailable)
}
