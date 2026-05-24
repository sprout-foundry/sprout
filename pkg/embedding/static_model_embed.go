//go:build !js && staticmodel

package embedding

import _ "embed"

// When built with the "staticmodel" tag and the model blob is present, bake it
// into the binary so sprout ships self-contained. Without the tag (the
// default), static_model_nostub.go is compiled instead and staticModelData
// stays empty — semantic search will return an error at runtime.

//go:embed static_model.bin
var embeddedStaticModelData []byte

func init() {
	staticModelData = embeddedStaticModelData
}
