package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/utils"
)

// CommitFileChange describes a staged file with git status code.
type CommitFileChange struct {
	Status string
	Path   string
}

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
		return nil, errors.New("client is required")
	}
	diffText := strings.TrimSpace(opts.Diff)
	if diffText == "" {
		return nil, errors.New("staged diff is empty")
	}

	primaryAction := "Updates"
	actionCounts := make(map[string]int)
	fileActions := make([]string, 0, len(opts.FileChanges))
	for _, change := range opts.FileChanges {
		action := actionFromStatus(change.Status)
		actionCounts[action]++
		if strings.TrimSpace(change.Path) != "" {
			fileActions = append(fileActions, fmt.Sprintf("%s %s", action, change.Path))
		}
	}
	// Use a specific action only when all files share the same change type.
	// For mixed change types (adds + updates + deletes), fall back to "Updates".
	if len(actionCounts) == 1 {
		for action := range actionCounts {
			primaryAction = action
			break
		}
	}
	fileActionsSummary := fmt.Sprintf("%s %d files", primaryAction, len(opts.FileChanges))
	if len(fileActions) == 1 {
		fileActionsSummary = fileActions[0]
	}

	branchPrefix := ""
	if !isDefaultBranch(opts.Branch) && strings.TrimSpace(opts.Branch) != "" {
		branchPrefix = fmt.Sprintf("[%s] ", strings.TrimSpace(opts.Branch))
	}
	prefixAndActions := branchPrefix + fileActionsSummary + " - "
	availableSpace := 72

	optimizer := utils.NewDiffOptimizer()
	optimizedDiff := optimizer.OptimizeDiff(diffText)

	var contextInfo string
	if len(optimizedDiff.FileSummaries) > 0 {
		contextInfo = "\n\nFile summaries for optimized content:\n"
		for file, summary := range optimizedDiff.FileSummaries {
			contextInfo += fmt.Sprintf("- %s: %s\n", file, summary)
		}
	}
	promptContent := fmt.Sprintf("%s%s", optimizedDiff.OptimizedContent, contextInfo)
	if strings.TrimSpace(opts.UserInstructions) != "" {
		promptContent = fmt.Sprintf("USER INSTRUCTIONS:\n%s\n\nCODE CHANGES:\n%s", strings.TrimSpace(opts.UserInstructions), promptContent)
	}

	titlePrompt := fmt.Sprintf(`Base responses on the following changes:

%s

Generate a concise git commit title starting with the word: '%s'.
The total length MUST be under %d characters. Don't include the file name or any
colons. The title should be a single line without any markdown formatting. Only
return the short title and nothing else.

CRITICAL: Do NOT use markdown code blocks. Return plain text only.`, promptContent, primaryAction, availableSpace)

	titleMessages := []api.Message{
		{
			Role:    "system",
			Content: "You are a git commit message generator. Generate concise, clear commit messages following conventional commit standards.",
		},
		{
			Role:    "user",
			Content: titlePrompt,
		},
	}
	descPrompt := fmt.Sprintf(`Base responses on the following changes:

%s

Generate a Git commit message summary. The message should follow these rules:
1. The total length MUST be under 500 characters.
2. DO NOT include a title.
3. DO NOT include any code blocks or filenames.
4. DO NOT include any markdown formatting. 
5. DO NOT include any ordered lists or unordered lists.
6. Message will be a SINGLE paragraph without any markdown formatting.
7. The message should be clear and concise and only give reasoning for the change if provided by the user.`, promptContent)

	descMessages := []api.Message{
		{
			Role: "system",
			Content: `
					You are a git commit message generator. Generate clear, concise descriptions that follow these immutable rules.
					RULES:
					1. The total length MUST be under 500 characters.
					2. DO NOT include a title.
					3. DO NOT include any code blocks or filenames.
					4. DO NOT include any markdown formatting. 
					5. DO NOT include any ordered lists or unordered lists.
					6. Message will be a SINGLE paragraph without any markdown formatting.
					7. The message should be clear and concise and only give reasoning for the change if provided by the user.
					`,
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	titleChan := make(chan callResult, 1)
	descChan := make(chan callResult, 1)

	go func() {
		r, e := client.SendChatRequest(titleMessages, nil, "")
		titleChan <- callResult{r, e}
	}()

	go func() {
		r, e := client.SendChatRequest(descMessages, nil, "")
		descChan <- callResult{r, e}
	}()

	select {
	case result := <-titleChan:
		titleResp, titleErr = result.resp, result.err
	case <-ctx.Done():
		return nil, errors.New("LLM request timed out after 60s")
	}

	select {
	case result := <-descChan:
		descResp, descErr = result.resp, result.err
	case <-ctx.Done():
		return nil, errors.New("LLM request timed out after 60s")
	}

	if titleErr != nil {
		return nil, fmt.Errorf("failed to generate commit title: %w", titleErr)
	}
	if descErr != nil {
		return nil, fmt.Errorf("failed to generate commit description: %w", descErr)
	}
	if len(titleResp.Choices) == 0 {
		return nil, errors.New("no response from model for commit title")
	}
	if len(descResp.Choices) == 0 {
		return nil, errors.New("no response from model for commit description")
	}

	shortTitle := NormalizeShortTitle(titleResp.Choices[0].Message.Content)
	shortTitle = TruncateRunes(shortTitle, availableSpace)
	description := strings.TrimSpace(descResp.Choices[0].Message.Content)
	wrappedDesc := WrapText(description, 72)
	commitMessage := strings.TrimSpace(prefixAndActions + shortTitle + "\n\n" + wrappedDesc)
	if commitMessage == "" {
		return nil, errors.New("generated commit message was empty")
	}

	approx := 0
	approx += titleResp.Usage.TotalTokens
	approx += descResp.Usage.TotalTokens

	return &CommitMessageResult{
		Message:      commitMessage,
		ApproxTokens: approx,
		Warnings:     append([]string(nil), optimizedDiff.Warnings...),
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
