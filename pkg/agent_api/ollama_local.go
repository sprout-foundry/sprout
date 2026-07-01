package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// localOllamaListResponse is the JSON shape returned by Ollama's GET /api/tags.
type localOllamaListResponse struct {
	Models []localOllamaListModel `json:"models"`
}

// localOllamaListModel describes one entry in the local model list.
type localOllamaListModel struct {
	Name string `json:"name"`
}

// localOllamaChatRequest mirrors the JSON body POSTed to /api/chat.
type localOllamaChatRequest struct {
	Model    string                 `json:"model"`
	Messages []localOllamaMessage   `json:"messages"`
	Options  map[string]any         `json:"options,omitempty"`
	Tools    []localOllamaTool      `json:"tools,omitempty"`
	Stream   *bool                  `json:"stream,omitempty"`
	Format   map[string]any         `json:"format,omitempty"`
	KeepAlive string                `json:"keep_alive,omitempty"`
}

// localOllamaMessage is one entry in a chat request or response.
type localOllamaMessage struct {
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Images    [][]byte               `json:"images,omitempty"`
	ToolCalls []localOllamaToolCall   `json:"tool_calls,omitempty"`
}

// localOllamaTool mirrors Ollama's tool schema.
type localOllamaTool struct {
	Type     string                  `json:"type"`
	Function localOllamaToolFunction `json:"function"`
}

// localOllamaToolFunction is the callable portion of a tool.
type localOllamaToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// localOllamaToolCall describes one tool invocation returned by the model.
type localOllamaToolCall struct {
	Function localOllamaToolCallFunction `json:"function"`
}

// localOllamaToolCallFunction carries the name + raw JSON arguments.
type localOllamaToolCallFunction struct {
	Index     int             `json:"index"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// localOllamaChatResponse is one NDJSON line of /api/chat output.
type localOllamaChatResponse struct {
	Model     string               `json:"model"`
	CreatedAt time.Time            `json:"created_at"`
	Message   localOllamaMessage   `json:"message"`
	Done      bool                 `json:"done"`
	DoneReason string              `json:"done_reason"`
	Metrics   localOllamaMetrics   `json:"metrics,omitempty"`
}

// localOllamaMetrics carries the model's reported token counts.
type localOllamaMetrics struct {
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}

// ollamaClient is the interface our OllamaLocalClient talks to.
type ollamaClient interface {
	List(ctx context.Context) (*localOllamaListResponse, error)
	Chat(ctx context.Context, req *localOllamaChatRequest, fn func(*localOllamaChatResponse) error) error
}

type ollamaClientFactory func() (ollamaClient, error)

// httpOllamaClient is a minimal net/http-backed implementation of ollamaClient.
// It replaces the upstream github.com/ollama/ollama/api client (which
// transitively pulls in 8 Dependabot-flagged CVEs).
type httpOllamaClient struct {
	baseURL string
	http    *http.Client
}

func newHTTPClientFromEnv() *httpOllamaClient {
	host := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if host == "" {
		host = "http://127.0.0.1:11434"
	}
	host = strings.TrimRight(host, "/")
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return &httpOllamaClient{
		baseURL: host,
		http:    &http.Client{},
	}
}

func newHTTPClientAt(baseURL string) *httpOllamaClient {
	return &httpOllamaClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
}

func (c *httpOllamaClient) List(ctx context.Context) (*localOllamaListResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("build list request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama list read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama list status %d: %s", resp.StatusCode, string(body))
	}

	var out localOllamaListResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("ollama list decode: %w", err)
	}
	return &out, nil
}

func (c *httpOllamaClient) Chat(ctx context.Context, req *localOllamaChatRequest, fn func(*localOllamaChatResponse) error) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama chat status %d: %s", resp.StatusCode, string(respBody))
	}

	streaming := req.Stream != nil && *req.Stream

	if streaming {
		return readChatNDJSON(resp.Body, fn)
	}

	var single localOllamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&single); err != nil {
		return fmt.Errorf("ollama chat decode: %w", err)
	}
	if fn != nil {
		if err := fn(&single); err != nil {
			return fmt.Errorf("chat response callback: %w", err)
		}
	}
	return nil
}

// readChatNDJSON consumes the newline-delimited JSON streaming body from
// Ollama, invoking fn for each parsed chunk.
func readChatNDJSON(r io.Reader, fn func(*localOllamaChatResponse) error) error {
	scanner := bufio.NewScanner(r)
	// Allow large lines (vision responses can exceed 64K).
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk localOllamaChatResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			return fmt.Errorf("ollama chat ndjson decode: %w", err)
		}
		if fn != nil {
			if err := fn(&chunk); err != nil {
				return fmt.Errorf("chat chunk callback: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("ollama chat read: %w", err)
	}
	return nil
}

// OllamaLocalClient handles local Ollama API requests
type OllamaLocalClient struct {
	*TPSBase
	model         string
	debug         bool
	clientFactory ollamaClientFactory
}

func isWSL() bool {
	if _, err := os.Stat("/proc/version"); err != nil {
		return false
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}

func getWindowsHostIP() string {
	if !isWSL() {
		return ""
	}

	cmd := exec.Command("ip", "route", "show")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "default") {
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "default" && i+2 < len(parts) {
					ip := parts[i+2]
					if net.ParseIP(ip) != nil {
						return ip
					}
				}
			}
		}
	}
	return ""
}

func init() {
	if hostIP := getWindowsHostIP(); hostIP != "" {
		os.Setenv("OLLAMA_HOST", "http://"+hostIP+":11434")
	}
}

func defaultOllamaClientFactory() (ollamaClient, error) {
	return newHTTPClientFromEnv(), nil
}

func ensureModelAvailable(ctx context.Context, client ollamaClient, model string) error {
	listResp, err := client.List(ctx)
	if err != nil {
		return agenterrors.NewNetwork("failed to list local models", err)
	}

	availableModels := make([]string, 0, len(listResp.Models))
	for _, m := range listResp.Models {
		availableModels = append(availableModels, m.Name)
		if m.Name == model {
			return nil
		}
	}

	return agenterrors.NewProviderError(fmt.Sprintf("model '%s' not found locally. Available models: %s", model, availableModels), nil, "ollama", "")
}

func newOllamaLocalClientWithFactory(model string, factory ollamaClientFactory) (*OllamaLocalClient, error) {
	if factory == nil {
		factory = defaultOllamaClientFactory
	}

	client, err := factory()
	if err != nil {
		return nil, agenterrors.NewConfig("could not create ollama client", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listResp, err := client.List(ctx)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to list local models", err)
	}

	if strings.TrimSpace(model) == "" {
		if len(listResp.Models) == 0 {
			return nil, errors.New("no models available locally. Please pull a model first using 'ollama pull <model>'")
		}
		model = listResp.Models[0].Name
	} else {
		availableModels := make([]string, 0, len(listResp.Models))
		for _, m := range listResp.Models {
			availableModels = append(availableModels, m.Name)
			if m.Name == model {
				return &OllamaLocalClient{
					TPSBase:       NewTPSBase(),
					model:         model,
					debug:         false,
					clientFactory: factory,
				}, nil
			}
		}

		if len(listResp.Models) > 0 {
			fmt.Fprintf(os.Stderr, "[WARN] Model '%s' not found locally. Available models: %v\n", model, availableModels)
			fmt.Fprintf(os.Stderr, "[~] Falling back to first available model: %s\n", listResp.Models[0].Name)
			model = listResp.Models[0].Name
		} else {
			return nil, agenterrors.NewProviderError(fmt.Sprintf("model %s not found locally and no other models available. Available models: %s", model, availableModels), nil, "ollama", "")
		}
	}

	return &OllamaLocalClient{
		TPSBase:       NewTPSBase(),
		model:         model,
		debug:         false,
		clientFactory: factory,
	}, nil
}

// NewOllamaLocalClient creates a new local Ollama client
func NewOllamaLocalClient(model string) (*OllamaLocalClient, error) {
	return newOllamaLocalClientWithFactory(model, nil)
}

func (c *OllamaLocalClient) newClient() (ollamaClient, error) {
	if c.clientFactory == nil {
		c.clientFactory = defaultOllamaClientFactory
	}
	return c.clientFactory()
}

func (c *OllamaLocalClient) buildChatRequest(messages []Message, tools []Tool, reasoning string, stream bool) (*localOllamaChatRequest, int) {
	ollamaMessages := make([]localOllamaMessage, 0, len(messages)+1)
	ollamaTools := convertToolsToOllamaTools(tools)

	// Optional: fold system content into first user message for templates that ignore system role
	if envutil.GetEnvSimple("OLLAMA_FOLD_SYSTEM") != "" {
		var systemParts []string
		injected := false
		for _, m := range messages {
			role := strings.ToLower(m.Role)
			if role == "system" {
				if t := strings.TrimSpace(m.Content); t != "" {
					systemParts = append(systemParts, t)
				}
				continue
			}
			if !injected && role == "user" && len(systemParts) > 0 {
				combined := "System:\n" + strings.Join(systemParts, "\n\n") + "\n\n" + m.Content
				ollamaMessages = append(ollamaMessages, localOllamaMessage{Role: m.Role, Content: combined})
				injected = true
				continue
			}
			ollamaMessages = append(ollamaMessages, localOllamaMessage{Role: m.Role, Content: m.Content})
		}
		if len(ollamaMessages) == 0 { // no user message existed
			for _, m := range messages {
				if strings.ToLower(m.Role) != "system" {
					ollamaMessages = append(ollamaMessages, localOllamaMessage{Role: m.Role, Content: m.Content})
				}
			}
		}
	} else {
		for _, msg := range messages {
			ollamaMsg := localOllamaMessage{Role: msg.Role, Content: msg.Content}
			if len(msg.Images) > 0 {
				ollamaImages := [][]byte{}
				for _, img := range msg.Images {
					if img.Base64 != "" {
						data, err := base64.StdEncoding.DecodeString(img.Base64)
						if err == nil {
							ollamaImages = append(ollamaImages, data)
						}
					}
				}
				ollamaMsg.Images = ollamaImages
			}
			ollamaMessages = append(ollamaMessages, ollamaMsg)
		}
	}

	totalTokens := EstimateInputTokens(messages, tools)

	contextLimit, _ := c.GetModelContextLimit()
	if contextLimit <= 0 {
		contextLimit = 32000
	}

	headroom := contextLimit / 10
	if headroom < 2048 {
		headroom = 2048
	}
	if headroom > 8192 {
		headroom = 8192
	}
	numCtx := totalTokens + headroom
	if numCtx > contextLimit {
		numCtx = contextLimit
	}
	if numCtx < totalTokens+1024 {
		numCtx = totalTokens + 1024
		if numCtx > contextLimit {
			numCtx = contextLimit
		}
	}

	numPredict, ok := CalculateOutputBudget(contextLimit, totalTokens)
	if !ok {
		numPredict = MinOutputTokens
	}
	maxPredict := getOllamaMaxPredictCap(contextLimit)
	if numPredict > maxPredict {
		numPredict = maxPredict
	}

	options := map[string]any{
		"num_ctx":     numCtx,
		"num_predict": numPredict,
		"stream":      stream,
	}

	if reasoning != "" && strings.Contains(strings.ToLower(c.model), "gpt-oss") {
		options["reasoning_effort"] = reasoning
	}

	req := &localOllamaChatRequest{
		Model:    c.model,
		Messages: ollamaMessages,
		Options:  options,
	}

	if len(ollamaTools) > 0 {
		req.Tools = ollamaTools
	}
	req.Stream = &stream

	return req, totalTokens
}

func getOllamaMaxPredictCap(contextLimit int) int {
	cap := 8192
	raw := strings.TrimSpace(envutil.GetEnvSimple("OLLAMA_MAX_PREDICT"))
	if raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			cap = parsed
		}
	}

	maxByContext := contextLimit / 2
	if maxByContext < 1024 {
		maxByContext = 1024
	}
	if cap > maxByContext {
		cap = maxByContext
	}
	return cap
}

// SendChatRequest sends a chat request to local Ollama
func (c *OllamaLocalClient) SendChatRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	client, err := c.newClient()
	if err != nil {
		return nil, agenterrors.NewConfig("could not create ollama client", err)
	}

	req, totalTokens := c.buildChatRequest(messages, tools, reasoning, false)

	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	var responseContent strings.Builder
	var toolCalls []ToolCall
	var lastDoneReason string
	var lastMetrics localOllamaMetrics
	respFunc := func(res *localOllamaChatResponse) error {
		if len(res.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, convertOllamaToolCalls(res.Message.ToolCalls)...)
		} else if trimmed := strings.TrimSpace(res.Message.Content); trimmed != "" {
			responseContent.WriteString(res.Message.Content)
		}

		if res.DoneReason != "" {
			lastDoneReason = res.DoneReason
		}

		lastMetrics = res.Metrics

		return nil
	}

	startTime := time.Now()

	err = client.Chat(ctx, req, respFunc)
	if err != nil {
		return nil, agenterrors.NewProviderError("ollama chat failed", err, "ollama", c.model)
	}

	duration := time.Since(startTime)

	finishReason := lastDoneReason
	if finishReason == "" {
		finishReason = "stop"
	}

	response := &ChatResponse{
		ID:      "ollama-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   c.model,
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    "assistant",
				Content: responseContent.String(),
			},
			FinishReason: finishReason,
		}},
	}

	promptTokens := totalTokens
	if lastMetrics.PromptEvalCount > 0 {
		promptTokens = lastMetrics.PromptEvalCount
	}

	completionTokens := EstimateTokens(responseContent.String())
	if lastMetrics.EvalCount > 0 {
		completionTokens = lastMetrics.EvalCount
	}

	response.Usage.PromptTokens = promptTokens
	response.Usage.CompletionTokens = completionTokens
	response.Usage.TotalTokens = promptTokens + completionTokens
	response.Usage.EstimatedCost = 0

	if len(toolCalls) > 0 {
		response.Choices[0].Message.ToolCalls = toolCalls
	}

	if c.GetTracker() != nil && completionTokens > 0 {
		c.GetTracker().RecordRequest(duration, completionTokens)
	}

	return response, nil
}

// SetDebug enables or disables debug mode
func (c *OllamaLocalClient) SetDebug(debug bool) {
	c.debug = debug
}

// GetModel returns the current model
func (c *OllamaLocalClient) GetModel() string {
	return c.model
}

// GetProvider returns the provider name
func (c *OllamaLocalClient) GetProvider() string {
	return "ollama-local"
}

// CheckConnection verifies local Ollama is accessible
func (c *OllamaLocalClient) CheckConnection() error {
	client, err := c.newClient()
	if err != nil {
		return agenterrors.NewConfig("could not create ollama client", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.List(ctx)
	if err != nil {
		return agenterrors.NewNetwork("ollama connection check failed", err)
	}
	return nil
}

// GetModelContextLimit returns the context limit for the model
func (c *OllamaLocalClient) GetModelContextLimit() (int, error) {
	if strings.Contains(c.model, "qwen3-coder") || strings.Contains(c.model, "gpt-oss") {
		return 128000, nil
	}
	return 32000, nil
}

// SetModel updates the active model after validating it exists locally
func (c *OllamaLocalClient) SetModel(model string) error {
	if strings.TrimSpace(model) == "" {
		return errors.New("model name cannot be empty")
	}

	if model == c.model {
		return nil
	}

	client, err := c.newClient()
	if err != nil {
		return agenterrors.NewConfig("could not create ollama client", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listResp, err := client.List(ctx)
	if err != nil {
		return agenterrors.NewNetwork("failed to list local models", err)
	}

	availableModels := make([]string, 0, len(listResp.Models))
	for _, m := range listResp.Models {
		availableModels = append(availableModels, m.Name)
		if m.Name == model {
			c.model = model
			return nil
		}
	}

	if len(listResp.Models) > 0 {
		fmt.Fprintf(os.Stderr, "[WARN] Model '%s' not found locally. Available models: %v\n", model, availableModels)
		fmt.Fprintf(os.Stderr, "[~] Falling back to first available model: %s\n", listResp.Models[0].Name)
		c.model = listResp.Models[0].Name
		return nil
	}

	return agenterrors.NewProviderError(fmt.Sprintf("model %s not found locally and no other models available. Available models: %s", model, availableModels), nil, "ollama", "")
}

// ListModels returns available local models
func (c *OllamaLocalClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	client, err := c.newClient()
	if err != nil {
		return nil, agenterrors.NewConfig("could not create ollama client", err)
	}

	listResp, err := client.List(ctx)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to list local models", err)
	}

	models := make([]ModelInfo, 0, len(listResp.Models))
	for _, m := range listResp.Models {
		models = append(models, ModelInfo{
			ID:       m.Name,
			Provider: "ollama-local",
		})
	}

	return models, nil
}

// SupportsVision returns true for OCR-capable models
func (c *OllamaLocalClient) SupportsVision() bool {
	modelLower := strings.ToLower(c.model)
	return strings.Contains(modelLower, "ocr") ||
		strings.Contains(modelLower, "vision") ||
		strings.Contains(modelLower, "llama3.2")
}

// SupportsConversationalVision returns true only for multimodal chat models.
// OCR-only models (e.g. glm-ocr) accept images but produce extraction output
// that doesn't help free-form conversational turns — the tool path
// (analyze_image_content) is the right channel for them. Inline embedding
// is only useful for chat models like llama3.2-vision.
func (c *OllamaLocalClient) SupportsConversationalVision() bool {
	modelLower := strings.ToLower(c.model)
	if strings.Contains(modelLower, "ocr") {
		return false
	}
	return strings.Contains(modelLower, "vision") ||
		strings.Contains(modelLower, "llama3.2")
}

// GetVisionModel returns empty string as vision is not supported
func (c *OllamaLocalClient) GetVisionModel() string {
	return ""
}

// SendVisionRequest handles vision/OCR requests for Ollama
// Delegates to SendChatRequest since the image handling is done in buildChatRequest
func (c *OllamaLocalClient) SendVisionRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	return c.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}

// SendChatRequestStream streams responses from local Ollama as they arrive
func (c *OllamaLocalClient) SendChatRequestStream(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
	client, err := c.newClient()
	if err != nil {
		return nil, agenterrors.NewConfig("could not create ollama client", err)
	}

	req, totalTokens := c.buildChatRequest(messages, tools, reasoning, true)

	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	builder := NewStreamingResponseBuilder(callback)
	var lastMetrics localOllamaMetrics
	var lastDoneReason string

	startTime := time.Now()

	err = client.Chat(ctx, req, func(res *localOllamaChatResponse) error {
		chunk := convertOllamaResponseToStreamingChunk(res)
		if err := builder.ProcessChunk(chunk); err != nil {
			return agenterrors.NewProviderError("failed to process ollama chat chunk", err, "ollama", c.model)
		}

		if res.DoneReason != "" {
			lastDoneReason = res.DoneReason
		}

		lastMetrics = res.Metrics
		return nil
	})
	if err != nil {
		return nil, agenterrors.NewProviderError("ollama chat failed", err, "ollama", c.model)
	}

	response := builder.GetResponse()
	if response == nil {
		response = &ChatResponse{}
	}

	if response.ID == "" {
		response.ID = "ollama-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if response.Object == "" {
		response.Object = "chat.completion"
	}
	if response.Created == 0 {
		response.Created = time.Now().Unix()
	}
	response.Model = c.model

	if len(response.Choices) == 0 {
		response.Choices = []Choice{{}}
	}

	choice := &response.Choices[0]
	if choice.Message.Role == "" {
		choice.Message.Role = "assistant"
	}
	if choice.FinishReason == "" {
		if lastDoneReason != "" {
			choice.FinishReason = lastDoneReason
		} else {
			choice.FinishReason = "stop"
		}
	}

	promptTokens := totalTokens
	if lastMetrics.PromptEvalCount > 0 {
		promptTokens = lastMetrics.PromptEvalCount
	}

	completionTokens := EstimateTokens(choice.Message.Content)
	if lastMetrics.EvalCount > 0 {
		completionTokens = lastMetrics.EvalCount
	}

	response.Usage.PromptTokens = promptTokens
	response.Usage.CompletionTokens = completionTokens
	response.Usage.TotalTokens = promptTokens + completionTokens
	response.Usage.EstimatedCost = 0

	if c.GetTracker() != nil && completionTokens > 0 {
		c.GetTracker().RecordRequest(time.Since(startTime), completionTokens)
	}

	return response, nil
}

func convertToolsToOllamaTools(tools []Tool) []localOllamaTool {
	if len(tools) == 0 {
		return nil
	}

	result := make([]localOllamaTool, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Type) == "" {
			continue
		}

		ollamaTool := localOllamaTool{
			Type: tool.Type,
			Function: localOllamaToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
			},
		}

		params := json.RawMessage(`{"type":"object"}`)
		if tool.Function.Parameters != nil {
			if raw, err := json.Marshal(tool.Function.Parameters); err == nil {
				params = raw
			}
		}

		ollamaTool.Function.Parameters = params
		result = append(result, ollamaTool)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func convertOllamaResponseToStreamingChunk(res *localOllamaChatResponse) *StreamingChatResponse {
	chunk := &StreamingChatResponse{
		ID:    res.Model,
		Model: res.Model,
	}

	if !res.CreatedAt.IsZero() {
		chunk.Created = res.CreatedAt.Unix()
	}

	delta := StreamingDelta{Role: res.Message.Role}

	if len(res.Message.ToolCalls) == 0 {
		trimmed := strings.TrimSpace(res.Message.Content)
		if trimmed != "" {
			delta.Content = res.Message.Content
		}
	}

	if len(res.Message.ToolCalls) > 0 {
		delta.ToolCalls = make([]StreamingToolCall, 0, len(res.Message.ToolCalls))
		for _, call := range res.Message.ToolCalls {
			arguments := ""
			if len(call.Function.Arguments) > 0 {
				arguments = string(call.Function.Arguments)
			}

			delta.ToolCalls = append(delta.ToolCalls, StreamingToolCall{
				Index: call.Function.Index,
				Function: &StreamingToolCallFunction{
					Name:      call.Function.Name,
					Arguments: arguments,
				},
			})
		}
	}

	choice := StreamingChoice{
		Index: 0,
		Delta: delta,
	}

	if res.DoneReason != "" {
		reason := res.DoneReason
		choice.FinishReason = &reason
	} else if res.Done {
		reason := "stop"
		choice.FinishReason = &reason
	}

	chunk.Choices = []StreamingChoice{choice}
	return chunk
}

func convertOllamaToolCalls(calls []localOllamaToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}

	result := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		arguments := ""
		if len(call.Function.Arguments) > 0 {
			arguments = string(call.Function.Arguments)
		}

		toolCall := ToolCall{Type: "function"}
		toolCall.Function.Name = strings.Split(call.Function.Name, "<|channel|>")[0]
		toolCall.Function.Arguments = arguments
		result = append(result, toolCall)
	}

	return result
}