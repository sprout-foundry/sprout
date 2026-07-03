//go:build !js

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// gateResult is the JSON response from the gate LLM call.
type gateResult struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Skip       bool   `json:"skip"`
	SkipReason string `json:"skip_reason"`
}

// gateTriageResult is the JSON response from the triage gate call.
type gateTriageResult struct {
	Action string `json:"action"` // "retry" or "skip"
	Reason string `json:"reason"`
}

// findNextTodoItem reads a markdown file and returns:
// - lineNum: the 1-based line number of the first "[ ]" item found
// - sectionText: the text of the enclosing ## section (from the ## header above
//   the item to just before the next ## header or end of file)
// - err: non-nil if the file can't be read or no unchecked items exist
func findNextTodoItem(todoFile string) (lineNum int, sectionText string, err error) {
	data, err := os.ReadFile(filepath.Clean(todoFile))
	if err != nil {
		return 0, "", fmt.Errorf("failed to read %s: %w", todoFile, err)
	}

	lines := strings.Split(string(data), "\n")
	uncheckedRe := regexp.MustCompile(`^\s*- \[ \]`)

	// Find first unchecked item line (1-based).
	var itemLine int // 0-based index
	for i, line := range lines {
		if uncheckedRe.MatchString(line) {
			itemLine = i
			break
		}
	}
	if itemLine == 0 && !uncheckedRe.MatchString(lines[0]) {
		// No unchecked items found (itemLine stayed at 0 but first line doesn't match).
		return 0, "", fmt.Errorf("no unchecked [ ] items found in %s", todoFile)
	}

	// Find the enclosing ## section header by searching upward.
	headerRe := regexp.MustCompile(`^## `)
	sectionStart := 0
	for i := itemLine - 1; i >= 0; i-- {
		if headerRe.MatchString(lines[i]) {
			sectionStart = i
			break
		}
	}

	// Find the next ## header after the item (search downward).
	sectionEnd := len(lines)
	for i := itemLine + 1; i < len(lines); i++ {
		if headerRe.MatchString(lines[i]) {
			sectionEnd = i
			break
		}
	}

	sectionText = strings.Join(lines[sectionStart:sectionEnd], "\n")
	return itemLine + 1, sectionText, nil // return 1-based line number
}

// markTodoDone changes "- [ ]" to "- [x]" at the given 1-based line number
// in the specified markdown file.
func markTodoDone(todoFile string, lineNum int) error {
	data, err := os.ReadFile(filepath.Clean(todoFile))
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", todoFile, err)
	}

	lines := bytes.Split(data, []byte("\n"))
	if lineNum < 1 || lineNum > len(lines) {
		return fmt.Errorf("line number %d out of range (file has %d lines)", lineNum, len(lines))
	}

	idx := lineNum - 1 // 0-based
	orig := lines[idx]
	modified := bytes.Replace(orig, []byte("- [ ]"), []byte("- [x]"), 1)

	if bytes.Equal(orig, modified) {
		return fmt.Errorf("line %d does not contain '- [ ]': %s", lineNum, orig)
	}

	lines[idx] = modified
	return os.WriteFile(filepath.Clean(todoFile), bytes.Join(lines, []byte("\n")), 0644)
}

// providerBaseURL returns the OpenAI-compatible chat completions base URL
// for the given provider name.
func providerBaseURL(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "openai":
		return "https://api.openai.com/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "deepinfra":
		return "https://api.deepinfra.com/v1/openai"
	case "zai", "z.ai":
		return "https://z.ai/v1"
	case "mistral":
		return "https://api.mistral.ai/v1"
	case "minimax":
		return "https://api.minimax.chat/v1"
	case "chutes":
		return "https://chutes.ai/v1"
	case "cerebras":
		return "https://api.cerebras.ai/v1"
	case "ollama", "ollama-local":
		return "http://localhost:11434/v1"
	case "ollama-cloud":
		return "https://turbo.ollama.ai/v1"
	case "lmstudio":
		return "http://localhost:1234/v1"
	default:
		// Generic fallback: try api.<provider>.com/v1
		return fmt.Sprintf("https://api.%s.com/v1", p)
	}
}

// gateCall makes a stateless OpenAI-compatible chat completion API call.
// It resolves the API key from the provider's credentials, constructs the
// request, and returns the assistant's text response.
func gateCall(ctx context.Context, apiKey, baseURL, model, systemPrompt, userContent string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("API key is empty")
	}
	if baseURL == "" {
		return "", fmt.Errorf("base URL is empty")
	}

	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userContent},
		},
		"max_tokens":  2000,
		"temperature": 0.1,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal gate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create gate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gate API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read gate response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("gate API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the response to extract the assistant's content.
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse gate response JSON: %w (raw: %s)", err, string(respBody))
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("gate API returned no choices")
	}

	content := strings.TrimSpace(result.Choices[0].Message.Content)
	return content, nil
}

// parseGateResponse extracts a gateResult from the LLM's text response,
// stripping markdown fences if present.
func parseGateResponse(text string) (gateResult, error) {
	// Strip markdown code fences if present.
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		// Find the closing fence.
		lines := strings.Split(text, "\n")
		var inner []string
		skipping := true
		for _, line := range lines {
			if skipping {
				if strings.HasPrefix(line, "```") {
					skipping = false
				}
				continue
			}
			inner = append(inner, line)
		}
		text = strings.Join(inner, "\n")
	}

	var result gateResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return gateResult{}, fmt.Errorf("failed to parse gate JSON: %w (text: %s)", err, text)
	}
	return result, nil
}

// parseTriageResponse extracts a gateTriageResult from the LLM's text response.
func parseTriageResponse(text string) (gateTriageResult, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		var inner []string
		skipping := true
		for _, line := range lines {
			if skipping {
				if strings.HasPrefix(line, "```") {
					skipping = false
				}
				continue
			}
			inner = append(inner, line)
		}
		text = strings.Join(inner, "\n")
	}

	var result gateTriageResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &result); err != nil {
		return gateTriageResult{}, fmt.Errorf("failed to parse triage JSON: %w (text: %s)", err, text)
	}
	return result, nil
}

// runAgentWorkflowLoop iterates over unchecked TODO items, processing each
// with a fresh agent context. Between items, the conversation is cleared.
func runAgentWorkflowLoop(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, cfg *AgentWorkflowConfig, state *workflowExecutionState) (bool, error) {
	if cfg == nil || cfg.Loop == nil {
		return false, nil
	}

	loop := cfg.Loop
	todoFile := loop.TodoFile

	// Read the gate system prompt.
	gatePrompt, err := resolveWorkflowTextOrFile("", loop.GatePromptFile, "gate_prompt")
	if err != nil {
		return false, fmt.Errorf("failed to resolve gate_prompt_file: %w", err)
	}

	// Resolve gate provider and model.
	gateProvider := strings.TrimSpace(loop.GateProvider)
	gateModel := strings.TrimSpace(loop.GateModel)
	if gateProvider == "" {
		if cfg.Initial != nil {
			gateProvider = strings.TrimSpace(cfg.Initial.Provider)
		}
		if gateProvider == "" {
			gateProvider = strings.TrimSpace(chatAgent.GetProvider())
		}
	}
	if gateModel == "" {
		if cfg.Initial != nil {
			gateModel = strings.TrimSpace(cfg.Initial.Model)
		}
		if gateModel == "" {
			gateModel = strings.TrimSpace(chatAgent.GetModel())
		}
	}

	// Resolve the API key for the gate provider.
	apiKey, err := credentials.ResolveProviderAPIKey(gateProvider, gateProvider)
	if err != nil {
		return false, fmt.Errorf("failed to resolve API key for gate provider %q: %w", gateProvider, err)
	}
	baseURL := providerBaseURL(gateProvider)

	itemsProcessed := 0
	itemsSkipped := 0
	itemsFailed := 0

	fmt.Println()
	console.GlyphAction.Printf("TODO loop: provider=%s model=%s gate=%s/%s todo=%s",
		chatAgent.GetProvider(), chatAgent.GetModel(), gateProvider, gateModel, todoFile)

	for {
		// Check for context cancellation.
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("loop cancelled: %w", err)
		}

		// Check budget exceeded.
		if chatAgent.FleetBudgetExceeded() {
			fmt.Println()
			console.GlyphWarning.Print("Budget exceeded — stopping TODO loop")
			break
		}

		// Step 1: Find next unchecked item.
		lineNum, sectionText, findErr := findNextTodoItem(todoFile)
		if findErr != nil {
			// No more items or read error.
			if strings.Contains(findErr.Error(), "no unchecked") {
				fmt.Println()
				console.GlyphSuccess.Printf("TODO loop complete: processed=%d skipped=%d failed=%d",
					itemsProcessed, itemsSkipped, itemsFailed)
				state.Complete = true
				return false, nil
			}
			return false, fmt.Errorf("failed to find next TODO item: %w", findErr)
		}

		fmt.Println()
		console.GlyphAction.Printf("TODO item at line %d", lineNum)

		// Step 2: Gate call — parse section into delegation prompt.
		gateText, gateErr := gateCall(ctx, apiKey, baseURL, gateModel, gatePrompt, sectionText)
		if gateErr != nil {
			console.GlyphWarning.Printf("Gate call failed: %v", gateErr)
			itemsFailed++
			continue
		}

		gateRes, parseErr := parseGateResponse(gateText)
		if parseErr != nil {
			console.GlyphWarning.Printf("Gate parse failed: %v", parseErr)
			itemsFailed++
			continue
		}

		console.GlyphInfo.Printf("Gate: title=%q skip=%v", gateRes.Title, gateRes.Skip)

		// Step 3: If skip, mark done and continue.
		if gateRes.Skip {
			reason := gateRes.SkipReason
			if reason == "" {
				reason = "no reason given"
			}
			console.GlyphInfo.Printf("Skipping: %s", reason)
			if markErr := markTodoDone(todoFile, lineNum); markErr != nil {
				console.GlyphWarning.Printf("Failed to mark item done: %v", markErr)
			}
			itemsSkipped++
			continue
		}

		if gateRes.Prompt == "" {
			console.GlyphWarning.Printf("Gate returned empty prompt, skipping item")
			itemsFailed++
			continue
		}

		// Step 4: Process the item with the agent.
		console.GlyphAction.Printf("Processing: %s", gateRes.Title)

		// Override max iterations for this item.
		prevMaxIter := chatAgent.GetMaxIterations()
		chatAgent.SetMaxIterations(loop.MaxIterations)

		// Run the agent with the gate-generated prompt.
		processErr := ProcessQuery(ctx, chatAgent, eventBus, gateRes.Prompt)

		// Restore max iterations.
		chatAgent.SetMaxIterations(prevMaxIter)

		if processErr != nil {
			console.GlyphWarning.Printf("Agent processing failed: %v", processErr)
		}

		// Step 5: Build verification.
		buildFailed := false
		if processErr == nil {
			buildCmd := strings.TrimSpace(loop.BuildCommand)
			if buildCmd != "" {
				console.GlyphShell.Printf("%s", buildCmd)
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				cmd := exec.CommandContext(ctx, shell, "-c", buildCmd)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if buildErr := cmd.Run(); buildErr != nil {
					console.GlyphWarning.Printf("Build failed: %v", buildErr)
					buildFailed = true
				} else {
					fmt.Println()
					console.GlyphSuccess.Print("Build passed")
				}
			}
		} else {
			buildFailed = true
		}

		// Step 6: Triage on failure — retry up to MaxRetries.
		retries := 0
		for buildFailed && retries < loop.MaxRetries {
			retries++
			fmt.Println()
			console.GlyphAction.Printf("Build failed — triaging (attempt %d/%d)", retries, loop.MaxRetries)

			triagePrompt := fmt.Sprintf(
				"Task: %s\n\nPrevious attempt failed. Decide whether to retry or skip.\nReturn JSON: {\"action\": \"retry\"|\"skip\", \"reason\": \"...\"}",
				gateRes.Title)

			triageText, triageErr := gateCall(ctx, apiKey, baseURL, gateModel,
				"You are a build error triage agent. Given a task title and context, decide: retry (transient/fixable) or skip (fundamental/blocking). Return ONLY JSON: {\"action\": \"retry\"|\"skip\", \"reason\": \"...\"}",
				triagePrompt)
			if triageErr != nil {
				console.GlyphWarning.Printf("Triage gate call failed: %v — defaulting to retry", triageErr)
				triageText = `{"action": "retry", "reason": "triage failed"}`
			}

			triageRes, parseErr := parseTriageResponse(triageText)
			if parseErr != nil {
				console.GlyphWarning.Printf("Triage parse failed: %v — defaulting to retry", parseErr)
				triageRes = gateTriageResult{Action: "retry", Reason: "parse failed"}
			}

			console.GlyphInfo.Printf("Triage: action=%s reason=%s", triageRes.Action, triageRes.Reason)

			if strings.EqualFold(triageRes.Action, "skip") {
				buildFailed = false
				itemsSkipped++
				break
			}

			// Retry: re-run with error context.
			retryPrompt := fmt.Sprintf(
				"Previous attempt failed. Fix the issue and ensure the build passes.\n\nOriginal task:\n%s",
				gateRes.Prompt)

			prevMaxIter := chatAgent.GetMaxIterations()
			retryMaxIter := loop.MaxIterations / 2
			if retryMaxIter < 5 {
				retryMaxIter = 5
			}
			chatAgent.SetMaxIterations(retryMaxIter)

			retryErr := ProcessQuery(ctx, chatAgent, eventBus, retryPrompt)
			chatAgent.SetMaxIterations(prevMaxIter)

			if retryErr != nil {
				console.GlyphWarning.Printf("Retry agent processing failed: %v", retryErr)
			}

			// Re-check build.
			buildCmd := strings.TrimSpace(loop.BuildCommand)
			if buildCmd != "" {
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				cmd := exec.CommandContext(ctx, shell, "-c", buildCmd)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if buildErr := cmd.Run(); buildErr != nil {
					console.GlyphWarning.Printf("Build still fails after retry: %v", buildErr)
				} else {
					buildFailed = false
					fmt.Println()
					console.GlyphSuccess.Print("Build passed after retry")
				}
			}
		}

		if buildFailed {
			itemsFailed++
			console.GlyphWarning.Printf("Item failed after retries: %s", gateRes.Title)
		} else if processErr == nil && !gateRes.Skip {
			// Mark done on success.
			if markErr := markTodoDone(todoFile, lineNum); markErr != nil {
				console.GlyphWarning.Printf("Failed to mark item done: %v", markErr)
			} else {
				itemsProcessed++
				fmt.Println()
				console.GlyphSuccess.Printf("Item complete: %s", gateRes.Title)
			}
		}

		// Step 7: CRITICAL — clear conversation between items.
		chatAgent.ClearConversationHistory()
	}

	state.Complete = true
	return false, nil
}
