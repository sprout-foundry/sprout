//go:build !js

package webcontent

// EvalStatement wraps a page script into the callable form Rod evaluates (see
// scriptAsStatement), so a caller driving its own page can write plain page
// JavaScript — statements, expressions, or an IIFE — and have it run in global
// scope with document and window available.
func EvalStatement(script string) string {
	return scriptAsStatement(script)
}
