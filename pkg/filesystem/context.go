package filesystem

import (
	"context"
	"strings"
)

type workspaceRootContextKey string

const (
	workspaceRootContextKeyValue  workspaceRootContextKey = "workspace_root"
	securityBypassContextKeyValue workspaceRootContextKey = "security_bypass"
)

// WithWorkspaceRoot stores an explicit workspace root on the context so file and
// process operations do not depend on the process-global cwd.
func WithWorkspaceRoot(ctx context.Context, workspaceRoot string) context.Context {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if ctx == nil {
		ctx = context.Background()
	}
	if workspaceRoot == "" {
		return ctx
	}
	return context.WithValue(ctx, workspaceRootContextKeyValue, workspaceRoot)
}

// WorkspaceRootFromContext returns the explicit workspace root carried on ctx, if any.
func WorkspaceRootFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(workspaceRootContextKeyValue).(string)
	return strings.TrimSpace(value)
}

// WithSecurityBypass marks a context as having explicit user approval for file
// access outside the workspace root.
func WithSecurityBypass(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, securityBypassContextKeyValue, true)
}

// SecurityBypassEnabled reports whether the context carries an explicit
// filesystem security bypass approval.
func SecurityBypassEnabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(securityBypassContextKeyValue).(bool)
	return enabled
}
