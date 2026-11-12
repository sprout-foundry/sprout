//go:build js

package tools

// StartCodeIndex does nothing in the browser build: the code index needs
// SQLite and a git checkout.
func StartCodeIndex(dir string) {}
