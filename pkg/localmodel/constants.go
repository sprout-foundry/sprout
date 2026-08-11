package localmodel

import (
	"os"
	"path/filepath"
)

// DefaultPort is the port the local LLM server listens on (for the
// standalone HTTP server mode). The in-process provider doesn't use it.
const DefaultPort = 18081

// DefaultModelsDir is where downloaded models are stored.
var DefaultModelsDir = func() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "dev", "llm-models")
	}
	return filepath.Join(os.TempDir(), "llm-models")
}()
