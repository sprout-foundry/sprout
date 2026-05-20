//go:build js

package embedding

import (
	"os"
	"testing"
)

// TestMain on WASM loads the static model from disk before any package
// test runs. The native build embeds the model via `//go:embed` (see
// static_model_embed.go), but the WASM build leaves it empty so the host
// page can lazy-load it from a separate URL. Tests need the same
// bootstrap to exercise StaticProvider paths.
//
// The wasm_exec_node helper routes os.ReadFile through Node's fs module,
// so reading the file from the package directory works when tests are
// invoked via `GOOS=js GOARCH=wasm go test -exec go_js_wasm_exec`.
func TestMain(m *testing.M) {
	data, err := os.ReadFile("static_model.bin")
	if err != nil {
		// Fail loudly — every StaticProvider-touching test downstream
		// would emit the confusing "static model data is empty" error
		// otherwise.
		panic("wasm test bootstrap: failed to load static_model.bin from the package directory: " + err.Error())
	}
	SetStaticModelData(data)
	os.Exit(m.Run())
}
