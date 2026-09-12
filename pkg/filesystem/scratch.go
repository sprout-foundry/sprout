package filesystem

import "os"

// ScratchDir is the agent scratch directory advertised in the system
// prompts for transient files (screenshots, logs, debugging output).
// Hardcoded literal to stay in lockstep with the prompt text and the
// security classifier's path allowlist, which both name /tmp/sprout.
const ScratchDir = "/tmp/sprout"

// EnsureScratchDir creates the agent scratch directory. Best-effort and
// idempotent: sandboxed environments with a read-only or namespace-
// isolated /tmp fail silently, which is acceptable — the system prompts
// tell the model to verify the directory is usable from shell_command
// and fall back to ./.scratch/ inside the workspace when it is not.
func EnsureScratchDir() {
	_ = os.MkdirAll(ScratchDir, 0o700)
}
