//go:build !js

package webcontent

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBrowseStepHandlers_MatchCanonicalActions is the drift guard for the
// step vocabulary. The canonical verb list (browseStepActions, exposed as
// BrowseStepActions) is what the tool schema advertises to the model, and
// browseStepHandlers is what the executor can actually dispatch. If they
// diverge, the model is either offered a verb that fails at runtime or a
// working verb is hidden from it.
//
// When this test fails, add the verb to BOTH the canonical list in
// browse_steps_actions.go and the handler map in browser_rod_interaction.go
// (or remove it from both).
func TestBrowseStepHandlers_MatchCanonicalActions(t *testing.T) {
	t.Parallel()

	canonical := BrowseStepActions()
	require.NotEmpty(t, canonical)

	// Direction 1: every advertised verb has a handler.
	for _, action := range canonical {
		_, ok := browseStepHandlers[action]
		assert.True(t, ok,
			"canonical action %q has no handler in browseStepHandlers — add it to browser_rod_interaction.go (or remove it from browseStepActions in browse_steps_actions.go)", action)
	}

	// Direction 2: every handler is an advertised verb.
	handled := make([]string, 0, len(browseStepHandlers))
	for action := range browseStepHandlers {
		handled = append(handled, action)
	}
	sort.Strings(handled)

	for _, action := range handled {
		assert.Contains(t, canonical, action,
			"handler %q is not in BrowseStepActions() — add it to browseStepActions in browse_steps_actions.go (or remove it from browseStepHandlers in browser_rod_interaction.go)", action)
	}

	assert.Equal(t, canonical, handled, "the handler set and the canonical action set must be identical")
}

// TestBrowseStepHandlers_NoNilHandlers catches a map entry added without a
// function, which would panic at dispatch time instead of failing here.
func TestBrowseStepHandlers_NoNilHandlers(t *testing.T) {
	t.Parallel()

	for action, handler := range browseStepHandlers {
		assert.NotNil(t, handler, "handler for %q must not be nil", action)
	}
}
