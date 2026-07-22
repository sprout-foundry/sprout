package filesystem

import (
	"context"
	"strings"
)

type workspaceRootContextKey string
type effectiveCwdContextKey string
type sessionAllowedFoldersContextKey string
type auditLoggerContextKey string

const (
	workspaceRootContextKeyValue          workspaceRootContextKey = "workspace_root"
	securityBypassContextKeyValue         workspaceRootContextKey = "security_bypass"
	effectiveCwdContextKeyValue          effectiveCwdContextKey = "effective_cwd"
	sessionAllowedFoldersContextKeyValue sessionAllowedFoldersContextKey = "session_allowed_folders"
	auditLoggerContextKeyValue           auditLoggerContextKey = "audit_logger"
)

// AuditLogger is an interface for security audit logging.
// This allows the filesystem package to accept any audit logger implementation
// (e.g., *tools.AuditLogger from pkg/agent_tools) without importing that package,
// avoiding import cycles with packages that depend on filesystem.
type AuditLogger interface {
	LogEntry(entry any) error
	LogJSON(data []byte) error
}

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

// WithEffectiveCwd stores the agent's effective working directory (shell cwd)
// on the context for filesystem path resolution.
func WithEffectiveCwd(ctx context.Context, effectiveCwd string) context.Context {
	effectiveCwd = strings.TrimSpace(effectiveCwd)
	if ctx == nil {
		ctx = context.Background()
	}
	if effectiveCwd == "" {
		return ctx
	}
	return context.WithValue(ctx, effectiveCwdContextKeyValue, effectiveCwd)
}

// AgentEffectiveCwdFromContext returns the agent's effective working directory
// carried on ctx, if any.
func AgentEffectiveCwdFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(effectiveCwdContextKeyValue).(string)
	return strings.TrimSpace(value)
}

// WithSessionAllowedFolders stores the session-allowlisted folders on the context
// for filesystem path resolution. These are workflow-declared allowed_paths plus
// folders the user approved mid-session.
func WithSessionAllowedFolders(ctx context.Context, folders []string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(folders) == 0 {
		return ctx
	}
	// Make a copy to prevent mutation after storing
	foldersCopy := make([]string, len(folders))
	copy(foldersCopy, folders)
	return context.WithValue(ctx, sessionAllowedFoldersContextKeyValue, foldersCopy)
}

// SessionAllowedFoldersFromContext returns the session-allowlisted folders
// carried on ctx, if any.
func SessionAllowedFoldersFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	value, _ := ctx.Value(sessionAllowedFoldersContextKeyValue).([]string)
	if value == nil {
		return nil
	}
	// Return a copy to prevent mutation
	result := make([]string, len(value))
	copy(result, value)
	return result
}

// WithAgentContext is a convenience helper that stores both the agent's
// effective working directory and session-allowlisted folders on the context.
// This combines WithEffectiveCwd and WithSessionAllowedFolders in one call.
func WithAgentContext(ctx context.Context, effectiveCwd string, sessionFolders []string) context.Context {
	ctx = WithEffectiveCwd(ctx, effectiveCwd)
	ctx = WithSessionAllowedFolders(ctx, sessionFolders)
	return ctx
}

// WithAuditLogger stores an audit logger on the context for filesystem gate
// decision logging. This is used by SP-127 Phase 2.6 to emit audit entries
// for filesystem path resolution decisions.
func WithAuditLogger(ctx context.Context, logger AuditLogger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, auditLoggerContextKeyValue, logger)
}

// AuditLoggerFromContext returns the audit logger carried on ctx, if any.
func AuditLoggerFromContext(ctx context.Context) AuditLogger {
	if ctx == nil {
		return nil
	}
	logger, _ := ctx.Value(auditLoggerContextKeyValue).(AuditLogger)
	return logger
}
