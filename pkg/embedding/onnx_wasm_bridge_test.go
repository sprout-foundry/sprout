//go:build wasm

// Tests for the WASM-side ONNX bridge. Run with:
//
//	GOOS=js GOARCH=wasm go test \
//	  -exec "$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
//	  ./pkg/embedding/ -run TestONNXBridge
//
// Each test installs a fake `globalThis.__sproutONNX` that mimics the
// real onnxruntime-web provider's shape (embed/embedBatch returning
// Promises of Float32Array), constructs the bridge, and asserts the
// Go-side round-trip matches the JS-side payload.

package embedding

import (
	"context"
	"syscall/js"
	"testing"
	"time"
)

// withFakeBridge installs a mock __sproutONNX for the duration of the test
// and tears it down afterwards via t.Cleanup. The behavior callback receives
// each incoming text and returns the Float32Array the embed call should
// resolve to.
func withFakeBridge(t *testing.T, embedFn func(text string) []float32) {
	t.Helper()
	g := js.Global()

	makeFloat32Array := func(vec []float32) js.Value {
		// Construct a JS Float32Array from a Go []float32. We can't pass the
		// slice directly via js.ValueOf — typed arrays need explicit
		// construction. Build a JS Array, then convert.
		arr := g.Get("Array").New(len(vec))
		for i, v := range vec {
			arr.SetIndex(i, js.ValueOf(float64(v)))
		}
		return g.Get("Float32Array").New(arr)
	}

	embedFunc := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		text := ""
		if len(args) > 0 && args[0].Type() == js.TypeString {
			text = args[0].String()
		}
		vec := embedFn(text)
		return g.Get("Promise").Call("resolve", makeFloat32Array(vec))
	})
	embedBatchFunc := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		var batch js.Value
		if len(args) > 0 {
			batch = args[0]
		}
		results := g.Get("Array").New(batch.Length())
		for i := 0; i < batch.Length(); i++ {
			text := ""
			if batch.Index(i).Type() == js.TypeString {
				text = batch.Index(i).String()
			}
			results.SetIndex(i, makeFloat32Array(embedFn(text)))
		}
		return g.Get("Promise").Call("resolve", results)
	})

	provider := g.Get("Object").New()
	provider.Set("embed", embedFunc)
	provider.Set("embedBatch", embedBatchFunc)
	provider.Set("modelHash", js.ValueOf("test-bridge-hash"))
	provider.Set("modelName", js.ValueOf("test-onnx-bridge"))
	provider.Set("dimensions", js.ValueOf(3))

	g.Set("__sproutONNX", provider)

	t.Cleanup(func() {
		// Detach the global; releasing the JS funcs prevents goroutine leaks
		// from the syscall/js bookkeeping.
		g.Delete("__sproutONNX")
		embedFunc.Release()
		embedBatchFunc.Release()
	})
}

func TestONNXBridge_DetectsAbsentProvider(t *testing.T) {
	// Ensure no leftover from another test; defensive.
	js.Global().Delete("__sproutONNX")
	_, err := NewONNXEmbeddingProvider(context.Background(), nil, "", "", 768, 768)
	if err == nil {
		t.Fatal("expected errWASMNotSupported when __sproutONNX is unset, got nil")
	}
}

func TestONNXBridge_EmbedRoundTrip(t *testing.T) {
	withFakeBridge(t, func(text string) []float32 {
		// Return a deterministic vector derived from the input so we can
		// assert the bridge isn't swapping or aliasing batch elements.
		return []float32{float32(len(text)), 0.5, -0.5}
	})

	provider, err := NewONNXEmbeddingProvider(context.Background(), nil, "", "", 3, 768)
	if err != nil {
		t.Fatalf("NewONNXEmbeddingProvider: %v", err)
	}
	if provider.Dimensions() != 3 {
		t.Errorf("Dimensions = %d, want 3 (from JS-side override)", provider.Dimensions())
	}
	if provider.Name() != "test-onnx-bridge" {
		t.Errorf("Name = %q, want test-onnx-bridge", provider.Name())
	}
	if provider.ModelHash() != "test-bridge-hash" {
		t.Errorf("ModelHash = %q, want test-bridge-hash", provider.ModelHash())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := provider.Embed(ctx, "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	want := []float32{5.0, 0.5, -0.5} // len("hello") == 5
	if !float32SlicesEqual(got, want) {
		t.Errorf("Embed(hello) = %v, want %v", got, want)
	}
}

func TestONNXBridge_EmbedBatchPreservesOrder(t *testing.T) {
	withFakeBridge(t, func(text string) []float32 {
		return []float32{float32(len(text)), 0, 0}
	})

	provider, err := NewONNXEmbeddingProvider(context.Background(), nil, "", "", 3, 768)
	if err != nil {
		t.Fatalf("NewONNXEmbeddingProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := provider.EmbedBatch(ctx, []string{"a", "bb", "ccc"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	for i, want := range []float32{1, 2, 3} {
		if results[i][0] != want {
			t.Errorf("results[%d][0] = %v, want %v", i, results[i][0], want)
		}
	}
}

func TestONNXBridge_PromiseRejectionSurfaces(t *testing.T) {
	g := js.Global()
	rejectFunc := js.FuncOf(func(_ js.Value, _ []js.Value) interface{} {
		return g.Get("Promise").Call("reject", js.ValueOf("bridge boom"))
	})
	provider := g.Get("Object").New()
	provider.Set("embed", rejectFunc)
	provider.Set("embedBatch", rejectFunc)
	g.Set("__sproutONNX", provider)
	t.Cleanup(func() {
		g.Delete("__sproutONNX")
		rejectFunc.Release()
	})

	p, err := NewONNXEmbeddingProvider(context.Background(), nil, "", "", 768, 768)
	if err != nil {
		t.Fatalf("NewONNXEmbeddingProvider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = p.Embed(ctx, "x")
	if err == nil {
		t.Fatal("expected rejection to surface as an error")
	}
}

func TestONNXBridge_ContextCancellation(t *testing.T) {
	g := js.Global()
	// A promise that never resolves — simulates a hung JS side.
	hangingFunc := js.FuncOf(func(_ js.Value, _ []js.Value) interface{} {
		return g.Get("Promise").New(js.FuncOf(func(_ js.Value, _ []js.Value) interface{} {
			return nil // resolve never called
		}))
	})
	provider := g.Get("Object").New()
	provider.Set("embed", hangingFunc)
	provider.Set("embedBatch", hangingFunc)
	g.Set("__sproutONNX", provider)
	t.Cleanup(func() {
		g.Delete("__sproutONNX")
		hangingFunc.Release()
	})

	p, err := NewONNXEmbeddingProvider(context.Background(), nil, "", "", 768, 768)
	if err != nil {
		t.Fatalf("NewONNXEmbeddingProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = p.Embed(ctx, "hang")
	if err == nil {
		t.Fatal("expected cancellation to surface as an error")
	}
}

func float32SlicesEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
