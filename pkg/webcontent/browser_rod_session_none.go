//go:build js

package webcontent

import "context"

// RodSession is unavailable in the browser build: there is no headless
// browser to own a page (see browser_none.go).
type RodSession struct{}

// Close is a no-op for the unavailable session.
func (s *RodSession) Close() {}

// OpenRodSession reports that no browser is available here.
func (p *pageRenderer) OpenRodSession(_ context.Context) (*RodSession, error) {
	return nil, ErrBrowserUnavailable
}
