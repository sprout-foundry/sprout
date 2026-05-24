//go:build !js && !staticmodel

package embedding

// When the "staticmodel" build tag is NOT set (the default), staticModelData
// remains empty. Semantic search features that depend on the static provider
// will return a clear error at runtime. Build with -tags staticmodel and
// ensure static_model.bin is present to embed the model.

func init() {
	// staticModelData stays nil — NewStaticProvider returns "empty" error.
}
