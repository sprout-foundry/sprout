package agent

import (
	"strings"
)

// ---------------------------------------------------------------------------
// Subagent result envelope — typed structures and builder helpers
// ---------------------------------------------------------------------------

// statusFromResult maps the legacy resultMap state to a typed Status.
func statusFromResult(result *SubagentResult, m map[string]string) SubagentStatus {
	if result != nil && result.BudgetExceeded {
		return SubagentStatusBudgetExceeded
	}
	if m["exit_code"] != "0" {
		stderr := m["stderr"]
		stdout := m["stdout"]
		if strings.Contains(stderr, "ErrOutsideWorkingDirectory") ||
			strings.Contains(stderr, "ErrWriteOutsideWorkingDirectory") ||
			strings.Contains(stderr, "security") ||
			strings.Contains(stdout, "SUBAGENT_SECURITY_ERROR") {
			return SubagentStatusSecurityBlocked
		}
		return SubagentStatusFailed
	}
	return SubagentStatusCompleted
}

// buildSubagentReturn assembles the typed envelope from the legacy
// resultMap plus the SubagentResult struct. Mirrors all existing string
// keys for LLM-shape compatibility and layers on the typed fields.
func buildSubagentReturn(m map[string]string, result *SubagentResult, status SubagentStatus) *SubagentReturn {
	r := &SubagentReturn{
		Output:         m["stdout"],
		Stderr:         m["stderr"],
		ExitCode:       firstNonEmpty(m["exit_code"], "0"),
		Completed:      firstNonEmpty(m["completed"], "true"),
		TimedOut:       firstNonEmpty(m["timed_out"], "false"),
		BudgetExceeded: firstNonEmpty(m["budget_exceeded"], "false"),
		ElapsedSeconds: m["elapsed_seconds"],
		TokensUsed:     m["tokens_used"],
		Cost:           m["cost"],
		ToolCallCount:  m["tool_calls"],
		Summary:        m["summary"],
		ContextUsed:    m["context_used"],
		FilesUsed:      m["files_used"],
		WorkingDir:     m["working_dir"],
		Status:         status,
	}
	if status != SubagentStatusCompleted {
		// Surface stderr as the human-readable reason. The legacy
		// "error" key on resultMap (added in the failure path above)
		// is more descriptive when present.
		if reason, ok := m["error"]; ok {
			r.ErrorReason = reason
		} else if m["stderr"] != "" {
			r.ErrorReason = m["stderr"]
		}
	}
	if result != nil {
		r.Metrics = SubagentRunMetrics{
			TokensUsed: result.TokensUsed,
			Cost:       result.Cost,
			ToolCalls:  result.ToolCalls,
			Iterations: result.Iterations,
		}
		// SP-059 Phase 2c: structured file manifest, no more regex
		// scraping. Map ChangeTracker's operation labels (write/edit/
		// create) to the simpler created/modified/deleted vocabulary
		// the envelope exposes. Both "write" and "create" map to
		// "created" because the tracker doesn't distinguish them.
		if len(result.FileChanges) > 0 {
			files := make([]FileChange, 0, len(result.FileChanges))
			for _, ch := range result.FileChanges {
				files = append(files, FileChange{
					Path: ch.FilePath,
					Op:   normalizeChangeOp(ch.Operation),
				})
			}
			r.FilesModified = files
			// Prepend a plain-text manifest to the Output so the primary's
			// LLM cannot miss it — the structured FilesModified field is
			// also present, but a header in the text field surfaces the
			// information at the very top of what the model reads. The
			// observed failure mode was a primary that read the Output
			// and reverted "unfamiliar" diff because it didn't realize
			// the JSON envelope carried an authoritative file list.
			r.Output = prependFilesModifiedHeader(r.Output, files)
		}
		// SP-059 Phase 3a: pass the progress timeline through so the
		// primary's LLM sees what the subagent did, not just the
		// final answer. Type conversion exists because the runner's
		// SubagentProgressEntry is internal — envelope ships its own
		// ProgressEntry so the wire format is decoupled.
		if len(result.ProgressLog) > 0 {
			log := make([]ProgressEntry, 0, len(result.ProgressLog))
			for _, p := range result.ProgressLog {
				log = append(log, ProgressEntry{
					OffsetMS: p.OffsetMS,
					Phase:    p.Phase,
					Message:  p.Message,
				})
			}
			r.ProgressLog = log
		}
	}
	return r
}

// prependFilesModifiedHeader renders a git-style manifest of files the
// subagent touched and prepends it to the subagent's final assistant
// output. The format mirrors the inline checkpoint summary syntax used
// elsewhere (A/M/D one-letter ops) so the primary's LLM sees a
// consistent vocabulary across in-turn and subagent boundaries.
//
// Example header:
//
//	[subagent files modified]
//	M pkg/foo.go
//	A pkg/new.go
//	D pkg/old.go
//	[/subagent files modified]
//
// The wrapping sentinel tags make it grep-friendly for downstream
// consumers (logs, tests) and unambiguous as a section boundary inside
// what is otherwise free-form prose.
func prependFilesModifiedHeader(output string, files []FileChange) string {
	if len(files) == 0 {
		return output
	}
	var b strings.Builder
	b.WriteString("[subagent files modified]\n")
	for _, f := range files {
		b.WriteString(opLetter(f.Op))
		b.WriteByte(' ')
		b.WriteString(f.Path)
		b.WriteByte('\n')
	}
	b.WriteString("[/subagent files modified]\n\n")
	b.WriteString(output)
	return b.String()
}

// opLetter compresses the verbose envelope op vocabulary back to the
// one-letter git-style code used in the manifest header. Mirrors
// pkg/agent/turn_checkpoints.go's appendFileMetadataToSummary.
func opLetter(op string) string {
	switch op {
	case "created":
		return "A"
	case "modified":
		return "M"
	case "deleted":
		return "D"
	case "renamed":
		return "R"
	default:
		// Unknown op — pass through uppercase first letter so the manifest
		// still parses as one-letter codes for grep / tooling.
		if op == "" {
			return "?"
		}
		return strings.ToUpper(op[:1])
	}
}

func normalizeChangeOp(op string) string {
	switch op {
	case "write", "create":
		return "created"
	case "edit":
		return "modified"
	case "delete":
		return "deleted"
	default:
		// Pass through unknown verbs so future ChangeTracker additions
		// surface in the manifest without code changes here.
		return op
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
