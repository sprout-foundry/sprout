//go:build !js

// browser_rod_session.go exposes one interactive Rod page for callers that
// drive their own browser workflow (the app check): an incognito context
// opened the same way browse_url opens one, with a page the caller keeps
// across a whole run. The rest of the renderer API captures a result per
// call; a multi-page, form-filling workflow needs the page itself.

package webcontent

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-rod/rod"
)

// RodSession is an open incognito browser context with one page in it. Close
// it when done; it releases both, once.
type RodSession struct {
	Page      *rod.Page
	incognito *rod.Browser
	once      sync.Once
}

// Close closes the page and the incognito context. It is safe to call twice.
func (s *RodSession) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.Page != nil {
			_ = s.Page.Close()
		}
		if s.incognito != nil {
			_ = s.incognito.Close()
		}
	})
}

// OpenRodSession opens an incognito context and a page in it, launching the
// browser on first use.
func (r *rodRenderer) OpenRodSession(ctx context.Context) (*RodSession, error) {
	incognito, page, err := r.openIncognitoPage(ctx)
	if err != nil {
		return nil, fmt.Errorf("open browser page: %w", err)
	}
	return &RodSession{Page: page, incognito: incognito}, nil
}
