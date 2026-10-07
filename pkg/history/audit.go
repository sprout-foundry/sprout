// Audit logging for SP-077: tracks every write-back of OriginalCode
// (or NewCode) to the working tree. Each write-back is a potential
// source of silent committed-work reversion, so these audit lines are
// kept concise and greppable; the full call chain is available on demand
// with SPROUT_DEBUG set.
//
// The logs use a distinctive [SP077-AUDIT] prefix for easy grepping.
// They are written to the standard logger so they show up in agent
// debug output without requiring verbose mode.
package history

import (
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"
)

// auditStackEnabled gates the per-write stack dump. Capturing debug.Stack()
// on every write-back is diagnostic-only — a bulk revert/recover over a tree
// emits one per file, which floods the console with hundreds of stack traces
// and buries the operation's own output. Off by default; set SPROUT_DEBUG to
// include the stack when diagnosing which path triggered a write.
var auditStackEnabled atomic.Bool

func init() {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SPROUT_DEBUG"))) {
	case "1", "true", "yes", "on":
		auditStackEnabled.Store(true)
	}
}

// SetAuditStackEnabled toggles the stack dump at runtime (tests, --debug wiring).
func SetAuditStackEnabled(enabled bool) {
	auditStackEnabled.Store(enabled)
}

// AuditRevertWrite logs a write-back of tracked content (OriginalCode
// or NewCode) to the working tree. Called immediately before every
// os.WriteFile / filesystem.SaveFile in the rollback/recovery paths.
//
// `caller` identifies the function performing the write (e.g.
// "handleRevisionRollback", "revertOne"). `path` is the absolute or
// relative filesystem path being written. `contentType` is "OriginalCode"
// or "NewCode" so the log distinguishes reverts from restores.
//
// The full call chain — useful for diagnosing whether a write came from an
// LLM tool call, a CLI command, a test, or an unexpected automatic path — is
// appended only when SPROUT_DEBUG is set.
func AuditRevertWrite(caller, path, contentType string) {
	if auditStackEnabled.Load() {
		log.Printf("[SP077-AUDIT] revert-write caller=%s path=%q content=%s\n--- stack trace ---\n%s--- end stack ---",
			caller, path, contentType, debug.Stack())
		return
	}
	log.Printf("[SP077-AUDIT] revert-write caller=%s path=%q content=%s", caller, path, contentType)
}

// AuditRevertSkip logs when a staleness guard refuses a write-back.
// Useful for correlating how many reverts were blocked vs. how many
// went through, and confirming the guards are firing.
func AuditRevertSkip(caller, path, reason string) {
	log.Printf("[SP077-AUDIT] revert-skip caller=%s path=%q reason=%s",
		caller, path, reason)
}
