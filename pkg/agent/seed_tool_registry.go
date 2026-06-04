package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	core "github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// NewSeedToolRegistry creates a seed core.ToolRegistry with all 30 sprout tools
// registered. The registry implements core.ToolExecutor directly, so it can be
// used as the Executor in core.Options.
//
// Seed's ToolRegistry handles: channel suffix stripping, alias resolution,
// argument parsing/repair, type coercion, required parameter validation,
// per-tool timeouts, result truncation, circuit breakers, parallel execution
// for SafeForParallel tools, and event publishing.
//
// Sprout-specific concerns are wired through:
//   - PreExecuteHook: security classification + subagent nesting prevention
//   - Handler closures: capture agent for sprout's (ctx, agent, args) signature
//     and apply all post-processing (constraints, truncation, secret redaction,
//     duplicate embedding check, TodoWrite events, error sanitization).
func NewSeedToolRegistry(agent *Agent) *core.ToolRegistry {
	var ep core.EventPublisher
	if agent != nil && agent.GetEventBus() != nil {
		ep = newRichEventPublisher(agent.GetEventBus(), agent)
	}

	return newSeedToolRegistryWithPublisher(agent, ep)
}

// newSeedToolRegistryWithPublisher creates a seed ToolRegistry using the
// provided EventPublisher. This is used by processQueryWithSeed which creates
// one shared publisher for both the registry and the seed core agent so that
// all events carry the same client_id/chat_id/user_id metadata.
//
// The registry is built by iterating sprout's local ToolConfig declarations
// (pkg/agent/tool_registrations.go) and converting each entry into a
// core.ToolConfig — local is the single source of truth. Sprout-side
// concerns are layered on per call:
//   - PreExecuteHook (registry-wide): security classification + subagent
//     nesting prevention
//   - Per-tool handler closure: captures agent for sprout's
//     (ctx, *Agent, args) signature and applies the standard pipeline
//     (logToolExecution → handler → handleToolError → postProcessResult).
func newSeedToolRegistryWithPublisher(agent *Agent, ep core.EventPublisher) *core.ToolRegistry {
	registry := core.NewToolRegistry(core.ToolRegistryOptions{
		DefaultTimeout: 5 * time.Minute,
		MaxResultSize:  50 * 1024,
		EventPublisher: ep,
		PreExecuteHook: newPreExecuteHook(agent),
	})

	for _, cfg := range GetToolRegistry().GetAllToolConfigs() {
		if err := registry.Register(convertToSeedToolConfig(cfg, agent)); err != nil {
			// Local registry deduplicates by name, so Register should never
			// fail with "already registered". Anything else is a programmer
			// error worth surfacing loudly.
			panic(fmt.Sprintf("seed registry: failed to register %q: %v", cfg.Name, err))
		}
	}

	return registry
}

// convertToSeedToolConfig maps a sprout-local ToolConfig to a seed
// core.ToolConfig, wrapping the handler signature transition and applying
// the standard sprout post-processing pipeline.
func convertToSeedToolConfig(cfg ToolConfig, agent *Agent) core.ToolConfig {
	name := cfg.Name
	seed := core.ToolConfig{
		Name:            name,
		Description:     cfg.Description,
		Parameters:      convertParametersToSeed(cfg.Parameters),
		Aliases:         cfg.Aliases,
		Timeout:         cfg.Timeout,
		MaxResultSize:   cfg.MaxResultSize,
		SafeForParallel: cfg.SafeForParallel,
	}
	if cfg.Handler != nil {
		h := cfg.Handler // closure capture
		seed.Handler = func(ctx context.Context, args map[string]interface{}) (string, error) {
			logToolExecution(agent, name)
			result, err := h(ctx, agent, args)
			if err != nil {
				return handleToolError(agent, err, name)
			}
			return postProcessResult(ctx, agent, name, args, result), nil
		}
	}
	if cfg.HandlerImages != nil {
		h := cfg.HandlerImages // closure capture
		seed.HandlerWithImages = func(ctx context.Context, args map[string]interface{}) ([]core.ImageData, string, error) {
			logToolExecution(agent, name)
			imgs, result, err := h(ctx, agent, args)
			if err != nil {
				msg, _ := handleToolError(agent, err, name)
				return imgs, msg, err
			}
			return imgs, postProcessResult(ctx, agent, name, args, result), nil
		}
	}
	return seed
}

// convertParametersToSeed translates sprout-local ParameterConfig into
// seed core.ParameterConfig (same fields, different package).
func convertParametersToSeed(local []ParameterConfig) []core.ParameterConfig {
	if local == nil {
		return nil
	}
	out := make([]core.ParameterConfig, len(local))
	for i, p := range local {
		out[i] = core.ParameterConfig{
			Name:         p.Name,
			Type:         p.Type,
			Required:     p.Required,
			Alternatives: p.Alternatives,
			Description:  p.Description,
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// richEventPublisher — enriches seed's lightweight tool_start/tool_end events
// with rich metadata (display_name, persona, is_subagent, subagent_type)
// and emits CLI tool_log output for tool execution.
// ---------------------------------------------------------------------------

// richEventPublisher wraps an *events.EventBus and enriches ALL events with
// agent event metadata (client_id, chat_id, user_id) so that the WebSocket
// event router can deliver them to the correct browser tab.  For tool_start
// and tool_end events it also adds display_name, persona, is_subagent, and
// subagent_type fields that the webui expects, and emits CLI tool_log output.
type richEventPublisher struct {
	bus   *events.EventBus
	agent *Agent
}

func newRichEventPublisher(bus *events.EventBus, agent *Agent) *richEventPublisher {
	return &richEventPublisher{bus: bus, agent: agent}
}

// Publish implements core.EventPublisher. All events are decorated with the
// agent's event metadata (client_id, chat_id, user_id) so that the WebSocket
// handler's shouldForwardEventToConnection can route them to the correct
// browser connection.  Tool events receive additional enrichment (display_name,
// persona, is_subagent, subagent_type) and emit CLI tool_log output.
func (r *richEventPublisher) Publish(eventType string, data any) {
	// Decorate with agent event metadata for WebSocket routing.
	data = r.decorateWithMetadata(data)

	switch eventType {
	case core.EventTypeToolStart, core.EventTypeToolEnd:
		// Tool execution now happens entirely inside seed's ToolRegistry, which
		// doesn't go through Agent.callTool — so the only place we can keep
		// sprout's TotalToolCalls counter in sync is here, on tool_end.
		// SubagentResult.ToolCalls reads this counter; without the increment,
		// every subagent reports 0 tool calls and the orchestrator thinks
		// nothing happened.
		if eventType == core.EventTypeToolEnd && r.agent != nil && r.agent.state != nil {
			r.agent.state.IncrementTotalToolCalls()
		}
		enriched := r.enrichEventData(data, eventType)
		r.bus.Publish(eventType, enriched)
	default:
		r.bus.Publish(eventType, data)
	}
}

// decorateWithMetadata merges the agent's event metadata (client_id, chat_id,
// user_id) into the event payload. This ensures that events published by seed's
// core agent through the EventPublisher are properly routed to the originating
// browser tab via shouldForwardEventToConnection. Without this decoration,
// events without client_id/chat_id are silently dropped by the WebSocket
// forwarding logic.
func (r *richEventPublisher) decorateWithMetadata(data any) any {
	if r.agent == nil {
		return data
	}
	return r.agent.decorateEventPayload(data)
}

// enrichEventData adds rich metadata fields to a tool event payload.
// For tool_end, it also emits a CLI tool_log when streaming is disabled.
func (r *richEventPublisher) enrichEventData(data any, eventType string) any {
	if data == nil {
		return data
	}

	payload, ok := data.(map[string]interface{})
	if !ok {
		return data
	}

	// Extract tool_name (both event types have this)
	toolName, _ := payload["tool_name"].(string)

	if toolName == "" {
		return data
	}

	displayName := buildDisplayName(toolName, argsFromPayload(payload))
	isSubagent := isSubagentTool(toolName)
	var subagentType string
	if isSubagent {
		subagentType = func() string {
			if toolName == "run_subagent" {
				return "single"
			}
			if toolName == "run_parallel_subagents" {
				return "parallel"
			}
			return ""
		}()
	}
	persona := extractPersona(payload)

	// Enrich with rich fields
	payload["display_name"] = displayName
	if persona != "" {
		payload["persona"] = persona
	}
	if isSubagent {
		payload["is_subagent"] = true
		payload["subagent_type"] = subagentType
	}

	// Emit CLI tool_log for tool execution progress.
	//
	// tool_start: a single dim "→ <displayName>" line at the moment the
	//   tool begins. Uses the cleaner buildDisplayName format (no
	//   nested brackets / quoted JSON) — the structured payload still
	//   carries the raw args for WebUI consumers.
	// tool_end:   an indented duration / outcome chip ("  ✓ 124ms") that
	//   visually pairs with the line above. Emitted unconditionally so
	//   the user always sees how long a tool took; streaming or not.
	if r.agent != nil {
		if eventType == core.EventTypeToolStart {
			r.agent.ToolLog("executing tool", displayName)
		} else if eventType == core.EventTypeToolEnd {
			if router := r.agent.OutputRouter(); router != nil {
				duration := extractDurationMs(payload)
				status, _ := payload["status"].(string)
				errMsg, _ := payload["error"].(string)
				router.RouteToolCompletion(status != "failed", duration, errMsg)
			}
		}
	}
	return payload
}

// extractDurationMs reads the duration_ms field from a tool_end payload.
// seed publishes this as either an int or a float depending on whether the
// duration is whole milliseconds — handle both.
func extractDurationMs(payload map[string]interface{}) time.Duration {
	switch v := payload["duration_ms"].(type) {
	case int:
		return time.Duration(v) * time.Millisecond
	case int64:
		return time.Duration(v) * time.Millisecond
	case float64:
		return time.Duration(v * float64(time.Millisecond))
	}
	return 0
}

// argsFromPayload returns the tool's argument map from a tool event
// payload. seed core publishes the args as a JSON string under
// "arguments" (see core.EventTypeToolStart), so the primary-arg keys
// buildDisplayName looks for ("command", "path", …) are NOT top-level —
// without decoding, every shell line rendered as a bare "shell_command".
// Falls back to a structured "args"/"input" map, then to the payload
// itself, so older event shapes still work.
func argsFromPayload(payload map[string]interface{}) map[string]interface{} {
	for _, k := range []string{"args", "input", "parameters"} {
		if m, ok := payload[k].(map[string]interface{}); ok {
			return m
		}
	}
	if s, ok := payload["arguments"].(string); ok && s != "" {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			return m
		}
	}
	return payload
}

// buildDisplayName constructs a human-readable tool name by appending the
// primary argument to the tool name. For example:
//   - read_file /path/to/file → "read_file /path/to/file"
//   - shell_command ls -la → "shell_command ls -la"
//   - run_subagent with prompt → "run_subagent [task]"
func buildDisplayName(toolName string, payload map[string]interface{}) string {
	switch toolName {
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if path, ok := payload["path"].(string); ok && path != "" {
			return fmt.Sprintf("%s %s", toolName, path)
		}
	case "shell_command":
		if cmd, ok := payload["command"].(string); ok && cmd != "" {
			// Collapse newlines / whitespace runs so a multi-line command
			// renders as a single scannable line, then truncate.
			cmd = strings.Join(strings.Fields(cmd), " ")
			if len(cmd) > 80 {
				return fmt.Sprintf("%s %s...", toolName, cmd[:77])
			}
			return fmt.Sprintf("%s %s", toolName, cmd)
		}
	case "git":
		if op, ok := payload["operation"].(string); ok && op != "" {
			if args, ok2 := payload["args"].(string); ok2 && args != "" {
				return fmt.Sprintf("%s %s %s", toolName, op, args)
			}
			return fmt.Sprintf("%s %s", toolName, op)
		}
	case "commit":
		if msg, ok := payload["message"].(string); ok && msg != "" {
			if len(msg) > 80 {
				return fmt.Sprintf("%s %s...", toolName, msg[:77])
			}
			return fmt.Sprintf("%s %s", toolName, msg)
		}
		return toolName
	case "search_files":
		if pattern, ok := payload["search_pattern"].(string); ok && pattern != "" {
			return fmt.Sprintf("%s %s", toolName, pattern)
		}
	case "web_search", "semantic_search":
		if query, ok := payload["query"].(string); ok && query != "" {
			if len(query) > 80 {
				return fmt.Sprintf("%s %s...", toolName, query[:77])
			}
			return fmt.Sprintf("%s %s", toolName, query)
		}
	case "fetch_url", "browse_url":
		if url, ok := payload["url"].(string); ok && url != "" {
			// Truncate URLs for readability
			if len(url) > 80 {
				return fmt.Sprintf("%s %s...", toolName, url[:77])
			}
			return fmt.Sprintf("%s %s", toolName, url)
		}
	case "run_subagent":
		if prompt, ok := payload["prompt"].(string); ok && prompt != "" {
			if len(prompt) > 80 {
				return fmt.Sprintf("%s [task: %s...]", toolName, prompt[:77])
			}
			return fmt.Sprintf("%s [task: %s]", toolName, prompt)
		}
	case "run_parallel_subagents":
		if subagents, ok := payload["subagents"].([]interface{}); ok {
			return fmt.Sprintf("%s (%d subagents)", toolName, len(subagents))
		}
		return toolName
	case "ask_user":
		if question, ok := payload["question"].(string); ok && question != "" {
			if len(question) > 80 {
				return fmt.Sprintf("%s %s...", toolName, question[:77])
			}
			return fmt.Sprintf("%s %s", toolName, question)
		}
	case "TodoWrite":
		if todos, ok := payload["todos"].([]interface{}); ok {
			return fmt.Sprintf("%s (%d items)", toolName, len(todos))
		}
	case "embedding_index":
		if operation, ok := payload["operation"].(string); ok && operation != "" {
			return fmt.Sprintf("%s %s", toolName, operation)
		}
	case "activate_skill":
		if skillID, ok := payload["skill_id"].(string); ok && skillID != "" {
			return fmt.Sprintf("%s %s", toolName, skillID)
		}
	case "manage_memory":
		// Surface the affected memory name for add/read/delete; for list/search
		// fall through to the bare tool name.
		if name, ok := payload["name"].(string); ok && name != "" {
			return fmt.Sprintf("%s %s", toolName, name)
		}
	case "analyze_ui_screenshot":
		if imgPath, ok := payload["image_path"].(string); ok && imgPath != "" {
			return fmt.Sprintf("%s %s", toolName, imgPath)
		}
	case "analyze_image_content":
		if imgPath, ok := payload["image_path"].(string); ok && imgPath != "" {
			return fmt.Sprintf("%s %s", toolName, imgPath)
		}
	}

	return toolName
}

// extractPersona extracts the persona from tool arguments if present.
func extractPersona(payload map[string]interface{}) string {
	if persona, ok := payload["persona"].(string); ok && persona != "" {
		return persona
	}
	return ""
}

// ---------------------------------------------------------------------------
// Post-processing helpers: inlined in handler closures (seed's PostExecuteHook
// only receives (name, result) — no agent, no args, no context).
// ---------------------------------------------------------------------------

// buildSecretSource constructs the source string used by the elevation gate
// to identify the origin of detected secrets.
func buildSecretSource(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "shell_command":
		if cmd, ok := args["command"].(string); ok {
			if len(cmd) > 80 {
				return toolName + ": " + cmd[:77] + "..."
			}
			return toolName + ": " + cmd
		}
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if path, ok := args["path"].(string); ok && path != "" {
			return toolName + ": " + path
		}
	case "search_files":
		if pattern, ok := args["search_pattern"].(string); ok && pattern != "" {
			return toolName + ": " + pattern
		}
	}
	return toolName
}

// formatTodoItemsForEvent converts a []tools.TodoItem slice into the
// []map[string]interface{} format expected by PublishTodoUpdate.
func formatTodoItemsForEvent(todos []tools.TodoItem) []map[string]interface{} {
	result := make([]map[string]interface{}, len(todos))
	for i, t := range todos {
		result[i] = map[string]interface{}{
			"id":      t.ID,
			"content": t.Content,
			"status":  t.Status,
		}
	}
	return result
}

// logToolExecution is a legacy helper that was used to print tool execution
// messages in non-streaming mode. Now that richEventPublisher emits
// ToolLog("executing tool", ...) on tool_start for all modes, this is a
// no-op to avoid duplicate output.
func logToolExecution(_ *Agent, _ string) {
}

// handleToolError wraps a handler error into a sanitized result string and
// returns it along with the original error. Returning a non-nil error ensures
// seed's circuit breaker failure tracking and success/error classification
// work correctly, while the result string is sanitized for secret safety
// and model context. It also prints [FAIL] or [⚠️ SECURITY CAUTION] lines
// to the terminal (routed through the streaming callback for subagents).
func handleToolError(agent *Agent, err error, toolName string) (string, error) {
	if err == nil {
		return "", nil
	}
	safeMsg := sanitizeToolFailureMessage(err.Error())

	// Use typed error classification first to decide behavior.
	action := ClassifyError(err)

	switch action {
	case ActionEscalate:
		// Security error (typed) — escalate to user/LLM.
		if agent != nil {
			agent.PrintLine("")
			agent.PrintLine(fmt.Sprintf("[⚠️  SECURITY CAUTION - LLM VERIFICATION REQUIRED] %s", safeMsg))
			agent.PrintLine("")
		}
		return fmt.Sprintf("SECURITY_CAUTION_REQUIRED: %s", safeMsg), err

	case ActionFail:
		// Permanent/invalid input/context overflow — no retry.
		if agent != nil {
			agent.PrintLine("")
			agent.PrintLine(fmt.Sprintf("[FAIL] Tool '%s' failed: %s", toolName, safeMsg))
			agent.PrintLine("")
		}
		return fmt.Sprintf("Error: %s", safeMsg), err

	default:
		// ActionRetry — transient/rate-limited/unknown errors.
		// Log and return as normal error for potential retry.
		// Sub-classify so the LLM gets more context for its retry decision.
		if agent != nil {
			agent.PrintLine("")
			switch {
			case agenterrors.IsRateLimited(err):
				agent.PrintLine(fmt.Sprintf("[FAIL] Tool '%s' failed (rate limited): %s", toolName, safeMsg))
			case agenterrors.IsProviderError(err):
				agent.PrintLine(fmt.Sprintf("[FAIL] Tool '%s' failed (provider): %s", toolName, safeMsg))
			default:
				agent.PrintLine(fmt.Sprintf("[FAIL] Tool '%s' failed (transient): %s", toolName, safeMsg))
			}
			agent.PrintLine("")
		}
		return fmt.Sprintf("Error: %s", safeMsg), err
	}
}
// isLocalProvider returns true if the provider runs locally and never sends
// data outside the user's network. Secret redaction is skipped for these
// providers since there's no off-network leakage risk.
func isLocalProvider(agent *Agent) bool {
	if agent == nil {
		return false
	}
	ct := agent.GetProviderType()
	switch ct {
	case api.OllamaLocalClientType,
		api.OllamaClientType,      // "ollama" alias for ollama-local
		api.OllamaCloudClientType,
		api.LMStudioClientType,
		api.TestClientType,
		api.EditorClientType:
		return true
	}
	return false
}

// 1. Model-specific constraints (fetch_url truncation, analyze_image_content compaction)
// 2. Universal truncation (50K cap)
// 3. Secret redaction with elevation gate
// 4. Duplicate embedding check for write tools
// 5. TodoWrite event emission
// Returns the final result string to show to the LLM.
func postProcessResult(ctx context.Context, agent *Agent, toolName string, args map[string]interface{}, result string) string {
	if result == "" {
		return result
	}

	// 1. Model-specific constraints (constrainToolResultForModel handles fetch_url and analyze_image_content)
	result = constrainToolResultForModel(toolName, args, result)

	// 2. Universal truncation
	result = truncateToolResult(result)

	// 3. Secret redaction (only for sensitive tools, skip if local provider)
	if !isLocalProvider(agent) && isSecretSensitiveTool(toolName) && agent.security.GetOutputRedactor() != nil {
		redactResult := agent.security.GetOutputRedactor().RedactToolOutput(result, toolName, args)
		if len(redactResult.Secrets) > 0 {
			source := buildSecretSource(toolName, args)
			action, evalErr := agent.security.GetElevationGate().Evaluate(redactResult.Secrets, source)
			if evalErr != nil {
				if agent.debug {
					agent.debugLog("[security] elevation gate error: %v\n", evalErr)
				}
			}
			switch action {
			case security.SecretAllow:
				// keep original (already redacted by the redactor as fallback)
				if agent.debug {
					agent.debugLog("[security] user allowed %d secret(s) in %s\n", len(redactResult.Secrets), toolName)
				}
				result = redactResult.Content
			case security.SecretBlock:
				if agent.debug {
					agent.debugLog("[security] blocked %d secret(s) in %s\n", len(redactResult.Secrets), toolName)
				}
				return fmt.Sprintf("BLOCKED: detected secrets in output. Operation blocked. Found %d secret(s) — user chose to block.", len(redactResult.Secrets))
			default:
				// SecretRedact — redactResult.Content is already redacted
				if agent.debug {
					agent.debugLog("[security] redacted %d secret(s) from %s\n", len(redactResult.Secrets), toolName)
				}
				result = redactResult.Content
			}
		}
	}

	// 4. Duplicate embedding check + async re-index for write tools
	if shouldCheckDuplicates(toolName, agent) {
		if path, ok := args["path"].(string); ok && path != "" {
			if note := runDuplicateCheck(ctx, agent, path); note != "" {
				result = result + note
			}
			reindexFileAfterWrite(agent, path)
		}
	}

	// 5. TodoWrite event emission
	if toolName == "TodoWrite" {
		agent.PublishTodoUpdate(formatTodoItemsForEvent(agent.GetTodoManager().Read()))
	}

	return result
}

// ---------------------------------------------------------------------------
// Pre-execute hook: security classification + subagent nesting prevention
// ---------------------------------------------------------------------------

func newPreExecuteHook(agent *Agent) func(name string, args map[string]interface{}) error {
	if agent == nil {
		return nil
	}
	return func(name string, args map[string]interface{}) error {
		// 1. Depth-based subagent nesting prevention
		// Agents at or beyond the maximum nesting depth cannot spawn further subagents.
		// This prevents runaway agent chains while allowing configurable multi-level nesting.
		// ask_user is allowed for subagents because they share the event bus with the
		// primary agent and questions are routed through the same WebUI/CLI prompt mechanism.
		if !agent.CanSpawnSubagents() {
			if name == "run_subagent" || name == "run_parallel_subagents" {
				return agenterrors.NewSecurityError(
					fmt.Sprintf("SUBAGENT_RESTRICTION: Agent at depth %d cannot spawn subagents (max depth: %d). "+
						"This restriction prevents runaway agent chains and ensures proper task delegation. "+
						"If you need additional work done, please complete your current task and return "+
						"your results to the parent agent for further delegation.",
						agent.SubagentDepth(), agent.MaxSubagentDepth()), nil)
			}
		}

		// 2. Security classification
		secResult := tools.ClassifyToolCall(name, args)
		if !secResult.ShouldBlock && !secResult.ShouldPrompt {
			return nil // safe, no action needed
		}

		// Unsafe mode or session elevation skips the interactive prompt
		// for non-hard-block operations. Shared policy with
		// ToolRegistry.ExecuteTool via staticGateAutoApprove, so clicking
		// "Elevate (session)" actually suppresses subsequent static-classifier
		// prompts on the live seed path (not just the filesystem gate).
		if agent.staticGateAutoApprove(secResult) {
			if agent.debug {
				agent.debugLog("[UNLOCK] Static gate auto-approve (unsafe/elevated): bypassing security validation for %s (risk: %s)\n", name, secResult.Risk)
			}
			return nil
		}

		isSubagent := agent.IsSubagent()

		// Persistent allowlist: shell commands the user previously chose
		// "Always approve" for short-circuit BEFORE any prompt UI fires.
		// Critical-tier ops are evaluated separately in tool_handlers_shell.go
		// and cannot be allowlisted, so this is safe.
		if name == "shell_command" {
			if cmd, ok := args["command"].(string); ok && cmd != "" && agent.IsShellCommandAllowlisted(cmd) {
				// Mark so Gate 2 also sees it as approved.
				agent.markShellCommandApproved(cmd)
				return nil
			}
		}

		// WebUI approval path
		if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && !isSubagent && agent.HasActiveWebUIClients() {
			if agent.debug {
				agent.debugLog("[APPROVAL] Requesting security approval via webui for %s (risk: %s)\n", name, secResult.Risk)
			}
			extras := map[string]string{}
			if secResult.RiskType != "" {
				extras["risk_type"] = formatRiskType(secResult.RiskType)
			}
			var shellCommand string
			switch name {
			case "shell_command":
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					extras["command"] = cmd
					shellCommand = cmd
					// Signal the frontend that this prompt supports the
					// 4-option dialog (Approve / Deny / Always / Elevate).
					extras["allow_options"] = "true"
				}
			case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
				if path, ok := args["path"].(string); ok && path != "" {
					extras["target"] = path
				}
			case "git":
				if op, ok := args["operation"].(string); ok && op != "" {
					extras["target"] = fmt.Sprintf("git %s", op)
				}
			}
			if name == "shell_command" && shellCommand != "" {
				decision := mgr.RequestToolApprovalDecision(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), name, secResult.Risk.String(), secResult.Reasoning, extras)
				if !decision.Approved() {
					return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil)
				}
				agent.applyApprovalDecision(decision, shellCommand)
				agent.markShellCommandApproved(shellCommand)
				return nil
			}
			if !mgr.RequestToolApproval(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), name, secResult.Risk.String(), secResult.Reasoning, extras) {
				return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil)
			}
			return nil
		}

		// CLI approval path
		agentConfig := agent.GetConfig()
		logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
		canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

		if canPrompt {
			// shell_command gets the 4-option dialog so the user can
			// allowlist or elevate inline. Other tools stay on the
			// classic yes/no path until the dialog is generalized.
			if name == "shell_command" {
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					prompt := buildShellApprovalPrompt(secResult)
					choice := logger.AskForApprovalWithOptions(prompt, cmd)
					decision := approvalDecisionFromCLIChoice(choice)
					if !decision.Approved() {
						return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil)
					}
					agent.applyApprovalDecision(decision, cmd)
					agent.markShellCommandApproved(cmd)
					return nil
				}
			}
			prompt := buildSecurityPrompt(name, args, secResult)
			if !logger.AskForConfirmation(prompt, false, false) {
				return agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil)
			}
			return nil
		}

		// Non-interactive paths
		if secResult.ShouldBlock {
			return agenterrors.NewSecurityError(fmt.Sprintf("security block: %s — %s", name, secResult.Reasoning), nil)
		}

		if secResult.ShouldPrompt && !isSubagent {
			return agenterrors.NewSecurityError(
				fmt.Sprintf("security caution: %s — %s (requires LLM verification: confirm this action is safe, expected, and aligned with user goals before proceeding)",
					name, secResult.Reasoning), nil)
		}

		return nil
	}
}
