// Audit logging for SP-077: tracks every write-back of OriginalCode
// (or NewCode) to the working tree. Each write-back is a potential
// source of silent committed-work reversion, so these audit lines
// include a stack trace to definitively identify the caller.
//
// The logs use a distinctive [SP077-AUDIT] prefix for easy grepping.
// They are written to the standard logger so they show up in agent
// debug output without requiring verbose mode.
package history

import (
	"log"
	"runtime/debug"
)

// AuditRevertWrite logs a write-back of tracked content (OriginalCode
// or NewCode) to the working tree. Called immediately before every
// os.WriteFile / filesystem.SaveFile in the rollback/recovery paths.
//
// `caller` identifies the function performing the write (e.g.
// "handleRevisionRollback", "revertOne"). `path` is the absolute or
// relative filesystem path being written. `contentType` is "OriginalCode"
// or "NewCode" so the log distinguishes reverts from restores.
//
// The stack trace captures the full call chain — this is the critical
// piece for diagnosing whether the write was triggered by an LLM tool
// call, a CLI command, a test, or an unexpected automatic path.
func AuditRevertWrite(caller, path, contentType string) {
	log.Printf("[SP077-AUDIT] revert-write caller=%s path=%q content=%s\n--- stack trace ---\n%s--- end stack ---",
		caller, path, contentType, debug.Stack())
}

// AuditRevertSkip logs when a staleness guard refuses a write-back.
// Useful for correlating how many reverts were blocked vs. how many
// went through, and confirming the guards are firing.
func AuditRevertSkip(caller, path, reason string) {
	log.Printf("[SP077-AUDIT] revert-skip caller=%s path=%q reason=%s",
		caller, path, reason)
}
