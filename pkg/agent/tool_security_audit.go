// Extracted from tool_security.go — audit/logging helpers (SP-098).

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// auditPathDecision emits a JSONL audit entry for a filesystem gate decision.
// Nil-safe: skips silently when no audit logger is configured on ctx.
// Uses filesystem.AuditEntry (identical JSON fields to tools.AuditEntry)
// to avoid import cycles between pkg/agent and pkg/agent_tools.
func (a *Agent) auditPathDecision(ctx context.Context, filePath, resolvedPath, mode, action, riskLevel string) {
	logger := filesystem.AuditLoggerFromContext(ctx)
	if logger == nil {
		return
	}
	entry := filesystem.AuditEntry{
		Timestamp: time.Now(),
		Tool:      "filesystem_classify",
		Args:      filePath,
		RiskLevel: riskLevel,
		Category:  "fs_gate",
		Action:    action,
		Reasoning: fmt.Sprintf("path tier check: path=%s resolved=%s mode=%s action=%s", filePath, resolvedPath, mode, action),
		Source:    "gate1-classifier",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = logger.LogJSON(data)
}
