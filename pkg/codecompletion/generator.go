//go:build !js

package codecompletion

import (
	"context"
	"fmt"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

const (
	completionTimeout = 30 * time.Second
	defaultMaxTokens  = 128
)

const completionSystemPrompt = `You are a code completion engine. Complete the code at the cursor position marked by <CURSOR>. Return ONLY the code to insert at the cursor. No explanation, no markdown code fences, no backticks. Match the surrounding code style and indentation.`

// CompletionRequest holds the context for a code completion request.
type CompletionRequest struct {
	Prefix    string // Code before the cursor
	Suffix    string // Code after the cursor
	Language  string // Language ID (e.g., "go", "typescript")
	FilePath  string // File being edited
	MaxTokens int    // Max tokens to generate (default 128 if 0)
}

// CompletionResult contains the generated completion.
type CompletionResult struct {
	Text       string
	TokensUsed int
}

// GenerateCompletion generates a code completion using chat-formatted FIM.
func GenerateCompletion(client api.ClientInterface, req CompletionRequest) (*CompletionResult, error) {
	if client == nil {
		return nil, fmt.Errorf("client is required")
	}
	if strings.TrimSpace(req.Prefix) == "" {
		return nil, fmt.Errorf("prefix is required")
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if h, ok := client.(providers.MaxTokensHinter); ok {
		h.SetMaxTokensHint(maxTokens)
	}

	messages := []api.Message{
		{Role: "system", Content: completionSystemPrompt},
		{Role: "user", Content: buildUserPrompt(req)},
	}

	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()

	resp, err := client.SendChatRequest(ctx, messages, nil, "", false)
	if err != nil {
		return nil, fmt.Errorf("generating completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from model for completion")
	}

	text := cleanCompletion(resp.Choices[0].Message.Content)

	return &CompletionResult{
		Text:       text,
		TokensUsed: resp.Usage.TotalTokens,
	}, nil
}

func buildUserPrompt(req CompletionRequest) string {
	lang := strings.TrimSpace(req.Language)
	langPrefix := ""
	if lang != "" {
		langPrefix = lang + " "
	}

	if strings.TrimSpace(req.Suffix) == "" {
		return fmt.Sprintf("Complete the following %scode. Return ONLY the continuation, no explanation:\n\n%s", langPrefix, req.Prefix)
	}

	filePath := strings.TrimSpace(req.FilePath)
	return fmt.Sprintf("Complete the code at <CURSOR> in this %sfile (%s):\n\n%s<CURSOR>%s\n\nReturn ONLY the insertion text.", langPrefix, filePath, req.Prefix, req.Suffix)
}

// cleanCompletion strips markdown code fences and explanation prefixes from a
// model response. If the text contains a markdown fence (```) anywhere — not
// just at the start — the content between the first fence line (language tag
// skipped) and the last closing fence is returned. Otherwise the trimmed text
// is returned as-is. An unclosed fence (e.g. ```go\ncode) still yields the
// code after the fence line.
func cleanCompletion(raw string) string {
	text := strings.TrimSpace(raw)
	fenceStart := strings.Index(text, "```")
	if fenceStart < 0 {
		return text
	}
	// Skip the opening fence line, including any language tag (e.g. ```go).
	afterFence := text[fenceStart+3:]
	nl := strings.Index(afterFence, "\n")
	if nl < 0 {
		return ""
	}
	text = afterFence[nl+1:]
	if end := strings.LastIndex(text, "```"); end >= 0 {
		text = text[:end]
	}
	return strings.TrimSpace(text)
}
