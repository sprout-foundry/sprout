package filesystem

import "context"

// securityContextKey is the type for context keys related to security
type securityContextKey string

const (
	// securityBypassKey is the context key that stores whether to bypass security checks
	securityBypassKey securityContextKey = "security-bypass"
)

// WithSecurityBypass returns a new context that allows security checks to be bypassed.
// This should only be used when the user has explicitly approved the operation.
func WithSecurityBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, securityBypassKey, true)
}

// SecurityBypassEnabled returns true if security bypass is enabled in the context.
// This indicates that the user has explicitly approved bypassing security restrictions.
func SecurityBypassEnabled(ctx context.Context) bool {
	return ctx.Value(securityBypassKey) == true
}
