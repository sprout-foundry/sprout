//go:build !js

package codecompletion

import (
	"context"
	"fmt"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCompletionClient implements api.ClientInterface for tests.
type mockCompletionClient struct {
	response *api.ChatResponse
	err      error
	messages []api.Message
}

func (m *mockCompletionClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	m.messages = append(m.messages, messages...)
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return &api.ChatResponse{Choices: []api.Choice{}}, nil
}

func (m *mockCompletionClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockCompletionClient) CheckConnection() error             { return nil }
func (m *mockCompletionClient) SetDebug(bool)                      {}
func (m *mockCompletionClient) SetModel(string) error              { return nil }
func (m *mockCompletionClient) GetModel() string                   { return "mock" }
func (m *mockCompletionClient) GetProvider() string                { return "mock" }
func (m *mockCompletionClient) GetModelContextLimit() (int, error) { return 4096, nil }
func (m *mockCompletionClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (m *mockCompletionClient) SupportsVision() bool               { return false }
func (m *mockCompletionClient) SupportsConversationalVision() bool { return false }
func (m *mockCompletionClient) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilitiesDefault()
}
func (m *mockCompletionClient) GetVisionModel() string { return "" }
func (m *mockCompletionClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockCompletionClient) GetLastTPS() float64             { return 0 }
func (m *mockCompletionClient) GetAverageTPS() float64          { return 0 }
func (m *mockCompletionClient) GetTPSStats() map[string]float64 { return nil }
func (m *mockCompletionClient) ResetTPSStats()                  {}

func completionResponse(text string, totalTokens int) *api.ChatResponse {
	return &api.ChatResponse{
		Choices: []api.Choice{{Message: api.Message{Role: "assistant", Content: text}}},
		Usage:   api.ChatUsage{TotalTokens: totalTokens},
	}
}

// hintRecordingClient records max-token hints passed through MaxTokensHinter.
type hintRecordingClient struct {
	*mockCompletionClient
	hint int
}

func (c *hintRecordingClient) SetMaxTokensHint(tokens int) { c.hint = tokens }

func TestGenerateCompletion_HappyPath(t *testing.T) {
	client := &mockCompletionClient{response: completionResponse("fmt.Println(\"hi\")", 12)}
	result, err := GenerateCompletion(client, CompletionRequest{
		Prefix:   "package main\n\nfunc main() {\n\t",
		Suffix:   "\n}",
		Language: "go",
		FilePath: "main.go",
	})
	require.NoError(t, err)
	assert.Equal(t, "fmt.Println(\"hi\")", result.Text)
	assert.Equal(t, 12, result.TokensUsed)
}

func TestGenerateCompletion_NilClient(t *testing.T) {
	_, err := GenerateCompletion(nil, CompletionRequest{Prefix: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client is required")
}

func TestGenerateCompletion_EmptyPrefix(t *testing.T) {
	client := &mockCompletionClient{}
	_, err := GenerateCompletion(client, CompletionRequest{Prefix: "   "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prefix is required")
}

func TestGenerateCompletion_ClientError(t *testing.T) {
	client := &mockCompletionClient{err: fmt.Errorf("boom")}
	_, err := GenerateCompletion(client, CompletionRequest{Prefix: "package main\n"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generating completion")
}

func TestGenerateCompletion_EmptyChoices(t *testing.T) {
	client := &mockCompletionClient{}
	_, err := GenerateCompletion(client, CompletionRequest{Prefix: "package main\n"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no response from model for completion")
}

func TestGenerateCompletion_EmptyTextReturnsEmptyResult(t *testing.T) {
	client := &mockCompletionClient{response: completionResponse("   ", 7)}
	result, err := GenerateCompletion(client, CompletionRequest{Prefix: "package main\n"})
	require.NoError(t, err)
	assert.Equal(t, "", result.Text)
	assert.Equal(t, 7, result.TokensUsed)
}

func TestGenerateCompletion_StripsCodeFences(t *testing.T) {
	client := &mockCompletionClient{response: completionResponse("```go\nfmt.Println(\"hi\")\n```", 5)}
	result, err := GenerateCompletion(client, CompletionRequest{Prefix: "package main\n"})
	require.NoError(t, err)
	assert.Equal(t, "fmt.Println(\"hi\")", result.Text)
}

func TestGenerateCompletion_EmptySuffixUsesSimplePrompt(t *testing.T) {
	client := &mockCompletionClient{response: completionResponse("fmt.Println(\"hi\")", 5)}
	_, err := GenerateCompletion(client, CompletionRequest{
		Prefix:   "package main\n\nfunc main() {\n\t",
		Suffix:   "",
		Language: "go",
	})
	require.NoError(t, err)

	require.Len(t, client.messages, 2)
	userPrompt := client.messages[1].Content
	assert.Contains(t, userPrompt, "Complete the following go code.")
	assert.NotContains(t, userPrompt, "<CURSOR>")
}

func TestGenerateCompletion_ShortSuffixUsesFIMPrompt(t *testing.T) {
	client := &mockCompletionClient{response: completionResponse("fmt.Println(\"hi\")", 5)}
	_, err := GenerateCompletion(client, CompletionRequest{
		Prefix:   "package main\n\nfunc main() {\n\t",
		Suffix:   "\n}",
		Language: "go",
	})
	require.NoError(t, err)

	require.Len(t, client.messages, 2)
	userPrompt := client.messages[1].Content
	assert.Contains(t, userPrompt, "<CURSOR>")
	assert.Contains(t, userPrompt, "}")
}

func TestGenerateCompletion_LongSuffixUsesFIMPrompt(t *testing.T) {
	client := &mockCompletionClient{response: completionResponse("fmt.Println(\"hi\")", 5)}
	_, err := GenerateCompletion(client, CompletionRequest{
		Prefix:   "package main\n\nfunc main() {\n\t",
		Suffix:   "\n\tdefer close()\n}",
		Language: "go",
		FilePath: "main.go",
	})
	require.NoError(t, err)

	require.Len(t, client.messages, 2)
	userPrompt := client.messages[1].Content
	assert.Contains(t, userPrompt, "<CURSOR>")
	assert.Contains(t, userPrompt, "main.go")
	assert.Contains(t, userPrompt, "defer close()")
}

func TestGenerateCompletion_MaxTokensHint(t *testing.T) {
	client := &hintRecordingClient{mockCompletionClient: &mockCompletionClient{response: completionResponse("x", 1)}}
	_, err := GenerateCompletion(client, CompletionRequest{Prefix: "package main\n", MaxTokens: 128})
	require.NoError(t, err)
	assert.Equal(t, 128, client.hint)
}

func TestGenerateCompletion_DefaultMaxTokens(t *testing.T) {
	client := &hintRecordingClient{mockCompletionClient: &mockCompletionClient{response: completionResponse("x", 1)}}
	_, err := GenerateCompletion(client, CompletionRequest{Prefix: "package main\n"})
	require.NoError(t, err)
	assert.Equal(t, 128, client.hint)
}

func TestCleanCompletion_StripsFences(t *testing.T) {
	got := cleanCompletion("```go\nfmt.Println(\"hi\")\n```")
	assert.Equal(t, "fmt.Println(\"hi\")", got)
}

func TestCleanCompletion_NoFences(t *testing.T) {
	got := cleanCompletion("  fmt.Println(\"hi\")  ")
	assert.Equal(t, "fmt.Println(\"hi\")", got)
}

func TestCleanCompletion_UnclosedFence(t *testing.T) {
	got := cleanCompletion("```go\nfmt.Println(\"hi\")")
	assert.Equal(t, "fmt.Println(\"hi\")", got)
}

func TestCleanCompletion_FenceWithExplanationPrefix(t *testing.T) {
	got := cleanCompletion("Here is the completed code:\n```go\nfmt.Println(\"hi\")\n```")
	assert.Equal(t, "fmt.Println(\"hi\")", got)
}

func TestCleanCompletion_FenceWithoutLanguageTag(t *testing.T) {
	got := cleanCompletion("```\nfmt.Println(\"hi\")\n```")
	assert.Equal(t, "fmt.Println(\"hi\")", got)
}

func TestCleanCompletion_OnlyFenceDelimiters(t *testing.T) {
	got := cleanCompletion("```")
	assert.Equal(t, "", got)
}

func TestCleanCompletion_PlainTextKeepsBackticks(t *testing.T) {
	got := cleanCompletion("fmt.Println(\"`hi`\")")
	assert.Equal(t, "fmt.Println(\"`hi`\")", got)
}
