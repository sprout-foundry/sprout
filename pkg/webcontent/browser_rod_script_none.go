//go:build js

package webcontent

// EvalStatement has no meaning without a browser and returns the script
// unchanged.
func EvalStatement(script string) string {
	return script
}
