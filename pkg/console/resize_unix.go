//go:build unix && !js
// +build unix,!js

package console

// startResizeWatcher is a no-op on Unix. The StatusFooter's existing
// watchResize goroutine already listens for SIGWINCH, calls its own Resize(),
// and then calls notifyResizeSubscribers(). Starting a second SIGWINCH
// handler here would double-fire resize events.
//
// Returns a no-op stop function.
func startResizeWatcher(_ func(), _ func()) (stop func()) {
	return func() {}
}
