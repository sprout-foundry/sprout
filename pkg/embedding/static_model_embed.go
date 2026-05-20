//go:build !js

package embedding

import _ "embed"

// On native builds, embed the static model blob into the binary so sprout
// ships self-contained. The WASM build leaves staticModelData empty and
// expects the host page to populate it via SetStaticModelData — see
// docs/WASM_API.md.

//go:embed static_model.bin
var embeddedStaticModelData []byte

func init() {
	staticModelData = embeddedStaticModelData
}
