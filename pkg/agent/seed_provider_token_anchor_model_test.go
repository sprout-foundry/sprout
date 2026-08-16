package agent

import (
	"errors"
	"testing"

	core "github.com/sprout-foundry/seed/core"
)

// errContextLimitClient wraps MockClient and injects a GetModelContextLimit
// failure so Info()'s error-fallback path can be exercised.
type errContextLimitClient struct {
	*MockClient
}

func (e *errContextLimitClient) GetModelContextLimit() (int, error) {
	return 0, errors.New("lookup failed")
}

// TestTokenAnchorModelKeyInvalidatesOnModelSwitch verifies that a token
// anchor recorded under one model is not reused for a different model — the
// old model's tokenizer measurement is meaningless for the new model's
// context-window math.
func TestTokenAnchorModelKeyInvalidatesOnModelSwitch(t *testing.T) {
	var anchor tokenAnchor
	messages := []core.Message{{Role: "user", Content: "hi"}}
	anchor.update("model-a", messages, 2, 5000)

	if _, _, ok := anchor.estimate("model-a", messages, 2); !ok {
		t.Error("expected anchor to apply for the same model that measured it")
	}
	if _, _, ok := anchor.estimate("model-b", messages, 2); ok {
		t.Error("expected anchor to be invalidated when the model changes (tokenizer differs)")
	}
}
