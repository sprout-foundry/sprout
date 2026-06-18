//go:build js && wasm

package main

import (
	"syscall/js"
)

func main() {
	// Register the SproutEmbedWasm global object with ONLY the
	// embedding/memory functions. This is the lightweight, lazy-loaded WASM
	// module that runs alongside the main sprout.wasm shell module.
	//
	// The host page loads sprout.wasm (shell) immediately, then defers
	// embedding.wasm until the user actually opens the semantic search
	// or memory UI.
	apiSurface := map[string]interface{}{}
	for name, fn := range embeddingJSFuncs() {
		apiSurface[name] = fn
	}

	js.Global().Set("SproutEmbedWasm", js.ValueOf(apiSurface))

	// Block forever so the WASM module stays alive.
	c := make(chan struct{}, 0)
	<-c
}
