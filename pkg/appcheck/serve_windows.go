//go:build windows

package appcheck

import (
	"context"
	"errors"
)

// serveApp needs the hosting server's process group, which this build does not
// manage on Windows yet: the check reports that it could not run.
func serveApp(ctx context.Context, opts serveOptions) (*hostedApp, func(), error) {
	return nil, func() {}, errors.New("serving the app for the check is not supported on Windows yet")
}
