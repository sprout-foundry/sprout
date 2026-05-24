//go:build js && wasm

package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/llmproxy"
)

// wasmStreamReader implements io.Reader by wrapping a JS ReadableStreamDefaultReader.
// It bridges the async JS ReadableStream API to Go's synchronous io.Reader interface
// using a channel-based pattern.
type wasmStreamReader struct {
	reader js.Value // JS ReadableStreamDefaultReader
	buf    []byte
	done   bool
}

func (r *wasmStreamReader) Read(p []byte) (int, error) {
	// Return buffered data first
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}

	if r.done {
		return 0, io.EOF
	}

	// Use a loop instead of recursion to handle empty chunks without stack growth
	for {
		type chunkResult struct {
			data []byte
			done bool
			err  error
		}
		ch := make(chan chunkResult, 1)

		// Call reader.read() which returns a Promise
		readPromise := r.reader.Call("read")

		var thenCB js.Func
		thenCB = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			thenCB.Release()
			result := args[0]
			if result.Get("done").Bool() {
				ch <- chunkResult{done: true}
				return nil
			}
			value := result.Get("value")
			length := value.Get("length").Int()
			if length <= 0 {
				// Empty chunk, try reading again
				ch <- chunkResult{data: nil}
				return nil
			}
			data := make([]byte, length)
			js.CopyBytesToGo(data, value)
			ch <- chunkResult{data: data}
			return nil
		})

		var catchCB js.Func
		catchCB = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			catchCB.Release()
			msg := "stream read failed"
			if msg2 := extractJSErrorMessage(args[0]); msg2 != "" {
				msg = msg2
			}
			ch <- chunkResult{err: fmt.Errorf("%s: %s", "stream read failed", msg)}
			return nil
		})

		readPromise.Call("then", thenCB)
		readPromise.Call("catch", catchCB)

		res := <-ch
		if res.err != nil {
			return 0, res.err
		}
		if res.done {
			r.done = true
			return 0, io.EOF
		}
		if len(res.data) == 0 {
			continue // Empty chunk, try again (loop instead of recursion)
		}

		if len(res.data) <= len(p) {
			n := copy(p, res.data)
			return n, nil
		}

		// Chunk is larger than p — copy what fits, buffer the rest
		n := copy(p, res.data)
		r.buf = append(r.buf, res.data[n:]...)
		return n, nil
	}
}

// bytesReader is a simple io.Reader wrapper around a byte slice for empty body responses.
type bytesReader struct {
	data []byte
	off  int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

// wasmRoundTripper implements http.RoundTripper using the browser's fetch API.
// This enables Go HTTP clients to make network requests in WASM by bridging
// to JavaScript's fetch() and ReadableStream APIs.
type wasmRoundTripper struct{}

func (t *wasmRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	opts := js.Global().Get("Object").New()
	opts.Set("method", req.Method)

	// Build headers
	headers := js.Global().Get("Headers").New()
	for key, values := range req.Header {
		for _, value := range values {
			headers.Call("append", key, value)
		}
	}
	opts.Set("headers", headers)

	// Build body if present
	if req.Body != nil && req.ContentLength != 0 {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		req.Body.Close()

		if len(bodyBytes) > 0 {
			uint8Array := js.Global().Get("Uint8Array").New(len(bodyBytes))
			js.CopyBytesToJS(uint8Array, bodyBytes)
			opts.Set("body", uint8Array)
			// Explicitly set Content-Length header
			headers.Call("append", "Content-Length", strconv.Itoa(len(bodyBytes)))
		}
	}

	// Build URL (include scheme, host, path, query)
	url := req.URL.String()

	// Call fetch()
	fetchPromise := js.Global().Call("fetch", url, opts)

	type fetchResult struct {
		resp *http.Response
		err  error
	}
	ch := make(chan fetchResult, 1)

	var thenCB js.Func
	thenCB = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		thenCB.Release()
		jsResp := args[0]
		status := jsResp.Get("status").Int()
		statusText := jsResp.Get("statusText").String()

		goResp := &http.Response{
			Status:     fmt.Sprintf("%d %s", status, statusText),
			StatusCode: status,
			Header:     make(http.Header),
			Request:    req,
		}

		// Copy headers from JS response
		jsHeaders := jsResp.Get("headers")
		forEachCB := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			// forEach callback receives (value, key, parent)
			goResp.Header.Add(args[1].String(), args[0].String())
			return nil
		})
		jsHeaders.Call("forEach", forEachCB)
		forEachCB.Release()

		// Get reader for streaming body
		body := jsResp.Get("body")
		if body.Truthy() {
			reader := body.Call("getReader")
			goResp.Body = io.NopCloser(&wasmStreamReader{reader: reader})
			goResp.ContentLength = -1
		} else {
			// No body (e.g., HEAD response) — use empty reader
			goResp.Body = io.NopCloser(&bytesReader{data: []byte{}})
			goResp.ContentLength = 0
		}

		ch <- fetchResult{resp: goResp}
		return nil
	})

	var catchCB js.Func
	catchCB = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		catchCB.Release()
		msg := "fetch failed"
		if msg2 := extractJSErrorMessage(args[0]); msg2 != "" {
			msg = msg2
		}
		err := fmt.Errorf("fetch failed: %s", msg)

		// Wrap CORS-detectable errors with user-friendly guidance.
		if llmproxy.IsCORSError(err) {
			err = fmt.Errorf("%s", llmproxy.CORSErrorMessage(err, req.URL.Hostname()))
		}
		ch <- fetchResult{err: err}
		return nil
	})

	fetchPromise.Call("then", thenCB)
	fetchPromise.Call("catch", catchCB)

	res := <-ch
	return res.resp, res.err
}

// extractJSErrorMessage pulls a human-readable message from a JS error value,
// trying .message first, then string coercion, then .name fallback.
func extractJSErrorMessage(errVal js.Value) string {
	if errVal.IsUndefined() || errVal.IsNull() {
		return ""
	}
	if m := errVal.Get("message"); m.Truthy() {
		return m.String()
	}
	if errVal.Type() == js.TypeString {
		return errVal.String()
	}
	if m := errVal.Get("name"); m.Truthy() {
		return m.String() + " error"
	}
	return ""
}

// NewWasmStreamingHTTPClient creates an *http.Client that uses the browser's
// fetch API with ReadableStream support for streaming responses. This is the
// correct HTTP client to use in WASM environments for LLM SSE streaming.
func NewWasmStreamingHTTPClient() *http.Client {
	return &http.Client{
		Transport: &wasmRoundTripper{},
	}
}
