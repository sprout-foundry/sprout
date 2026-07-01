// Package agent provides the seed integration layer.
//
// seed_self_review.go — self-review gate and UseSeedLoop for post-query
// validation of tracked changes.

package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"github.com/sprout-foundry/sprout/pkg/spec"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// UseSeedLoop returns true if the agent should use seed's conversation loop
// instead of the native sprout ConversationHandler.
// DEPRECATED: Always returns true now that seed is the only path.
// Kept for backward compatibility with code that checks this value.
func UseSeedLoop() bool {
	return true
}

// ---------------------------------------------------------------------------
// Self-review gate (moved from conversation_handler_review.go)
// ---------------------------------------------------------------------------

// runSelfReviewGate runs self-review validation after conversation completion.
func (a *Agent) runSelfReviewGate() error {
	if configuration.GetEnvSimple("SKIP_SELF_REVIEW_GATE") == "1" {
		a.PrintLineAsync(fmt.Sprintf("%sSelf-review gate skipped (SPROUT_SKIP_SELF_REVIEW_GATE=1)", console.GlyphWarning.Prefix()))
		return nil
	}
	activePersona := a.GetActivePersona()
	if !isSelfReviewGatePersonaEnabled(activePersona) {
		if strings.TrimSpace(activePersona) == "" {
			a.PrintLineAsync(fmt.Sprintf("%sSelf-review gate skipped (persona=<none>)", console.GlyphInfo.Prefix()))
		} else {
			a.PrintLineAsync(fmt.Sprintf("%sSelf-review gate skipped (persona=%s)", console.GlyphInfo.Prefix(), activePersona))
		}
		return nil
	}

	revisionID := strings.TrimSpace(a.GetRevisionID())
	if revisionID == "" {
		return agenterrors.NewPermanentError("self-review gate blocked completion: no revision ID available for changed task", nil)
	}

	var cfgErr error
	cfg := a.GetConfigManager().GetConfig()
	if cfg == nil {
		cfg, cfgErr = configuration.Load()
		if cfgErr != nil {
			return agenterrors.Wrap(cfgErr, "self-review gate blocked completion: failed to load config")
		}
	}
	mode := cfg.GetSelfReviewGateMode()
	if mode == configuration.SelfReviewGateModeOff {
		a.PrintLineAsync(fmt.Sprintf("%sSelf-review gate skipped (mode=off)", console.GlyphInfo.Prefix()))
		return nil
	}
	if mode == configuration.SelfReviewGateModeCode && !hasCodeLikeTrackedFiles(a.GetTrackedFiles()) {
		a.PrintLineAsync(fmt.Sprintf("%sSelf-review gate skipped (mode=code, no code files changed)", console.GlyphInfo.Prefix()))
		return nil
	}

	logger := utils.GetLogger(true)
	result, err := spec.ReviewTrackedChanges(a.interruptCtx, revisionID, cfg, logger)
	if err != nil {
		return agenterrors.Wrap(err, "self-review gate blocked completion")
	}
	if result.ScopeResult != nil && !result.ScopeResult.InScope {
		summary := strings.TrimSpace(result.ScopeResult.Summary)
		if summary == "" {
			summary = "scope violations detected"
		}
		return agenterrors.NewAgent("self-review", "self-review gate blocked completion: "+summary, nil)
	}

	a.PrintLineAsync(fmt.Sprintf("%sSelf-review gate passed: revision %s is within scope", console.GlyphSuccess.Prefix(), revisionID))
	return nil
}

// ---------------------------------------------------------------------------
// Utility functions moved from deleted files
// ---------------------------------------------------------------------------

func hasCodeLikeTrackedFiles(files []string) bool {
	if len(files) == 0 {
		return false
	}

	codeExtensions := map[string]struct{}{
		".go": {}, ".py": {}, ".js": {}, ".ts": {}, ".tsx": {}, ".jsx": {}, ".java": {},
		".rs": {}, ".c": {}, ".cc": {}, ".cpp": {}, ".h": {}, ".hh": {}, ".hpp": {}, ".cs": {},
		".rb": {}, ".php": {}, ".swift": {}, ".kt": {}, ".kts": {}, ".scala": {}, ".sh": {},
		".bash": {}, ".zsh": {}, ".fish": {}, ".sql": {}, ".html": {}, ".css": {}, ".scss": {},
		".vue": {}, ".svelte": {}, ".yaml": {}, ".yml": {}, ".toml": {}, ".ini": {}, ".json": {},
		".xml": {}, ".proto": {}, ".tf": {},
	}
	codeBasenames := map[string]struct{}{
		"dockerfile":       {},
		"makefile":         {},
		"justfile":         {},
		"cmakelists.txt":   {},
		"build.gradle":     {},
		"build.gradle.kts": {},
	}

	for _, f := range files {
		path := strings.TrimSpace(f)
		if path == "" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := codeExtensions[ext]; ok {
			return true
		}
		base := strings.ToLower(filepath.Base(path))
		if _, ok := codeBasenames[base]; ok {
			return true
		}
	}

	return false
}

func isSelfReviewGatePersonaEnabled(persona string) bool {
	switch strings.ToLower(strings.TrimSpace(persona)) {
	case personas.IDOrchestrator, personas.IDCoder:
		return true
	default:
		return false
	}
}
