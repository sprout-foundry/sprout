//go:build (darwin || linux) && arm64 && cgo && ggml

// Package main registers the GGML tensor backend at init time.
package main

import _ "github.com/sprout-foundry/sprout/pkg/tensor/ggml"
