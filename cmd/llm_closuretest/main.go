//go:build darwin && arm64 && cgo && mlx

package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// Simulates the swiglu MLP block: 3 matmuls + silu + mul.
func makeSwigluFn(s *mlx.Stream, wg, wu, wd *mlx.Array) mlx.ClosureFunc {
	return func(inputs []*mlx.Array) ([]*mlx.Array, error) {
		h := inputs[0]
		gate, err := mlx.MatMul(h, wg, s)
		if err != nil {
			return nil, err
		}
		defer gate.Free()
		up, err := mlx.MatMul(h, wu, s)
		if err != nil {
			return nil, err
		}
		defer up.Free()
		sig, err := mlx.Sigmoid(gate, s)
		if err != nil {
			return nil, err
		}
		defer sig.Free()
		gated, err := mlx.Multiply(sig, up, s)
		if err != nil {
			return nil, err
		}
		defer gated.Free()
		out, err := mlx.MatMul(gated, wd, s)
		if err != nil {
			return nil, err
		}
		return []*mlx.Array{out}, nil
	}
}

func benchSwiglu(x *mlx.Array, s *mlx.Stream, wg, wu, wd *mlx.Array, iters int) (float64, float64) {
	// Eager
	eager := func() {
		g, _ := mlx.MatMul(x, wg, s)
		defer g.Free()
		u, _ := mlx.MatMul(x, wu, s)
		defer u.Free()
		sg, _ := mlx.Sigmoid(g, s)
		defer sg.Free()
		gd, _ := mlx.Multiply(sg, u, s)
		defer gd.Free()
		o, _ := mlx.MatMul(gd, wd, s)
		defer o.Free()
		o.Eval() // force GPU execution like the model's data read does
	}
	t0 := time.Now()
	for i := 0; i < iters; i++ {
		eager()
	}
	eagerTotal := time.Since(t0)

	// Compiled
	fn := makeSwigluFn(s, wg, wu, wd)
	plain, _ := mlx.NewClosure(fn)
	defer plain.Free()
	compiled, err := plain.Compile(false)
	if err != nil {
		fmt.Println("compile err:", err)
		os.Exit(1)
	}
	defer compiled.Free()

	t0 = time.Now()
	for i := 0; i < iters; i++ {
		out, err := compiled.Apply([]*mlx.Array{x})
		if err != nil {
			fmt.Println("apply err:", err)
			os.Exit(1)
		}
		out[0].Eval()
		out[0].Free()
	}
	compTotal := time.Since(t0)
	return eagerTotal.Seconds(), compTotal.Seconds()
}

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	s, err := mlx.NewGPUStream()
	if err != nil {
		fmt.Println("ERR stream:", err)
		os.Exit(1)
	}
	defer s.Free()

	// Qwen3 shapes: hidden=1024, intermediate=3072
	wg, _ := mlx.NewArrayFromFloat32(make([]float32, 3072*1024), []int{1024, 3072})
	wu, _ := mlx.NewArrayFromFloat32(make([]float32, 3072*1024), []int{1024, 3072})
	wd, _ := mlx.NewArrayFromFloat32(make([]float32, 1024*3072), []int{3072, 1024})
	defer wg.Free()
	defer wu.Free()
	defer wd.Free()
	x, _ := mlx.NewArrayFromFloat32(make([]float32, 1024), []int{1, 1024})
	defer x.Free()

	const iters = 100
	et, ct := benchSwiglu(x, s, wg, wu, wd, iters)
	fmt.Printf("eager:   %.3f ms/call\n", et/iters*1000)
	fmt.Printf("compiled: %.3f ms/call\n", ct/iters*1000)
}
