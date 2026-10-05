package tools

// starter_manifest_guard.go — the SP-149 §149b write rail, applied from the
// live write/edit tool handlers (which cannot import pkg/agent). The actual
// guard (verification-enabled check + path resolution against the starter
// manifest) lives on the *Agent in pkg/agent and is wired in here through
// the per-agent ToolFuncSet (GuardStarterManifestWrite). This helper is the
// bridge the handlers call.

// guardStarterManifestWrite applies the starter-manifest write guard from a
// tool's ToolEnv: it invokes the per-agent guard (wired by pkg/agent via
// GuardStarterManifestWrite) when one is present, and returns the refusal
// error so the caller can stop the write before any routing, read, or
// write. It is a no-op (nil error) when no guard is wired (standalone runs)
// or when the guard itself declines to refuse (verification disabled, or the
// path does not resolve to the starter manifest). The turn-start snapshot is
// the enforcement; this is the polite rail that refuses the write.
func guardStarterManifestWrite(env ToolEnv, path string) error {
	guard := env.ResolveToolFuncs().GuardStarterManifestWrite
	if guard == nil {
		return nil
	}
	return guard(path)
}
