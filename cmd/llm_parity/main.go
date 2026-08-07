//go:build darwin && arm64 && cgo && mlx

package main

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
)

func main() {
	modelDir := os.Getenv("HOME") + "/.cache/sprout/models/qwen3-0.6b"
	model, err := llm.NewModel(modelDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	prompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWhat is 2+2?<|im_end|>\n<|im_start|>assistant\n"
	tokens := model.TokenizerEncode(prompt)
	tokens = append([]int{model.BOSID()}, tokens...)
	next := 223 // arbitrary token to test decode

	maxDiff, fullTop5, cacheTop5, err := model.DebugDecodeComparison(tokens, next)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("max logit diff: %.4f (BF16 noise is expected ~0.1-0.5)\n", maxDiff)
	fmt.Printf("full re-encode top-5: %v\n", fullTop5)
	fmt.Printf("cache decode top-5:   %v\n", cacheTop5)

	// The correctness gate is token agreement, not exact logit equality:
	// BF16 fused kernels use a different summation order than the manual path.
	same := len(fullTop5) == len(cacheTop5)
	for i := range fullTop5 {
		if fullTop5[i] != cacheTop5[i] {
			same = false
		}
	}
	if same {
		fmt.Println("PASS: cache decode top-5 tokens match full re-encode")
	} else {
		fmt.Println("FAIL: cache decode diverges at token level")
		os.Exit(1)
	}
}
