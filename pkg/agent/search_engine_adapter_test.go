package agent

import (
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// Verify at compile time that *searchEngineAdapter implements tools.SearchEngine.
var _ tools.SearchEngine = (*searchEngineAdapter)(nil)

func TestNewSearchEngineAdapter_NilAgent(t *testing.T) {
	result := newSearchEngineAdapter(nil)
	if result != nil {
		t.Errorf("newSearchEngineAdapter(nil) = %v, want nil", result)
	}
}
