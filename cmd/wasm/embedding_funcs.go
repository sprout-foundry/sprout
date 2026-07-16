//go:build js && wasm

// SP-100 Phase 1: surface the embedding backend as SproutWasm.embedding*.
//
// The WASM shell (cmd/wasm/, sprout.wasm) exposes three JS-callable
// functions that give host pages a single control surface for the
// embedding pipeline. The actual ONNX bridge lives in TypeScript land
// (webui/src/services/sproutONNXBridge.ts + onnxruntimeWebLoader.ts);
// this Go-side layer is the control panel, not the engine.
//
// Functions:
//   SproutWasm.embeddingModel(): string
//     Returns the active embedding model name. The host page may
//     override the default via globalThis.__sproutEmbeddingModel.
//
//   SproutWasm.switchEmbeddingBackend(name: "static"|"onnx-web"): string
//     Asks the host page to switch embedding backends. The actual
//     install/uninstall happens in TS via globalThis.__sproutSwitchEmbeddingBackend.
//     Returns the new backend name on success; throws on unknown name.
//
//   SproutWasm.embeddingBackendStatus(): { backend, model, dimensions, ready }
//     Reports the current bridge state. Always returns an object —
//     callers don't need null checks.

package main

import (
	"fmt"
	"syscall/js"
)

// EmbeddingModelDefault is the model name returned by embeddingModelFunc
// when no host-side bridge has set a custom value via
// globalThis.__sproutEmbeddingModel. Mirrors the "gemma-300m" model name
// that BrowserONNXProvider installs on the JS bridge.
const EmbeddingModelDefault = "gemma-300m"

// Backend names. The Go side rejects anything outside this set so the
// host page can't be asked to install an unsupported backend by accident.
const (
	BackendStatic  = "static"
	BackendOnnxWeb = "onnx-web"
)

// ─── JS Surface ─────────────────────────────────────────────────

// embeddingJSFuncs returns the SproutWasm.embedding* entries to merge
// into the shell module's apiSurface. Returned map is consumed by
// cmd/wasm/main.go.
func embeddingJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"embeddingModel":         js.FuncOf(embeddingModelFunc),
		"switchEmbeddingBackend": js.FuncOf(switchEmbeddingBackendFunc),
		"embeddingBackendStatus": js.FuncOf(embeddingBackendStatusFunc),
	}
}

// ─── Handlers ───────────────────────────────────────────────────

// embeddingModelFunc returns the active embedding model name. Reads
// globalThis.__sproutEmbeddingModel first (host page override), falls
// back to EmbeddingModelDefault. The string is returned synchronously
// — there's no async work, no need for a Promise wrapper.
func embeddingModelFunc(_ js.Value, _ []js.Value) interface{} {
	if v := js.Global().Get("__sproutEmbeddingModel"); v.Type() == js.TypeString {
		return v.String()
	}
	return EmbeddingModelDefault
}

// switchEmbeddingBackendFunc delegates to the host page's
// globalThis.__sproutSwitchEmbeddingBackend helper. The TS side owns
// the actual install/uninstall of the __sproutONNX bridge because the
// bridge requires creating a BrowserONNXProvider instance, which is
// TypeScript-only.
//
// Returns the new backend name on success. On unknown name OR missing
// helper, returns a JS Error so the caller distinguishes from a normal
// return value.
func switchEmbeddingBackendFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) < 1 || args[0].Type() != js.TypeString {
		return js.Global().Get("Error").New(
			"switchEmbeddingBackend: missing or non-string backend name",
		)
	}
	name := args[0].String()
	if name != BackendStatic && name != BackendOnnxWeb {
		return js.Global().Get("Error").New(
			fmt.Sprintf("switchEmbeddingBackend: unknown backend %q (expected %q or %q)",
				name, BackendStatic, BackendOnnxWeb),
		)
	}
	helper := js.Global().Get("__sproutSwitchEmbeddingBackend")
	if helper.IsUndefined() || helper.Type() != js.TypeFunction {
		return js.Global().Get("Error").New(
			"switchEmbeddingBackend: host page hasn't installed __sproutSwitchEmbeddingBackend helper",
		)
	}
	helper.Invoke(name)
	return name
}

// embeddingBackendStatusFunc returns the current embedding backend state.
// Reads __sproutONNX on globalThis; if absent, reports the static
// backend. If present, extracts modelName / dimensions from the bridge.
// Always returns a plain object — no throw paths.
func embeddingBackendStatusFunc(_ js.Value, _ []js.Value) interface{} {
	bridge := js.Global().Get("__sproutONNX")
	if bridge.IsUndefined() || bridge.IsNull() {
		return map[string]interface{}{
			"backend":    BackendStatic,
			"model":      embeddingModelFunc(js.Value{}, nil),
			"dimensions": 0,
			"ready":      false,
		}
	}
	status := map[string]interface{}{
		"backend": BackendOnnxWeb,
		"model":   EmbeddingModelDefault,
		"ready":   true,
	}
	if v := bridge.Get("modelName"); v.Type() == js.TypeString {
		status["model"] = v.String()
	} else {
		status["model"] = embeddingModelFunc(js.Value{}, nil)
	}
	if v := bridge.Get("dimensions"); v.Type() == js.TypeNumber {
		status["dimensions"] = v.Int()
	}
	return status
}
