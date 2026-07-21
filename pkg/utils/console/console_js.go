//go:build js

// Package console — WASM stub. WASM builds don't have console process
// control; this stub keeps the package compilable.
package console

// SendCtrlBreak is a no-op in WASM builds.
func SendCtrlBreak(_ int) error {
	return nil
}
