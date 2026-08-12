//go:build darwin && arm64 && cgo && mlx

// Package main registers MLX-only architectures at init time.
package main

import (
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/gemma4"
)
