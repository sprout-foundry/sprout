//go:build !js

package cmd

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// maybeRecommendEmbeddingIndex prints a single, one-time-per-workspace nudge to
// turn on the semantic index when it is currently disabled.
//
// Embeddings (semantic recall + duplicate detection) are opt-in because the
// index loads a ~380MB local ONNX model — most of an agent's resident memory —
// so they stay off until the user asks for them. The downside of opt-in is
// discoverability: a user who'd benefit never learns the feature exists. This
// recommends it once per workspace (persisted alongside the first-run hint in
// ~/.sprout/state.json), then never nags again.
//
// Silent on: index already enabled, errors loading/persisting state, or a
// workspace already nudged. Prints to stderr to keep stdout pipe-clean.
func maybeRecommendEmbeddingIndex(chatAgent *agent.Agent) {
	if chatAgent == nil || chatAgent.IsEmbeddingIndexEnabled() {
		return
	}

	firstRunStateMu.Lock()
	defer firstRunStateMu.Unlock()

	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	statePath, err := firstRunStatePath()
	if err != nil {
		return
	}

	state, _ := loadFirstRunState(statePath) // best-effort; nil means show
	if state == nil {
		state = &sproutState{}
	}
	for _, ws := range state.SeenIndexRecommendation {
		if ws == cwd {
			return
		}
	}

	fmt.Fprintln(os.Stderr, console.GlyphInfo.Prefix()+
		"Tip: enable semantic recall & duplicate detection with /index on "+
		"(loads a ~380MB local model once; off by default to stay lightweight).")

	state.SeenIndexRecommendation = append(state.SeenIndexRecommendation, cwd)
	_ = saveFirstRunState(statePath, state) // best-effort
}
