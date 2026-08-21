//go:build js

package automate

// CheckMemoryFloor is a no-op on WASM: there is no subprocess launch and no
// /proc to read in the browser sandbox.
func CheckMemoryFloor() error { return nil }
