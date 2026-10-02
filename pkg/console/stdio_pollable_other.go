//go:build !unix || js

package console

// MakeStdioPollable is a no-op where the input readers never put the
// terminal into non-blocking mode.
func MakeStdioPollable() {}
