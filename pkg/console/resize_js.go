//go:build js
// +build js

package console

import "sync"

// startResizeWatcher is a no-op on WASM/js: there is no terminal to resize.
// Returns a no-op stop function.
func startResizeWatcher(_ func(), _ func()) (stop func()) {
	var once sync.Once
	return func() {
		once.Do(func() {})
	}
}
