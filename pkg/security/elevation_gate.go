package security

import "strings"

// SecretAction represents the user's decision for a detected secret.
type SecretAction int

const (
	SecretRedact SecretAction = iota // Replace secret with [REDACTED] and continue
	SecretAllow                      // Pass through as-is
	SecretBlock                      // Stop the operation entirely
)

// SecretPrompter is implemented by the agent layer to ask the user what to do
// when secrets are detected. Defined here (in security) to avoid circular imports
// with the agent package.
type SecretPrompter interface {
	// PromptSecretAction presents detected secrets to the user and returns
	// their decision. Returns error if prompting fails (e.g., non-interactive).
	PromptSecretAction(secrets []DetectedSecret, source string) (SecretAction, error)
}

// ElevationGate manages user elevation decisions for secret detection.
// It maintains a session-scoped vault so the user is only prompted once per
// unique secret pattern.
// SAFETY: A single Gate instance is used from one agent goroutine (sequential tool
// execution in pkg/agent/tool_executor_sequential.go), so no mutex is required.
// If parallel tool execution is ever introduced, this type must be protected with
// a sync.Mutex or sync.RWMutex.
type ElevationGate struct {
	vault    map[string]SecretAction // vaultKey -> resolved action (session-scoped)
	prompter SecretPrompter          // may be nil for subagent/non-interactive mode
	catchAll *SecretAction           // optional override for all uncached secrets
}

// NewElevationGate creates a gate that consults prompter for new detections.
// Pass nil to default to SecretRedact for uncached secrets (subagent usage).
func NewElevationGate(prompter SecretPrompter) *ElevationGate {
	return &ElevationGate{
		vault:    make(map[string]SecretAction),
		prompter: prompter,
	}
}

// SetDefault pre-seeds the vault for known patterns. Pass envVarName="" for a
// global catch-all, or e.g. "CommitSecret" to match vault keys of that type.
func (g *ElevationGate) SetDefault(action SecretAction, envVarName string) {
	if envVarName == "" {
		g.catchAll = &action
		return
	}
	// Store a wildcard-style key matched via prefix in lookup.
	g.vault[envVarName+":*"] = action
}

// vaultKey derives a stable key for a detected secret to avoid re-prompting
// equivalent detections within a session.
func vaultKey(s DetectedSecret) string {
	n := len(s.Snippet)
	if n > 8 {
		n = 8
	}
	return s.Type + ":" + s.Snippet[:n]
}

// lookup checks the exact key, then wildcard prefix entries, then catch-all.
// Returns the resolved action and whether a match was found.
func (g *ElevationGate) lookup(key string) (SecretAction, bool) {
	// Exact match.
	if action, ok := g.vault[key]; ok {
		return action, true
	}
	// Wildcard prefix match: entries like "Type:*" match any key starting with "Type:".
	if idx := strings.IndexByte(key, ':'); idx > 0 {
		if action, ok := g.vault[key[:idx]+":*"]; ok {
			return action, true
		}
	}
	// Global catch-all (SetDefault with empty envVarName).
	if g.catchAll != nil {
		return *g.catchAll, true
	}
	return 0, false
}

// Evaluate checks detected secrets against the session vault, prompts for
// any new ones, and returns the aggregated action to take.
//
// Returns SecretAllow when no secrets were found; the strictest action
// (any SecretBlock wins, then any SecretRedact, then SecretAllow) otherwise.
func (g *ElevationGate) Evaluate(secrets []DetectedSecret, source string) (SecretAction, error) {
	if len(secrets) == 0 {
		return SecretAllow, nil
	}

	hasRedact := false
	seen := make(map[string]bool, len(secrets))
	var newSecrets []DetectedSecret

	for _, s := range secrets {
		key := vaultKey(s)

		// Deduplicate within this batch.
		if seen[key] {
			continue
		}
		seen[key] = true

		if action, ok := g.lookup(key); ok {
			if action == SecretBlock {
				return SecretBlock, nil
			}
			if action == SecretRedact {
				hasRedact = true
			}
			continue
		}

		// New detection — collect for batch prompting.
		newSecrets = append(newSecrets, s)
	}

	// All secrets were cached.
	if len(newSecrets) == 0 {
		if hasRedact {
			return SecretRedact, nil
		}
		return SecretAllow, nil
	}

	// Determine action for new secrets.
	var action SecretAction
	if g.prompter == nil {
		action = SecretRedact // non-interactive safe default
	} else {
		var err error
		action, err = g.prompter.PromptSecretAction(newSecrets, source)
		if err != nil {
			action = SecretRedact // prompting failed — safe default
		}
	}

	// Cache the decision for each new secret.
	for _, s := range newSecrets {
		g.vault[vaultKey(s)] = action
	}

	if action == SecretBlock {
		return SecretBlock, nil
	}
	if action == SecretRedact {
		hasRedact = true
	}
	if hasRedact {
		return SecretRedact, nil
	}
	return SecretAllow, nil
}
