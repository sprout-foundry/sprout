package security

import "strings"

// SecretAction represents the user's decision for a detected secret.
type SecretAction int

const (
	SecretRedact      SecretAction = iota // Replace secret with [REDACTED] and continue
	SecretAllow                           // Pass through as-is (just this batch)
	SecretBlock                           // Stop the operation entirely
	SecretAllowSource                     // Pass through as-is AND whitelist the source for the rest of the session
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
// It maintains two session-scoped vaults:
//   - vault: per-secret decisions keyed by type + first 8 chars of snippet
//   - sourceAllow: whole-source allowlist (e.g. "read_file: /tmp/example.html")
//
// The user is prompted only once per unique secret pattern OR once per source
// (whichever they choose).
//
// SAFETY: A single Gate instance is used from one agent goroutine (sequential
// tool execution driven by seed core.ToolRegistry via
// pkg/agent/processQueryWithSeed), so no mutex is required. If parallel
// tool execution is ever introduced, this type must be protected with a
// sync.Mutex or sync.RWMutex.
type ElevationGate struct {
	vault       map[string]SecretAction // per-secret vaultKey -> resolved action
	sourceAllow map[string]bool         // sources the user has whitelisted for the session
	prompter    SecretPrompter          // may be nil for subagent/non-interactive mode
	catchAll    *SecretAction           // optional override for all uncached secrets
}

// NewElevationGate creates a gate that consults prompter for new detections.
// Pass nil to default to SecretRedact for uncached secrets (subagent usage).
func NewElevationGate(prompter SecretPrompter) *ElevationGate {
	return &ElevationGate{
		vault:       make(map[string]SecretAction),
		sourceAllow: make(map[string]bool),
		prompter:    prompter,
	}
}

// IsSourceAllowed reports whether the user has whitelisted this source for
// the rest of the session. Exported for use by callers that want to skip
// detection entirely on whitelisted sources.
func (g *ElevationGate) IsSourceAllowed(source string) bool {
	return g.sourceAllow[source]
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
// Returns SecretAllow when no secrets were found OR the source has been
// whitelisted; otherwise the strictest action (any SecretBlock wins, then
// any SecretRedact, then SecretAllow).
func (g *ElevationGate) Evaluate(secrets []DetectedSecret, source string) (SecretAction, error) {
	if len(secrets) == 0 {
		return SecretAllow, nil
	}

	// If the user previously allowed this whole source, short-circuit.
	if g.sourceAllow[source] {
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

	// SecretAllowSource whitelists the source AND allows this batch through.
	// Don't poison the per-secret vault with it — future detections for the
	// same secret type in a different source should still prompt normally.
	if action == SecretAllowSource {
		g.sourceAllow[source] = true
		return SecretAllow, nil
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
