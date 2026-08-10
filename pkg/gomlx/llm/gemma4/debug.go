//go:build darwin && arm64 && cgo && mlx

package gemma4

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// dumpArrayF32 writes an array's first few elements to stderr for debugging.
func dumpArrayF32(a tensor.Array, name string, backend tensor.Backend, s tensor.Stream) {
	if a.Dtype() != tensor.Float32 {
		f32, err := backend.AsType(a, tensor.Float32, s)
		if err != nil { return }
		defer f32.Free()
		a = f32
	}
	data, err := a.Float32Data()
	if err != nil { return }
	shape := a.Shape()
	total := len(data)
	n := 10
	if total < n { n = total }
	fmt.Fprintf(os.Stderr, "[gemma4] %s shape=%v first=%v\n", name, shape, data[:n])
}
