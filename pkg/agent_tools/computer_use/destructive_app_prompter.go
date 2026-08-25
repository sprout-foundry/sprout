package computer_use

import (
	"context"
	"errors"
	"sync"
)

// DestructiveAppDecision is the user's choice when a destructive-app
// action is intercepted by the gate.
type DestructiveAppDecision int

const (
	// DestructiveAppDeny blocks the action entirely.
	DestructiveAppDeny DestructiveAppDecision = iota
	// DestructiveAppAllowOnce allows this single invocation.
	DestructiveAppAllowOnce
	// DestructiveAppAllowAlways allows this invocation and persists the
	// app to the override allowlist so future sessions skip the prompt.
	DestructiveAppAllowAlways
)

// String returns a stable lowercase identifier for the decision.
func (d DestructiveAppDecision) String() string {
	switch d {
	case DestructiveAppDeny:
		return "deny"
	case DestructiveAppAllowOnce:
		return "allow_once"
	case DestructiveAppAllowAlways:
		return "allow_always"
	default:
		return "deny"
	}
}

// ErrDestructiveAppBlocked is returned when the user denies a destructive-app
// action via the approval cascade.
var ErrDestructiveAppBlocked = errors.New("destructive app action blocked by user")

// DestructiveAppPrompter is the interface the agent side implements to
// prompt the user when a denylisted app is detected. Tests can mock this.
type DestructiveAppPrompter interface {
	PromptDestructiveApp(ctx context.Context, action string, args map[string]any, cls Classification) DestructiveAppDecision
}

var (
	destructiveAppPrompterMu sync.RWMutex
	destructiveAppPrompter   DestructiveAppPrompter
)

// SetDestructiveAppPrompter installs the prompter used by
// classifyAndPrompt. Pass nil to revert to the safe no-op default.
func SetDestructiveAppPrompter(p DestructiveAppPrompter) {
	destructiveAppPrompterMu.Lock()
	defer destructiveAppPrompterMu.Unlock()
	destructiveAppPrompter = p
}

// GetDestructiveAppPrompter returns the current prompter, or a no-op
// default that always returns DestructiveAppDeny when none is set.
func GetDestructiveAppPrompter() DestructiveAppPrompter {
	destructiveAppPrompterMu.RLock()
	defer destructiveAppPrompterMu.RUnlock()
	if destructiveAppPrompter == nil {
		return defaultDestructiveAppPrompter{}
	}
	return destructiveAppPrompter
}

// defaultDestructiveAppPrompter is the safe fallback: always deny.
type defaultDestructiveAppPrompter struct{}

func (defaultDestructiveAppPrompter) PromptDestructiveApp(_ context.Context, _ string, _ map[string]any, _ Classification) DestructiveAppDecision {
	return DestructiveAppDeny
}

// classifyAndPrompt is a convenience helper that classifies the foreground
// app and, if blocked, prompts the user. Returns nil when the action may
// proceed, or an error when it must be blocked.
func classifyAndPrompt(ctx context.Context, action string, args map[string]any, fg ForegroundInfo) error {
	// Skip if no foreground info is available.
	if fg.AppName == "" && fg.BundleID == "" && fg.WindowClass == "" {
		return nil
	}
	cls := DefaultLoader().IsDestructiveApp(fg)
	if !cls.IsBlocked() {
		return nil
	}
	decision := GetDestructiveAppPrompter().PromptDestructiveApp(ctx, action, args, cls)
	if decision == DestructiveAppDeny {
		return ErrDestructiveAppBlocked
	}
	return nil
}
