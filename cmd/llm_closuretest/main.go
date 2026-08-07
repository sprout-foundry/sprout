//go:build darwin && arm64 && cgo && mlx

package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	s, err := mlx.NewGPUStream()
	if err != nil {
		fmt.Println("ERR stream:", err)
		os.Exit(1)
	}
	defer s.Free()

	// 4x128 weight, q4 group64 → weight [4,16] u32, scales [4,2], biases [4,2]
	data := make([]float32, 4*128)
	for i := range data {
		data[i] = float32(i+1) * 0.0137
	}
	w, err := mlx.NewArrayFromFloat32(data, []int{4, 128})
	if err != nil {
		fmt.Println("ERR w:", err)
		os.Exit(1)
	}
	defer w.Free()

	parts, err := mlx.Quantize(w, 64, 4, "affine", s)
	if err != nil {
		fmt.Println("ERR quantize:", err)
		os.Exit(1)
	}
	defer func() {
		for _, p := range parts {
			p.Free()
		}
	}()

	qw, _ := parts[0].Uint32Data()
	sc, _ := parts[1].Float32Data()
	var bi []float32
	if len(parts) > 2 {
		bi, _ = parts[2].Float32Data()
	}
	fmt.Printf("weight(%d): %v\n", len(qw), qw[:16])
	fmt.Printf("scales(%d): %v\n", len(sc), sc)
	fmt.Printf("biases(%d): %v\n", len(bi), bi)
}
