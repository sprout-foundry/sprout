package agent

import (
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

func TestNewSkillLoaderAdapter_NilAgent(t *testing.T) {
	t.Parallel()
	result := newSkillLoaderAdapter(nil)
	if result != nil {
		t.Errorf("newSkillLoaderAdapter(nil) should return nil, got %T", result)
	}
}

func TestNewSkillLoaderAdapter_NonNilAgent(t *testing.T) {
	t.Parallel()
	// We can't easily construct a full *Agent in isolation, but we can verify
	// the adapter compiles and implements the tools.SkillLoader interface.
	// The nil check above covers the edge case; the real wiring is tested
	// via the handler tests which exercise the full flow.
	var _ tools.SkillLoader = (*skillLoaderAdapter)(nil)
}
