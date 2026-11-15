//go:build darwin && arm64 && cgo

package gpulink

import (
	"net/http"
	"path/filepath"

	"github.com/sprout-foundry/sinter/llm/openaisserver"

	"github.com/sprout-foundry/sprout/pkg/localmodel"
)

// SinterAvailable reports whether this build runs sinter models in-process.
const SinterAvailable = true

// SinterHandler loads the sinter model in dir and returns sinter's
// OpenAI-compatible handler for it, the name it serves the model under, and
// the function that unloads it.
func SinterHandler(dir string) (http.Handler, string, func(), error) {
	m, err := localmodel.LoadServingModel(dir)
	if err != nil {
		return nil, "", nil, err
	}
	name := filepath.Base(dir)
	// No max_tokens cap: the platform's agents bound their own replies.
	return openaisserver.New(m, name, 0).Handler(), name, func() { _ = m.Close() }, nil
}
