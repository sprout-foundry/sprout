//go:build js && wasm

package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"syscall/js"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// ---------------------------------------------------------------------------
// wasmStreamReader tests
// ---------------------------------------------------------------------------

func TestWasmStreamReader_BufferedData(t *testing.T) {
	reader := &wasmStreamReader{
		buf:  []byte("hello world"),
		done: false,
	}

	buf := make([]byte, 5)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes, got %d", n)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(buf[:n]))
	}

	buf2 := make([]byte, 10)
	n2, err := reader.Read(buf2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n2 != 6 {
		t.Fatalf("expected 6 bytes, got %d", n2)
	}
	if string(buf2[:n2]) != " world" {
		t.Fatalf("expected ' world', got %q", string(buf2[:n2]))
	}
}

func TestWasmStreamReader_BufferedDataExhaust(t *testing.T) {
	reader := &wasmStreamReader{
		buf:  []byte("test"),
		done: true,
	}

	buf := make([]byte, 10)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4 bytes, got %d", n)
	}

	n2, err := reader.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 bytes on EOF, got %d", n2)
	}
}

func TestWasmStreamReader_ReadEOF(t *testing.T) {
	reader := &wasmStreamReader{
		buf:  nil,
		done: true,
	}

	buf := make([]byte, 10)
	n, err := reader.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}

func TestWasmStreamReader_LargeChunk(t *testing.T) {
	data := make([]byte, 1024)
	for i := 0; i < 1024; i++ {
		data[i] = byte(i % 256)
	}
	reader := &wasmStreamReader{
		buf:  data,
		done: false,
	}

	// Read with small buffer (64 bytes)
	smallBuf := make([]byte, 64)
	n, err := reader.Read(smallBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 64 {
		t.Fatalf("expected 64 bytes, got %d", n)
	}
	for i := 0; i < 64; i++ {
		if smallBuf[i] != byte(i) {
			t.Fatalf("byte %d: expected %d, got %d", i, i, smallBuf[i])
		}
	}

	// Read remaining data in chunks
	nextBuf := make([]byte, 128)
	totalRead := 64
	for totalRead < 1024 {
		chunkSize := 128
		if 1024-totalRead < 128 {
			chunkSize = 1024 - totalRead
		}
		n, err = reader.Read(nextBuf[:chunkSize])
		if err != nil {
			t.Fatalf("unexpected error on chunk read: %v", err)
		}
		if n != chunkSize {
			t.Fatalf("expected %d bytes, got %d", chunkSize, n)
		}
		totalRead += n
	}
}

func TestWasmStreamReader_EmptyBuffer(t *testing.T) {
	reader := &wasmStreamReader{
		buf:  nil,
		done: true,
	}

	buf := make([]byte, 10)
	n, err := reader.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}

func TestWasmStreamReader_JSReadableStream(t *testing.T) {
	// Create a JS ReadableStream with known data
	startFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		controller := args[0]
		uint8Array := js.Global().Get("Uint8Array").New(5)
		js.CopyBytesToJS(uint8Array, []byte{1, 2, 3, 4, 5})
		controller.Call("enqueue", uint8Array)
		controller.Call("close")
		return nil
	})

	stream := js.Global().Call("ReadableStream", map[string]interface{}{"start": startFunc})
	readerObj := stream.Call("getReader")

	wasmReader := &wasmStreamReader{
		reader: readerObj,
		buf:    nil,
		done:   false,
	}

	buf := make([]byte, 10)
	n, err := wasmReader.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes, got %d", n)
	}
	expected := []byte{1, 2, 3, 4, 5}
	for i := 0; i < 5; i++ {
		if buf[i] != expected[i] {
			t.Fatalf("byte %d: expected %d, got %d", i, expected[i], buf[i])
		}
	}

	n2, err := wasmReader.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n2 != 0 {
		t.Fatalf("expected 0 bytes on EOF, got %d", n2)
	}
}

func TestWasmStreamReader_JSReadableStream_EOF(t *testing.T) {
	startFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		controller := args[0]
		controller.Call("close")
		return nil
	})

	stream := js.Global().Call("ReadableStream", map[string]interface{}{"start": startFunc})
	readerObj := stream.Call("getReader")

	wasmReader := &wasmStreamReader{
		reader: readerObj,
		buf:    nil,
		done:   false,
	}

	buf := make([]byte, 10)
	_, err := wasmReader.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestWasmStreamReader_JSReadableStream_LargeChunk(t *testing.T) {
	data := make([]byte, 1024)
	for i := 0; i < 1024; i++ {
		data[i] = byte(i % 256)
	}

	startFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		controller := args[0]
		uint8Array := js.Global().Get("Uint8Array").New(1024)
		js.CopyBytesToJS(uint8Array, data)
		controller.Call("enqueue", uint8Array)
		controller.Call("close")
		return nil
	})

	stream := js.Global().Call("ReadableStream", map[string]interface{}{"start": startFunc})
	readerObj := stream.Call("getReader")

	wasmReader := &wasmStreamReader{
		reader: readerObj,
		buf:    nil,
		done:   false,
	}

	// Read with small buffer (64 bytes) — tests backpressure buffering
	smallBuf := make([]byte, 64)
	n, err := wasmReader.Read(smallBuf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 64 {
		t.Fatalf("expected 64 bytes, got %d", n)
	}
	for i := 0; i < 64; i++ {
		if smallBuf[i] != byte(i) {
			t.Fatalf("byte %d: expected %d, got %d", i, i, smallBuf[i])
		}
	}

	// Read remaining in 128-byte chunks
	nextBuf := make([]byte, 128)
	totalRead := 64
	for totalRead < 1024 {
		chunkSize := 128
		if 1024-totalRead < 128 {
			chunkSize = 1024 - totalRead
		}
		n, err = wasmReader.Read(nextBuf[:chunkSize])
		if err != nil {
			t.Fatalf("unexpected error on chunk read: %v", err)
		}
		if n != chunkSize {
			t.Fatalf("expected %d bytes, got %d", chunkSize, n)
		}
		totalRead += n
	}

	_, err = wasmReader.Read(nextBuf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestWasmStreamReader_JSReadableStream_MultipleChunks(t *testing.T) {
	startFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		controller := args[0]
		hello := js.Global().Get("Uint8Array").New(6)
		js.CopyBytesToJS(hello, []byte("hello "))
		controller.Call("enqueue", hello)
		world := js.Global().Get("Uint8Array").New(5)
		js.CopyBytesToJS(world, []byte("world"))
		controller.Call("enqueue", world)
		controller.Call("close")
		return nil
	})

	stream := js.Global().Call("ReadableStream", map[string]interface{}{"start": startFunc})
	readerObj := stream.Call("getReader")

	wasmReader := &wasmStreamReader{
		reader: readerObj,
		buf:    nil,
		done:   false,
	}

	var result []byte
	buf := make([]byte, 100)
	for {
		n, err := wasmReader.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		result = append(result, buf[:n]...)
	}

	if string(result) != "hello world" {
		t.Fatalf("expected 'hello world', got %q", string(result))
	}
}

func TestWasmStreamReader_JSReadableStream_EmptyChunkThenData(t *testing.T) {
	startFunc := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		controller := args[0]
		controller.Call("enqueue", js.Global().Get("Uint8Array").New(0))
		dataArr := js.Global().Get("Uint8Array").New(4)
		js.CopyBytesToJS(dataArr, []byte("data"))
		controller.Call("enqueue", dataArr)
		controller.Call("close")
		return nil
	})

	stream := js.Global().Call("ReadableStream", map[string]interface{}{"start": startFunc})
	readerObj := stream.Call("getReader")

	wasmReader := &wasmStreamReader{
		reader: readerObj,
		buf:    nil,
		done:   false,
	}

	buf := make([]byte, 10)
	n, err := wasmReader.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4 bytes, got %d", n)
	}
	if string(buf[:n]) != "data" {
		t.Fatalf("expected 'data', got %q", string(buf[:n]))
	}
}

// ---------------------------------------------------------------------------
// bytesReader tests
// ---------------------------------------------------------------------------

func TestBytesReader(t *testing.T) {
	reader := &bytesReader{data: []byte("hello"), off: 0}

	buf := make([]byte, 3)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 bytes, got %d", n)
	}
	if string(buf[:n]) != "hel" {
		t.Fatalf("expected 'hel', got %q", string(buf[:n]))
	}

	buf2 := make([]byte, 10)
	n2, err := reader.Read(buf2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n2 != 2 {
		t.Fatalf("expected 2 bytes, got %d", n2)
	}
	if string(buf2[:n2]) != "lo" {
		t.Fatalf("expected 'lo', got %q", string(buf2[:n2]))
	}

	n3, err := reader.Read(buf2)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n3 != 0 {
		t.Fatalf("expected 0 bytes on EOF, got %d", n3)
	}
}

func TestBytesReader_EOF(t *testing.T) {
	reader := &bytesReader{data: []byte{}, off: 0}

	buf := make([]byte, 10)
	n, err := reader.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// NewWasmStreamingHTTPClient tests
// ---------------------------------------------------------------------------

func TestNewWasmStreamingHTTPClient(t *testing.T) {
	client := NewWasmStreamingHTTPClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Transport == nil {
		t.Fatal("expected non-nil Transport")
	}
	if _, ok := client.Transport.(*wasmRoundTripper); !ok {
		t.Fatalf("expected Transport to be *wasmRoundTripper, got %T", client.Transport)
	}
}

// ---------------------------------------------------------------------------
// wasmRoundTripper structural tests
// (full RoundTrip tests require an actual HTTP server)
// ---------------------------------------------------------------------------

func TestWasmRoundTripper_Type(t *testing.T) {
	tripper := &wasmRoundTripper{}
	if tripper == nil {
		t.Fatal("expected non-nil roundTripper")
	}

	// Verify it implements http.RoundTripper at the type level
	var _ http.RoundTripper = tripper
}

func TestWasmRoundTripper_RequestHeaders(t *testing.T) {
	req := newRequest(t, "POST", "https://example.com/test", map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer test-token",
		"X-Custom":      "custom-value",
	})

	// Verify request headers are set
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", req.Header.Get("Content-Type"))
	}
	if req.Header.Get("Authorization") != "Bearer test-token" {
		t.Errorf("expected Authorization 'Bearer test-token', got %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Custom") != "custom-value" {
		t.Errorf("expected X-Custom 'custom-value', got %q", req.Header.Get("X-Custom"))
	}
}

func TestWasmRoundTripper_RequestBody(t *testing.T) {
	bodyData := []byte(`{"prompt":"test"}`)
	req := newRequest(t, "POST", "https://example.com/test", map[string]string{
		"Content-Type": "application/json",
	})
	req.Body = io.NopCloser(strings.NewReader(string(bodyData)))
	req.ContentLength = int64(len(bodyData))

	if req.Body == nil {
		t.Fatal("expected non-nil request body")
	}
	if req.ContentLength != int64(len(bodyData)) {
		t.Fatalf("expected content length %d, got %d", len(bodyData), req.ContentLength)
	}
}

func TestWasmRoundTripper_RequestMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	for _, method := range methods {
		req := newRequest(t, method, "https://example.com/test", nil)
		if req.Method != method {
			t.Fatalf("expected method %s, got %s", method, req.Method)
		}
	}
}

func TestWasmRoundTripper_RequestURLs(t *testing.T) {
	urls := []string{
		"https://example.com/test",
		"https://example.com/test?query=value",
		"https://example.com:8080/test",
	}
	for _, url := range urls {
		req := newRequest(t, "GET", url, nil)
		if req.URL.String() != url {
			t.Fatalf("expected URL %s, got %s", url, req.URL.String())
		}
	}
}

// ---------------------------------------------------------------------------
// injectWasmStreamingClient tests
// ---------------------------------------------------------------------------

func TestInjectWasmStreamingClient_Nil(t *testing.T) {
	// Should not panic on nil
	injectWasmStreamingClient(nil)
}

func TestInjectWasmStreamingClient_NonGenericProvider(t *testing.T) {
	// Should not panic on non-GenericProvider (type assertion fails gracefully)
	mockClient := &mockClient{}
	injectWasmStreamingClient(mockClient)
}

func TestInjectWasmStreamingClient_GenericProvider(t *testing.T) {
	config := newTestProviderConfig("test-provider", "test-model", "https://example.com")
	provider, err := providers.NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	oldHTTPClient := provider.GetHTTPClient()
	oldStreamingClient := provider.GetStreamingClient()

	// Inject the WASM streaming client
	injectWasmStreamingClient(provider)

	newHTTPClient := provider.GetHTTPClient()
	newStreamingClient := provider.GetStreamingClient()

	if newHTTPClient == oldHTTPClient {
		t.Fatal("expected HTTP client to be replaced")
	}
	if newStreamingClient == oldStreamingClient {
		t.Fatal("expected streaming client to be replaced")
	}

	// Verify the new clients use wasmRoundTripper
	if newHTTPClient.Transport == nil {
		t.Fatal("expected non-nil Transport on new HTTP client")
	}
	if _, ok := newHTTPClient.Transport.(*wasmRoundTripper); !ok {
		t.Fatalf("expected Transport to be *wasmRoundTripper, got %T", newHTTPClient.Transport)
	}
	if newStreamingClient.Transport == nil {
		t.Fatal("expected non-nil Transport on new streaming client")
	}
	if _, ok := newStreamingClient.Transport.(*wasmRoundTripper); !ok {
		t.Fatalf("expected Transport to be *wasmRoundTripper, got %T", newStreamingClient.Transport)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newRequest(t *testing.T, method, url string, headers map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func newTestProviderConfig(name, model, endpoint string) *providers.ProviderConfig {
	return &providers.ProviderConfig{
		Name:     name,
		Endpoint: endpoint,
		Auth: providers.AuthConfig{
			Type: "none",
		},
		Defaults: providers.RequestDefaults{
			Model: model,
		},
		Models: providers.ModelConfig{
			DefaultContextLimit: 4096,
		},
	}
}

// mockClient implements api.ClientInterface for testing injectWasmStreamingClient
// It is NOT a *GenericProvider, so injectWasmStreamingClient should skip it.
type mockClient struct{}

func (m *mockClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, nil
}

func (m *mockClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return nil, nil
}

func (m *mockClient) CheckConnection() error                        { return nil }
func (m *mockClient) SetDebug(debug bool)                           {}
func (m *mockClient) SetModel(model string) error                   { return nil }
func (m *mockClient) GetModel() string                              { return "mock" }
func (m *mockClient) GetProvider() string                           { return "mock" }
func (m *mockClient) GetModelContextLimit() (int, error)            { return 0, nil }
func (m *mockClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) { return nil, nil }
func (m *mockClient) SupportsVision() bool                          { return false }
func (m *mockClient) GetVisionModel() string                        { return "" }
func (m *mockClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, nil
}
func (m *mockClient) GetLastTPS() float64                           { return 0 }
func (m *mockClient) GetAverageTPS() float64                        { return 0 }
func (m *mockClient) GetTPSStats() map[string]float64               { return nil }
func (m *mockClient) ResetTPSStats()                                {}
