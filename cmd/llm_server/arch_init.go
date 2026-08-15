//go:build cgo && ((darwin && arm64 && (mlx || ggml)) || (linux && ggml && (arm64 || amd64)))

// Package main registers backend-agnostic architectures at init time.
package main

import (
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/lfm2"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen35"
)
