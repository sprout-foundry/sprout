package agent

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/sprout-foundry/sprout/pkg/agent_audit"
)

// toolAuditFileArgKeys are the argument keys that name the file(s) a tool
// operates on. The audit event records these declared paths as the files the
// call touched — a factual, content-free summary of a tool's file footprint.
var toolAuditFileArgKeys = []string{"path", "file_path", "filepath", "files", "target_file"}

// emitToolAudit records a facts-only audit event for one tool execution. It
// hashes the arguments (never storing them), records the result status, and
// lists the file paths the arguments declared. Nil-safe: a no-op when no audit
// sink is installed.
func (a *Agent) emitToolAudit(ctx context.Context, toolName string, args map[string]interface{}, status string) {
	if agent_audit.CurrentSink() == nil {
		return
	}
	argsJSON, _ := json.Marshal(args)
	argsHash, argsBytes := agent_audit.Digest(argsJSON)

	ev := agent_audit.ToolEvent{
		Tool:         toolName,
		ArgsSHA256:   argsHash,
		ArgsBytes:    argsBytes,
		Status:       status,
		FilesTouched: declaredFilePaths(args),
	}
	if a != nil {
		ev.ChatID = a.GetChatID()
		ev.SessionID = a.GetSessionID()
		ev.Trigger = a.auditTrigger()
	}
	agent_audit.EmitTool(ctx, ev)
}

// auditTrigger classifies this agent's calls: a subagent's calls are
// subagent-triggered, everything else is an agent turn.
func (a *Agent) auditTrigger() string {
	if a != nil && a.IsSubagent() {
		return agent_audit.TriggerSubagent
	}
	return agent_audit.TriggerUserTurn
}

// declaredFilePaths extracts the file paths a tool's arguments name, deduped
// and sorted for a deterministic event. It accepts a single string path or a
// list of string paths under any of the known file-argument keys.
func declaredFilePaths(args map[string]interface{}) []string {
	if len(args) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	for _, key := range toolAuditFileArgKeys {
		raw, ok := args[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case string:
			if v != "" {
				seen[v] = struct{}{}
			}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					seen[s] = struct{}{}
				}
			}
		case []string:
			for _, s := range v {
				if s != "" {
					seen[s] = struct{}{}
				}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
