package git

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// CommitFileChange describes a staged file with git status code.
type CommitFileChange struct {
	Status string
	Path   string
}

// multiFileRules teaches synthesis over per-file narration. When many
// files change at once, models tend to produce laundry-list messages;
// these rules push toward intent-level summaries.
const multiFileRules = `When multiple files change, synthesize the overall intent rather than describing each file. Focus on WHAT changed and WHY, not which files were touched. Avoid vague words like "updated" or "modified" — be specific about the actual change.`

// CommitMessageOptions configures commit message generation behavior.
type CommitMessageOptions struct {
	Diff             string
	Branch           string
	FileChanges      []CommitFileChange
	UserInstructions string
}

// CommitMessageResult contains generated message and diagnostics.
type CommitMessageResult struct {
	Message      string
	ApproxTokens int
	Warnings     []string
}

// GenerateCommitMessageFromStagedDiff generates commit text using the same two-pass
// title+description algorithm used by /commit.
func GenerateCommitMessageFromStagedDiff(client api.ClientInterface, opts CommitMessageOptions) (*CommitMessageResult, error) {
	if client == nil {
		return nil, fmt.Errorf("client is required")
	}
	diffText := strings.TrimSpace(opts.Diff)
	if diffText == "" {
		return nil, fmt.Errorf("staged diff is empty")
	}

	// Get timeout from agent config, default to 5 minutes
	timeoutSec := 300 // Default 5 minutes
	if agent, ok := client.(interface{ GetConfig() *configuration.Config }); ok {
		if cfg := agent.GetConfig(); cfg != nil && cfg.APITimeouts != nil && cfg.APITimeouts.CommitMessageTimeoutSec > 0 {
			timeoutSec = cfg.APITimeouts.CommitMessageTimeoutSec
		}
	}

	primaryAction := "Updates"
	actionCounts := make(map[string]int)
	for _, change := range opts.FileChanges {
		action := actionFromStatus(change.Status)
		actionCounts[action]++
	}
	// Use a specific action only when all files share the same change type.
	if len(actionCounts) == 1 {
		for action := range actionCounts {
			primaryAction = action
			break
		}
	}

	// Budgeted diff presentation: semantic diffs first under a byte cap,
	// non-semantic files as one-line summaries (see diff_presentation.go).
	budgetedDiff, budgetWarnings := prepareBudgetedDiff(opts.FileChanges, diffText, maxCommitDiffBytes)

	promptContent := budgetedDiff
	if strings.TrimSpace(opts.UserInstructions) != "" {
		promptContent = fmt.Sprintf("USER INSTRUCTIONS:\n%s\n\nCODE CHANGES:\n%s", strings.TrimSpace(opts.UserInstructions), promptContent)
	}

	// Diff notation guide, shared by both generation passes. Small models
	// reliably misread unified diffs without it (e.g. treating -lines as
	// deleted content that still exists), and the worked example anchors
	// the expected output format.
	diffGuide := `Diff notation guide:
- Each file section begins with a context header: → path (action, type): +N -M
- Lines starting with + are additions, - are deletions, unmarked lines are unchanged context
- Lines beginning with @@ are hunk headers showing the function or line range that changed
- If the same line appears as both + and -, it was modified
- Files may be tagged as source, dependency lock file, binary, minified build output, or auto-generated

Example diff:
-------
→ src/App.tsx (modified, source): +2 -3
diff --git a/src/App.tsx b/src/App.tsx
@@ -17,6 +17,8 @@ import {Platform } from 'react-native';
 const InnerApp = () => {
-  console.log('hello World')
   const applicationConfig = platformAppConfig(appConfig);
+  const edgeInsets = useSafeAreaInsets();
+  return applicationConfig;
--------
`

	titleSystemPrompt := `You are a git commit message generator. ALWAYS generate titles following the Conventional Commits format.

FORMAT: <type>(<optional scope>): <imperative description>

Types: feat, fix, docs, style, refactor, perf, test, chore, ci

Rules:
- Use imperative mood: "add feature" not "added feature"
- No period at end
- Max 72 characters total
- Infer scope from file paths (e.g., "api", "auth", "ui", "config")
- Lowercase type and description
- Return PLAIN TEXT ONLY — no markdown, no code blocks, no explanation`

	titlePrompt := fmt.Sprintf(`Based on the changes below, generate a conventional commit title in format: type(scope): description

The changes primarily %s the following files.

%s

%s

Staged changes:
\"\"\"
%s
\"\"\"

Return ONLY the commit title. No markdown, no code blocks, no explanation.`, primaryAction, diffGuide, multiFileRules, promptContent)

	titleMessages := []api.Message{
		{
			Role:    "system",
			Content: titleSystemPrompt,
		},
		{
			Role:    "user",
			Content: titlePrompt,
		},
	}
	descSystemPrompt := `You are a git commit message description generator. Generate clear, concise commit bodies.

RULES:
1. Max 500 characters total
2. Plain text only — NO markdown formatting, NO code blocks, NO lists
3. SINGLE paragraph
4. Explain WHAT and WHY, not how
5. Be concise — omit details obvious from the diff`

	descPrompt := fmt.Sprintf(`Based on the changes below, generate a brief commit description body.

%s

%s

Staged changes:
\"\"\"
%s
\"\"\"

Return ONLY the description paragraph. No title, no markdown, no code blocks, no explanation.`, diffGuide, multiFileRules, promptContent)

	descMessages := []api.Message{
		{
			Role:    "system",
			Content: descSystemPrompt,
		},
		{
			Role:    "user",
			Content: descPrompt,
		},
	}

	var (
		titleResp *api.ChatResponse
		descResp  *api.ChatResponse
		titleErr  error
		descErr   error
	)

	type callResult struct {
		resp *api.ChatResponse
		err  error
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	titleChan := make(chan callResult, 1)
	descChan := make(chan callResult, 1)

	// The local provider serializes generation internally — one GPU, one
	// model mutex (Model.Generate) — so dispatching both calls concurrently
	// buys no real parallelism: the second call just queues up and starts
	// its own heavy prefill+decode the instant the first releases the lock,
	// back-to-back with no gap for the allocator to reclaim anything in
	// between. On a memory-constrained machine that measurably worsens the
	// second call's decode throughput. Cloud providers get genuine
	// parallelism from concurrent HTTP requests, so only the local provider
	// is serialized here.
	isLocalProvider := client.GetProvider() == string(api.SproutLocalClientType)

	go func() {
		r, e := client.SendChatRequest(ctx, titleMessages, nil, "", false)
		titleChan <- callResult{r, e}
		if isLocalProvider {
			r, e := client.SendChatRequest(ctx, descMessages, nil, "", false)
			descChan <- callResult{r, e}
		}
	}()

	if !isLocalProvider {
		go func() {
			r, e := client.SendChatRequest(ctx, descMessages, nil, "", false)
			descChan <- callResult{r, e}
		}()
	}

	select {
	case result := <-titleChan:
		titleResp, titleErr = result.resp, result.err
	case <-ctx.Done():
		return nil, fmt.Errorf("LLM request timed out after %ds", timeoutSec)
	}

	select {
	case result := <-descChan:
		descResp, descErr = result.resp, result.err
	case <-ctx.Done():
		return nil, fmt.Errorf("LLM request timed out after %ds", timeoutSec)
	}

	if titleErr != nil {
		return nil, fmt.Errorf("failed to generate commit title: %w", titleErr)
	}
	if descErr != nil {
		return nil, fmt.Errorf("failed to generate commit description: %w", descErr)
	}
	if len(titleResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from model for commit title")
	}
	if len(descResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from model for commit description")
	}

	shortTitle := NormalizeShortTitle(titleResp.Choices[0].Message.Content)
	if strings.TrimSpace(shortTitle) == "" {
		// LLM returned empty title — generate one from file changes
		shortTitle = generateFallbackCommitMessage([]CommitFileChange{})
	}
	description := strings.TrimSpace(descResp.Choices[0].Message.Content)
	wrappedDesc := WrapText(description, 72)
	commitMessage := strings.TrimSpace(shortTitle + "\n\n" + wrappedDesc)
	if commitMessage == "" {
		return nil, fmt.Errorf("generated commit message was empty")
	}

	approx := 0
	approx += titleResp.Usage.TotalTokens
	approx += descResp.Usage.TotalTokens

	return &CommitMessageResult{
		Message:      commitMessage,
		ApproxTokens: approx,
		Warnings:     budgetWarnings,
	}, nil
}

func actionFromStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "A":
		return "Adds"
	case "D":
		return "Deletes"
	case "R":
		return "Renames"
	default:
		return "Updates"
	}
}

func isDefaultBranch(branch string) bool {
	b := strings.TrimSpace(branch)
	return b == "master" || b == "main" || b == "develop" || b == "dev"
}

func NormalizeShortTitle(raw string) string {
	title := strings.TrimSpace(raw)
	if strings.Contains(title, "\n") {
		title = strings.TrimSpace(strings.SplitN(title, "\n", 2)[0])
	}
	title = strings.Trim(title, "`")
	title = strings.TrimSpace(strings.TrimPrefix(title, "title:"))
	title = strings.TrimSpace(strings.TrimPrefix(title, "Title:"))
	return title
}

func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	if max <= 3 {
		return string([]rune(s)[:max])
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max-3])) + "..."
}

func WrapText(text string, lineLength int) string {
	if text == "" {
		return ""
	}

	paragraphs := strings.Split(text, "\n\n")
	var wrappedParagraphs []string

	for _, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			wrappedParagraphs = append(wrappedParagraphs, "")
			continue
		}

		words := strings.Fields(paragraph)
		if len(words) == 0 {
			wrappedParagraphs = append(wrappedParagraphs, "")
			continue
		}

		var lines []string
		currentLine := words[0]
		for i := 1; i < len(words); i++ {
			word := words[i]
			if len(currentLine)+1+len(word) <= lineLength {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			lines = append(lines, currentLine)
		}

		wrappedParagraphs = append(wrappedParagraphs, strings.Join(lines, "\n"))
	}

	return strings.Join(wrappedParagraphs, "\n\n")
}
