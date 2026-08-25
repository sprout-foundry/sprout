package agent

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// SetTrainingConfig configures opt-in session recording for training data
// collection. When enabled and an endpoint is set, each SaveStateScoped call
// pushes a PII-redacted copy of the session to the endpoint.
//
// This uses a callback function (SetTrainingPushFunc) to invoke the actual
// push implementation from pkg/training, avoiding a circular import between
// pkg/agent and pkg/training.
func (a *Agent) SetTrainingConfig(cfg configuration.TrainingConfig) {
	a.trainingMu.Lock()
	defer a.trainingMu.Unlock()
	a.trainingEnabled = cfg.Enabled
	a.trainingEndpoint = cfg.Endpoint
	a.trainingExclude = cfg.ExcludePaths
}

// SetTrainingPushFunc wires the push implementation. The callback receives
// a ConversationState (already populated), the endpoint URL, and the exclude
// path list. It should be non-blocking or fast — SaveStateScoped calls it in
// a goroutine.
//
// Typically called from cmd/ with training.PushSession as the argument.
func (a *Agent) SetTrainingPushFunc(fn func(state ConversationState, endpoint string, excludePaths []string) error) {
	a.trainingMu.Lock()
	defer a.trainingMu.Unlock()
	a.trainingPushFn = fn
}

// isTrainingEnabled returns whether training data collection is active for
// this agent. Thread-safe.
func (a *Agent) isTrainingEnabled() bool {
	a.trainingMu.RLock()
	defer a.trainingMu.RUnlock()
	return a.trainingEnabled && a.trainingEndpoint != "" && a.trainingPushFn != nil
}

// pushTrainingSession fires the training push callback in a goroutine if
// training is enabled. This is fire-and-forget — errors from the callback
// are logged to stderr but never block or fail the session save.
func (a *Agent) pushTrainingSession(state ConversationState) {
	if !a.isTrainingEnabled() {
		return
	}

	a.trainingMu.RLock()
	fn := a.trainingPushFn
	endpoint := a.trainingEndpoint
	exclude := a.trainingExclude
	a.trainingMu.RUnlock()

	go func() {
		if err := fn(state, endpoint, exclude); err != nil {
			// Log to stderr — this is fire-and-forget and must not panic
			// or cause the session save to fail.
			fmt.Fprintf(os.Stderr, "[TRAINING] Push failed: %v\n", err)
		}
	}()
}
