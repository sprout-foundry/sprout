package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

const strictSwitchRecentToolSummaryCount = 6

var fullOutputPathRegex = regexp.MustCompile(`Full output saved to ([^\]\n]+)`)

type strictSyntaxNormalizationReport struct {
	beforeMessages                  int
	afterMessages                   int
	beforeTokens                    int
	afterTokens                     int
	removedToolMessages             int
	strippedAssistantToolCallBlocks int
	droppedEmptyAssistantMessages   int
	toolSummaryEntries              int
	retainedRecentToolSummaries     int
}

func (a *Agent) isStrictToolCallSyntaxModel() bool {
	if a == nil {
		return false
	}
	provider := strings.ToLower(strings.TrimSpace(a.GetProvider()))
	model := strings.ToLower(strings.TrimSpace(a.GetModel()))
	return provider == "minimax" || provider == "deepseek" ||
		strings.Contains(model, "minimax") || strings.Contains(model, "deepseek")
}

func estimateMessageTokens(messages []api.Message) int {
	total := 0
	for _, m := range messages {
		total += EstimateTokens(m.Content)
		if m.ReasoningContent != "" {
			total += EstimateTokens(m.ReasoningContent)
		}
	}
	return total
}

func summarizeToolMessage(msg api.Message) (string, string) {
	header := strings.Split(msg.Content, "\n")[0]
	header = strings.TrimSpace(header)
	if header == "" {
		return "Tool result (summary unavailable)", ""
	}

	if strings.HasPrefix(header, "Tool call result for read_file:") {
		path := strings.TrimSpace(strings.TrimPrefix(header, "Tool call result for read_file:"))
		return fmt.Sprintf("Read file: %s", path), ""
	}

	if strings.HasPrefix(header, "Tool call result for shell_command:") {
		cmd := strings.TrimSpace(strings.TrimPrefix(header, "Tool call result for shell_command:"))
		artifact := ""
		if matches := fullOutputPathRegex.FindStringSubmatch(msg.Content); len(matches) > 1 {
			artifact = strings.TrimSpace(matches[1])
		}
		if artifact != "" {
			return fmt.Sprintf("Shell command: `%s` (full output: %s)", cmd, artifact), artifact
		}
		return fmt.Sprintf("Shell command: `%s`", cmd), ""
	}

	return header, ""
}

func summarizeAssistantToolCalls(msg api.Message) string {
	if len(msg.ToolCalls) == 0 {
		return ""
	}
	toolNames := make([]string, 0, len(msg.ToolCalls))
	seen := make(map[string]struct{}, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			name = "unknown_tool"
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		toolNames = append(toolNames, name)
	}
	if len(toolNames) == 0 {
		return "Assistant tool-call block"
	}
	return fmt.Sprintf("Assistant invoked tools: %s", strings.Join(toolNames, ", "))
}

func buildToolCompressionMessage(toolSummaries []string, artifactPointers []string) string {
	var b strings.Builder
	b.WriteString("Context preserved from prior tool interactions (strict syntax mode):\n")
	for i, line := range toolSummaries {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, line))
	}
	if len(artifactPointers) > 0 {
		b.WriteString("\nArtifacts for deep details:\n")
		for _, p := range artifactPointers {
			b.WriteString("- ")
			b.WriteString(p)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nUse these summaries and artifacts to avoid re-running expensive steps unless needed.")
	return strings.TrimSpace(b.String())
}

func normalizeConversationForStrictToolSyntax(messages []api.Message) ([]api.Message, strictSyntaxNormalizationReport) {
	report := strictSyntaxNormalizationReport{
		beforeMessages: len(messages),
		beforeTokens:   estimateMessageTokens(messages),
	}
	if len(messages) == 0 {
		return messages, report
	}

	normalized := make([]api.Message, 0, len(messages))
	toolSummaries := make([]string, 0, 16)
	artifactSet := make(map[string]struct{})

	for _, msg := range messages {
		switch msg.Role {
		case "tool":
			report.removedToolMessages++
			summary, artifact := summarizeToolMessage(msg)
			if summary != "" {
				toolSummaries = append(toolSummaries, summary)
			}
			if artifact != "" {
				artifactSet[artifact] = struct{}{}
			}
			continue
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				report.strippedAssistantToolCallBlocks++
				if summary := summarizeAssistantToolCalls(msg); summary != "" {
					toolSummaries = append(toolSummaries, summary)
				}
				rewritten := msg
				rewritten.ToolCalls = nil
				if strings.TrimSpace(rewritten.Content) == "" && strings.TrimSpace(rewritten.ReasoningContent) == "" {
					report.droppedEmptyAssistantMessages++
					continue
				}
				normalized = append(normalized, rewritten)
				continue
			}
		}
		normalized = append(normalized, msg)
	}

	report.toolSummaryEntries = len(toolSummaries)
	if len(toolSummaries) > 0 {
		start := 0
		if len(toolSummaries) > strictSwitchRecentToolSummaryCount {
			start = len(toolSummaries) - strictSwitchRecentToolSummaryCount
		}
		recent := toolSummaries[start:]
		report.retainedRecentToolSummaries = len(recent)

		compressed := make([]string, 0, len(recent)+1)
		if start > 0 {
			compressed = append(compressed, fmt.Sprintf("... %d earlier tool interactions summarized and omitted for brevity", start))
		}
		compressed = append(compressed, recent...)

		artifactPointers := make([]string, 0, len(artifactSet))
		for p := range artifactSet {
			artifactPointers = append(artifactPointers, p)
		}
		sort.Strings(artifactPointers)
		if len(artifactPointers) > 6 {
			artifactPointers = artifactPointers[:6]
		}

		normalized = append(normalized, api.Message{
			Role:    "assistant",
			Content: buildToolCompressionMessage(compressed, artifactPointers),
		})
	}

	report.afterMessages = len(normalized)
	report.afterTokens = estimateMessageTokens(normalized)
	return normalized, report
}

func (a *Agent) buildSwitchContextRefreshMessage(report strictSyntaxNormalizationReport, fromProvider, fromModel string) string {
	if a == nil {
		return ""
	}

	toProvider := a.GetProvider()
	toModel := a.GetModel()

	var b strings.Builder
	b.WriteString("Provider/model switch compatibility refresh:\n")
	b.WriteString(fmt.Sprintf("- Switched from `%s/%s` to `%s/%s`\n",
		strings.TrimSpace(fromProvider), strings.TrimSpace(fromModel),
		strings.TrimSpace(toProvider), strings.TrimSpace(toModel)))
	b.WriteString(fmt.Sprintf("- History normalization: removed %d tool results, stripped %d tool-call blocks, dropped %d empty assistant stubs\n",
		report.removedToolMessages, report.strippedAssistantToolCallBlocks, report.droppedEmptyAssistantMessages))
	b.WriteString(fmt.Sprintf("- Context footprint: ~%d -> ~%d tokens\n", report.beforeTokens, report.afterTokens))

	if len(a.taskActions) > 0 {
		b.WriteString("- Recent completed actions:\n")
		start := 0
		if len(a.taskActions) > 6 {
			start = len(a.taskActions) - 6
		}
		for i := start; i < len(a.taskActions); i++ {
			action := a.taskActions[i]
			b.WriteString(fmt.Sprintf("  - %s: %s\n", action.Type, action.Description))
		}
	}

	if a.changeTracker != nil {
		files := a.changeTracker.GetTrackedFiles()
		if len(files) > 0 {
			if len(files) > 8 {
				files = files[len(files)-8:]
			}
			b.WriteString("- Recently changed files:\n")
			for _, f := range files {
				b.WriteString("  - ")
				b.WriteString(f)
				b.WriteString("\n")
			}
		}
	}

	shellArtifacts := make([]string, 0, len(a.shellCommandHistory))
	for _, h := range a.shellCommandHistory {
		if h == nil || strings.TrimSpace(h.FullOutputPath) == "" {
			continue
		}
		shellArtifacts = append(shellArtifacts, fmt.Sprintf("%d|%s|%s", h.ExecutedAt, h.Command, h.FullOutputPath))
	}
	sort.Strings(shellArtifacts)
	if len(shellArtifacts) > 5 {
		shellArtifacts = shellArtifacts[len(shellArtifacts)-5:]
	}
	if len(shellArtifacts) > 0 {
		b.WriteString("- Shell output artifacts:\n")
		for _, row := range shellArtifacts {
			parts := strings.SplitN(row, "|", 3)
			if len(parts) == 3 {
				b.WriteString(fmt.Sprintf("  - `%s` -> %s\n", parts[1], parts[2]))
			}
		}
	}

	b.WriteString("- If more detail is needed, re-open the referenced files/artifacts instead of replaying old tool-call protocol messages.")
	return strings.TrimSpace(b.String())
}

func (a *Agent) setPendingSwitchContextRefresh(msg string) {
	if a == nil {
		return
	}
	a.pendingSwitchContextRefresh = strings.TrimSpace(msg)
}

func (a *Agent) consumePendingSwitchContextRefresh() string {
	if a == nil {
		return ""
	}
	msg := strings.TrimSpace(a.pendingSwitchContextRefresh)
	a.pendingSwitchContextRefresh = ""
	return msg
}

func (a *Agent) setPendingStrictSwitchNotice(msg string) {
	if a == nil {
		return
	}
	a.pendingStrictSwitchNotice = strings.TrimSpace(msg)
}

func (a *Agent) ConsumePendingStrictSwitchNotice() string {
	if a == nil {
		return ""
	}
	msg := strings.TrimSpace(a.pendingStrictSwitchNotice)
	a.pendingStrictSwitchNotice = ""
	return msg
}

func (a *Agent) normalizeConversationForCurrentModelSyntax(fromProvider, fromModel string) {
	if a == nil || len(a.messages) == 0 || !a.isStrictToolCallSyntaxModel() {
		return
	}

	normalized, report := normalizeConversationForStrictToolSyntax(a.messages)
	a.messages = normalized

	refresh := a.buildSwitchContextRefreshMessage(report, fromProvider, fromModel)
	a.setPendingSwitchContextRefresh(refresh)

	if report.removedToolMessages > 0 || report.strippedAssistantToolCallBlocks > 0 {
		a.setPendingStrictSwitchNotice(fmt.Sprintf(
			"history compressed for strict syntax: %d tool result(s) removed, %d tool-call block(s) compacted, %d summary block(s) retained",
			report.removedToolMessages, report.strippedAssistantToolCallBlocks, report.retainedRecentToolSummaries,
		))
		a.PrintLineAsync(fmt.Sprintf(
			"🔄 Strict syntax normalization applied: removed %d tool result(s), stripped %d tool-call block(s), ~%d -> ~%d tokens",
			report.removedToolMessages, report.strippedAssistantToolCallBlocks, report.beforeTokens, report.afterTokens,
		))
		if a.debug {
			a.debugLog("🧹 Strict syntax switch normalization report: %+v\n", report)
		}
	}
}
