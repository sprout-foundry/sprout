//go:build embed_ort

package embedding

// getEmbeddedORT returns the embedded ONNX Runtime library bytes.
//
// When built with the embed_ort tag, users can inject library bytes
// at link time via ldflags:
//
//	go build -tags embed_ort -ldflags "-X embedding.ortLibBytes=base64data"
//
// If no data is injected, returns nil and resolveORTLibrary falls through
// to its other resolution strategies (env var, system paths, etc.).
var ortLibBytes []byte

func getEmbeddedORT() []byte {
	return ortLibBytes
}
