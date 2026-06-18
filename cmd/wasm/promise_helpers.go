//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall/js"
	"time"
)

// ─── JS Promise Helpers ─────────────────────────────────────────────
// Shared across all func files in the WASM shell and embedding modules.

// asPromise wraps a Go function that does async work into a JS Promise. The
// browser side gets `await SproutWasm.searchSemantic(...)` semantics for free.
// Errors are surfaced as rejected promises; success results are passed to
// resolve() as native JS values (after running through marshalJS).
func asPromise(do func(ctx context.Context) (interface{}, error)) interface{} {
	return asPromiseWithTimeout(60*time.Second, do)
}

// asPromiseWithTimeout is asPromise with an explicit timeout — use for
// long-running calls (chat completions, agent loops) that the default
// 60s ceiling on asPromise would prematurely cancel. Pass 0 to disable
// the timeout entirely (caller is responsible for cancellation).
func asPromiseWithTimeout(timeout time.Duration, do func(ctx context.Context) (interface{}, error)) interface{} {
	promiseCtor := js.Global().Get("Promise")
	if promiseCtor.IsUndefined() {
		// No Promise constructor available — fall through to a synchronous
		// call. This path only happens in non-browser hosts.
		result, err := do(context.Background())
		if err != nil {
			return map[string]interface{}{"error": err.Error()}
		}
		return marshalJS(result)
	}
	return promiseCtor.New(js.FuncOf(func(_ js.Value, pargs []js.Value) interface{} {
		resolve, reject := pargs[0], pargs[1]
		go func() {
			var ctx context.Context
			var cancel context.CancelFunc
			if timeout > 0 {
				ctx, cancel = context.WithTimeout(context.Background(), timeout)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			result, err := do(ctx)
			if err != nil {
				reject.Invoke(js.ValueOf(err.Error()))
				return
			}
			resolve.Invoke(marshalJS(result))
		}()
		return nil
	}))
}

// marshalJS converts a Go value into a js.Value the browser can consume.
// We go through JSON because the values we return are already simple (no
// channels, funcs, or pointers) and round-tripping through JSON gives us
// guaranteed structural identity with the browser side.
func marshalJS(v interface{}) js.Value {
	if v == nil {
		return js.Null()
	}
	data, err := json.Marshal(v)
	if err != nil {
		return js.ValueOf(fmt.Sprintf("marshal error: %v", err))
	}
	return js.Global().Get("JSON").Call("parse", string(data))
}

// argString reads a positional string argument from the JS call site, with
// a default for missing/non-string slots. Keeps callsite parsing terse.
func argString(args []js.Value, idx int, def string) string {
	if idx >= len(args) || args[idx].IsUndefined() || args[idx].IsNull() {
		return def
	}
	if args[idx].Type() != js.TypeString {
		return def
	}
	return args[idx].String()
}

// argInt reads a positional integer argument from the JS call site, with
// a default for missing/non-number slots.
func argInt(args []js.Value, idx int, def int) int {
	if idx >= len(args) || args[idx].IsUndefined() || args[idx].IsNull() {
		return def
	}
	if args[idx].Type() != js.TypeNumber {
		return def
	}
	return args[idx].Int()
}

// argFloat32 reads a positional float32 argument from the JS call site, with
// a default for missing/non-number slots.
func argFloat32(args []js.Value, idx int, def float32) float32 {
	if idx >= len(args) || args[idx].IsUndefined() || args[idx].IsNull() {
		return def
	}
	if args[idx].Type() != js.TypeNumber {
		return def
	}
	return float32(args[idx].Float())
}
