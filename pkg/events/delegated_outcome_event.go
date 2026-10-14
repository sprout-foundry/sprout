package events

// DelegatedTaskOutcomeEvent creates the delegated_task_outcome event payload:
// the model that produced a delegated task's final result, its provider and
// role, the task type, the verification outcome, and the tokens and cost. A
// host aggregates these to see the share of work a cheaper worker model
// handled and the credits saved. No prompt text is carried.
func DelegatedTaskOutcomeEvent(model, provider, role, taskType, outcome string, tokens, promptTokens, completionTokens int, cost float64) map[string]interface{} {
	return map[string]interface{}{
		"model":             model,
		"provider":          provider,
		"role":              role,
		"task_type":         taskType,
		"outcome":           outcome,
		"tokens":            tokens,
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"cost":              cost,
	}
}
