package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/spec"
	"github.com/alantheprice/ledit/pkg/utils"
)

// runSelfReviewGate runs self-review validation after conversation completion.
func (ch *ConversationHandler) runSelfReviewGate() error {
	if os.Getenv("LEDIT_SKIP_SELF_REVIEW_GATE") == "1" {
		ch.agent.PrintLineAsync("[WARN] Self-review gate skipped (LEDIT_SKIP_SELF_REVIEW_GATE=1)")
		return nil
	}
	activePersona := ch.agent.GetActivePersona()
	if !isSelfReviewGatePersonaEnabled(activePersona) {
		if strings.TrimSpace(activePersona) == "" {
			ch.agent.PrintLineAsync("[info] Self-review gate skipped (persona=<none>)")
		} else {
			ch.agent.PrintLineAsync(fmt.Sprintf("[info] Self-review gate skipped (persona=%s)", activePersona))
		}
		return nil
	}

	revisionID := strings.TrimSpace(ch.agent.GetRevisionID())
	if revisionID == "" {
		return fmt.Errorf("self-review gate blocked completion: no revision ID available for changed task")
	}

	var cfgErr error
	cfg := ch.agent.GetConfigManager().GetConfig()
	if cfg == nil {
		cfg, cfgErr = configuration.Load()
		if cfgErr != nil {
			return fmt.Errorf("self-review gate blocked completion: failed to load config: %w", cfgErr)
		}
	}
	mode := cfg.GetSelfReviewGateMode()
	if mode == configuration.SelfReviewGateModeOff {
		ch.agent.PrintLineAsync("[info] Self-review gate skipped (mode=off)")
		return nil
	}
	if mode == configuration.SelfReviewGateModeCode && !hasCodeLikeTrackedFiles(ch.agent.GetTrackedFiles()) {
		ch.agent.PrintLineAsync("[info] Self-review gate skipped (mode=code, no code files changed)")
		return nil
	}

	logger := utils.GetLogger(true)
	result, err := spec.ReviewTrackedChanges(revisionID, cfg, logger)
	if err != nil {
		return fmt.Errorf("self-review gate blocked completion: %w", err)
	}
	if result.ScopeResult != nil && !result.ScopeResult.InScope {
		summary := strings.TrimSpace(result.ScopeResult.Summary)
		if summary == "" {
			summary = "scope violations detected"
		}
		return fmt.Errorf("self-review gate blocked completion: %s", summary)
	}

	ch.agent.PrintLineAsync(fmt.Sprintf("[OK] Self-review gate passed: revision %s is within scope", revisionID))
	return nil
}

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
	case "orchestrator", "coder":
		return true
	default:
		return false
	}
}
