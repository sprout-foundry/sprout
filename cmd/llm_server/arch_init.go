//go:build (darwin || linux) && arm64 && cgo && (mlx || ggml)

// Package main registers backend-agnostic architectures at init time.
package main

import (
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/lfm2"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen35"
)
