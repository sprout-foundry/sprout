package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/apikeys"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

type LLMProvider interface {
	GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, imagePath ...string) (string, *types.TokenUsage, error)
	GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer, imagePath ...string) (*types.TokenUsage, error)
}

type LLMProviderImpl struct{}

func NewLLMProvider() *LLMProviderImpl {
	return &LLMProviderImpl{}
}

func (p *LLMProviderImpl) GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, imagePath ...string) (string, *types.TokenUsage, error) {
	return GetLLMResponse(modelName, messages, filename, cfg, timeout, imagePath...)
}

func (p *LLMProviderImpl) GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer, imagePath ...string) (*types.TokenUsage, error) {
	return GetLLMResponseStream(modelName, messages, filename, cfg, timeout, writer, imagePath...)
}

// simple provider health state (in-memory)
var providerFailures = map[string]int{}
var providerLastFail = map[string]time.Time{}
var healthLoaded = false

const providerHealthPath = ".ledit/provider_health.json"

const failureThreshold = 3
const openAfter = 2 * time.Minute

// retryWithBackoff executes an HTTP request with exponential backoff retry logic
// Handles 5xx errors, network errors, and specific 4xx errors that might be transient
func retryWithBackoff(req *http.Request, client *http.Client) (*http.Response, error) {
	const maxRetries = 3
	const baseDelay = 100 * time.Millisecond

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Reset request body for retry
		var reqBody []byte
		if req.Body != nil && attempt > 0 {
			reqBody, _ = io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(reqBody))
		}

		resp, err := client.Do(req)
		lastResp = resp
		lastErr = err

		if err != nil {
			// Network errors - retry with exponential backoff
			if attempt < maxRetries {
				delay := baseDelay * time.Duration(1<<attempt) // 100ms, 200ms, 400ms
				time.Sleep(delay)
				continue
			}
			return resp, err
		}

		// Check for retryable status codes
		shouldRetry := false
		switch resp.StatusCode {
		case 408: // Request Timeout
			shouldRetry = true
		case 429: // Too Many Requests
			shouldRetry = true
		case 500, 502, 503, 504: // Server errors
			shouldRetry = true
		}

		if shouldRetry && attempt < maxRetries {
			// Close response body before retry
			resp.Body.Close()

			// Exponential backoff with jitter
			delay := baseDelay * time.Duration(1<<attempt)
			jitter := time.Duration(time.Now().UnixNano() % int64(delay) / 2) // Add up to 50% jitter
			totalDelay := delay + jitter

			time.Sleep(totalDelay)
			continue
		}

		// Success or non-retryable error
		return resp, err
	}

	return lastResp, lastErr
}

func providerOpen(provider string) bool {
	ensureHealthLoaded()
	n := providerFailures[provider]
	if n < failureThreshold {
		return true
	}
	if time.Since(providerLastFail[provider]) > openAfter {
		providerFailures[provider] = 0
		_ = saveProviderHealth()
		return true
	}
	return false
}

func recordFailure(provider string) {
	ensureHealthLoaded()
	providerFailures[provider]++
	providerLastFail[provider] = time.Now()
	_ = saveProviderHealth()
}
func recordSuccess(provider string) {
	ensureHealthLoaded()
	providerFailures[provider] = 0
	_ = saveProviderHealth()
}

type providerHealthFile struct {
	Failures map[string]int    `json:"failures"`
	LastFail map[string]string `json:"last_fail"`
}

func ensureHealthLoaded() {
	if healthLoaded {
		return
	}
	data, err := os.ReadFile(providerHealthPath)
	if err == nil {
		var pf providerHealthFile
		if json.Unmarshal(data, &pf) == nil {
			if pf.Failures != nil {
				fresh := map[string]int{}
				for k, v := range pf.Failures {
					fresh[k] = v
				}
				providerFailures = fresh
			}
			if pf.LastFail != nil {
				freshLast := map[string]time.Time{}
				for k, v := range pf.LastFail {
					if t, perr := time.Parse(time.RFC3339, v); perr == nil {
						freshLast[k] = t
					}
				}
				providerLastFail = freshLast
			}
		}
	}
	if providerFailures == nil {
		providerFailures = map[string]int{}
	}
	if providerLastFail == nil {
		providerLastFail = map[string]time.Time{}
	}
	healthLoaded = true
}

func saveProviderHealth() error {
	_ = os.MkdirAll(filepath.Dir(providerHealthPath), 0755)
	failCopy := map[string]int{}
	for k, v := range providerFailures {
		failCopy[k] = v
	}
	lastCopy := map[string]string{}
	for k, t := range providerLastFail {
		lastCopy[k] = t.Format(time.RFC3339)
	}
	pf := providerHealthFile{Failures: failCopy, LastFail: lastCopy}
	b, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(providerHealthPath, b, 0644)
}

var (
	// DefaultTokenLimit is the default token limit for API calls
	DefaultTokenLimit = prompts.DefaultTokenLimit
)

// ShouldUseJSONResponse determines if JSON mode should be enabled for the API call
func ShouldUseJSONResponse(messages []prompts.Message) bool {
	for _, msg := range messages {
		// Type assert Content to string
		contentStr, ok := msg.Content.(string)
		if !ok {
			continue
		}

		content := strings.ToLower(contentStr)

		// Check for explicit JSON format requirements in system/user messages
		if strings.Contains(content, "return only json") ||
			strings.Contains(content, "respond with json") ||
			strings.Contains(content, "json format only") ||
			strings.Contains(content, "valid json object") ||
			strings.Contains(content, "return only a json object") ||
			strings.Contains(content, "you must respond with a valid json object") ||
			strings.Contains(content, "critical: you must respond with") {

			// Additional check: ensure it's not just mentioning JSON in general
			if strings.Contains(content, "json") {
				return true
			}
		}
	}
	return false
}

// GetLLMResponseWithTools makes an LLM call with tool calling support
func GetLLMResponseWithTools(modelName string, messages []prompts.Message, systemPrompt string, cfg *config.Config, timeout time.Duration) (string, *TokenUsage, error) {
	response, tokenUsage, err := GetLLMResponseWithToolsScoped(modelName, messages, systemPrompt, cfg, timeout, nil)
	return response, tokenUsage, err
}

// GetLLMResponseWithToolsScoped is like GetLLMResponseWithTools but restricts tools to an allowlist
func GetLLMResponseWithToolsScoped(modelName string, messages []prompts.Message, systemPrompt string, cfg *config.Config, timeout time.Duration, allowed []string) (string, *TokenUsage, error) {
	// Debug: Log function entry
	log := utils.GetLogger(cfg.SkipPrompt)
	log.Log("=== GetLLMResponseWithToolsScoped Debug ===")
	log.Log(fmt.Sprintf("Model: %s", modelName))
	log.Log(fmt.Sprintf("Messages count: %d", len(messages)))
	log.Log(fmt.Sprintf("System prompt length: %d", len(systemPrompt)))
	log.Log(fmt.Sprintf("Allowed tools: %v", allowed))
	// Tools always enabled (forced)

	// Check messages for detokenize before processing
	// for i, msg := range messages {
	// 	contentStr := fmt.Sprintf("%v", msg.Content)
	// 	if strings.Contains(contentStr, "detokenize") {
	// 		// logger.Log(fmt.Sprintf("ERROR: Found 'detokenize' in input message %d!", i))
	// 	}
	// }

	// Debug: Check message marshaling
	// for i, msg := range messages {
	// 	msgBytes, _ := json.Marshal(msg)
	// 	// logger.Log(fmt.Sprintf("Message %d JSON: %s", i, string(msgBytes)))
	// }

	// Use provider-specific tool calling strategy
	strategy := GetToolCallingStrategy(modelName)
	parts := strings.SplitN(modelName, ":", 3)
	provider := parts[0]
	model := ""
	if len(parts) > 1 {
		model = parts[1]
	}
	if len(parts) > 2 {
		model = parts[1] + parts[2]
	}

	log.Logf("DEBUG: Using tool calling strategy for %s: native=%v, capability=%d", provider, strategy.UseNative, strategy.Capability)

	// Filter available tools based on allowlist
	availableTools := GetAvailableTools()
	var filteredTools []Tool
	nameAllowed := map[string]bool{}
	if len(allowed) > 0 {
		for _, n := range allowed {
			nameAllowed[strings.ToLower(strings.TrimSpace(n))] = true
		}
	}

	for _, t := range availableTools {
		if strings.ToLower(t.Type) != "function" {
			continue
		}
		if len(nameAllowed) > 0 {
			lname := strings.ToLower(strings.TrimSpace(t.Function.Name))
			if !nameAllowed[lname] {
				continue
			}
		}
		filteredTools = append(filteredTools, t)
	}

	var apiURL string
	var apiKey string
	var err error
	switch provider {
	case "openai":
		log.Logf("DEBUG: Using OpenAI provider: %s", modelName)
		apiURL = "https://api.openai.com/v1/chat/completions"
		apiKey, err = apikeys.GetAPIKey("openai", true)
	case "groq":
		log.Logf("DEBUG: Using Groq provider: %s", modelName)
		apiURL = "https://api.groq.com/openai/v1/chat/completions"
		apiKey, err = apikeys.GetAPIKey("groq", true)
	case "deepseek":
		log.Logf("DEBUG: Using DeepSeek provider: %s", modelName)
		apiURL = "https://api.deepseek.com/openai/v1/chat/completions"
		apiKey, err = apikeys.GetAPIKey("deepseek", true)
	case "deepinfra":
		log.Logf("DEBUG: Using DeepInfra provider: %s", modelName)
		apiURL = "https://api.deepinfra.com/v1/openai/chat/completions"
		apiKey, err = apikeys.GetAPIKey("deepinfra", true)
	case "lambda-ai":
		apiURL = "https://api.lambda.ai/v1/chat/completions"
		apiKey, err = apikeys.GetAPIKey("lambda-ai", true)
	default:
		// Fallback to non-tools path
		resp, tokenUsage, e := GetLLMResponse(modelName, messages, systemPrompt, cfg, timeout)
		return resp, tokenUsage, e
	}
	if err != nil {
		return "", nil, err
	}

	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}

	// Add tools based on provider capability
	if strategy.UseNative {
		tools, err := strategy.PrepareToolsForProvider(filteredTools)
		if err != nil {
			return "", nil, fmt.Errorf("failed to prepare tools for provider: %w", err)
		}
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
		log.Logf("DEBUG: Added %d native tools for %s", len(filteredTools), provider)
	} else {
		// For providers without native tool support, add tool instructions to system prompt
		if systemPrompt == "" {
			systemPrompt = strategy.GetSystemPrompt()
		} else {
			systemPrompt = systemPrompt + "\n\n" + strategy.GetSystemPrompt()
		}
		log.Logf("DEBUG: Using text-based tool calling for %s with %d tools", provider, len(filteredTools))
	}
	// Enable JSON mode when prompts explicitly require strict JSON output, but not when using native tool calling
	if !strategy.UseNative && ShouldUseJSONResponse(messages) {
		payload["response_format"] = map[string]any{"type": "json_object"}
	}
	if cfg.Temperature != 0 {
		payload["temperature"] = cfg.Temperature
	}
	if systemPrompt != "" {
		// Prepend a system message if provided
		messages = append([]prompts.Message{{Role: "system", Content: systemPrompt}}, messages...)
		payload["messages"] = messages
	}

	body, merr := json.Marshal(payload)
	if merr != nil {
		return "", nil, merr
	}

	// Debug: Log the actual JSON payload being sent
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.Logf("DEBUG: LLM TOKEN_ESTIMATE: %d", EstimateTokens(string(body)))
	logger.Log(fmt.Sprintf("DEBUG: LLM Request Payload: %s", string(body)))
	logger.Log(fmt.Sprintf("DEBUG: Request URL: %s", apiURL))
	logger.Log(fmt.Sprintf("DEBUG: Model: %s", model))

	// Also check for any suspicious fields that might be causing issues
	suspiciousFields := []string{"detokenize", "tokenize", "_token", "token_"}
	for _, field := range suspiciousFields {
		if strings.Contains(string(body), field) {
			// logger.Log(fmt.Sprintf("WARNING: Found suspicious field '%s' in request payload", field))
		}
	}

	req, rerr := http.NewRequest("POST", apiURL, bytes.NewBuffer(body))
	if rerr != nil {
		return "", nil, rerr
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: timeout}
	resp, derr := retryWithBackoff(req, client)
	if derr != nil {
		return "", nil, derr
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)

		// logger.Log(fmt.Sprintf("DEBUG: HTTP error response: status=%d, body=%s", resp.StatusCode, string(raw)))

		lower := strings.ToLower(string(raw))
		// Check if this is the detokenize error
		if strings.Contains(lower, "detokenize") {
			// logger.Log("ERROR: DeepInfra detokenize validation error detected in HTTP response!")
			return "", nil, fmt.Errorf("DeepInfra detokenize validation error: %s", string(raw))
		}

		// Retry once without response_format for providers that don't support JSON mode
		if strings.Contains(lower, "response_format") || strings.Contains(lower, "unsupported") {
			delete(payload, "response_format")
			body2, _ := json.Marshal(payload)
			req2, _ := http.NewRequest("POST", apiURL, bytes.NewBuffer(body2))
			req2.Header.Set("Content-Type", "application/json")
			req2.Header.Set("Authorization", "Bearer "+apiKey)
			resp2, derr2 := client.Do(req2)
			if derr2 == nil {
				defer resp2.Body.Close()
				if resp2.StatusCode == 200 {
					resp = resp2
					// fallthrough to decoding below
				} else {
					raw2, _ := io.ReadAll(resp2.Body)
					return "", nil, fmt.Errorf("provider error %d: %s", resp2.StatusCode, string(raw2))
				}
			} else {
				return "", nil, derr2
			}
		} else {
			return "", nil, fmt.Errorf("provider error %d: %s", resp.StatusCode, string(raw))
		}
	}
	var full struct {
		Choices []struct {
			Message struct {
				Role      string     `json:"role"`
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage TokenUsage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&full); err != nil {
		return "", nil, err
	}
	if len(full.Choices) > 0 {
		msg := full.Choices[0].Message

		// Parse tool calls using the appropriate strategy
		if strategy.UseNative && len(msg.ToolCalls) > 0 {
			// Native tool calls from provider
			toolCalls, err := strategy.ParseToolCallsForProvider("", msg.ToolCalls)
			if err != nil {
				log.Logf("DEBUG: Failed to parse native tool calls: %v", err)
				// Fall back to text parsing
				toolCalls, err = strategy.ParseToolCallsForProvider(msg.Content, nil)
				if err == nil && len(toolCalls) > 0 {
					wrapper := map[string]any{"tool_calls": toolCalls}
					wb, _ := json.Marshal(wrapper)
					return string(wb), &full.Usage, nil
				}
			} else if len(toolCalls) > 0 {
				wrapper := map[string]any{"tool_calls": toolCalls}
				wb, _ := json.Marshal(wrapper)
				return string(wb), &full.Usage, nil
			}
		} else if !strategy.UseNative {
			// Text-based tool calling - parse from content
			toolCalls, err := strategy.ParseToolCallsForProvider(msg.Content, nil)
			if err == nil && len(toolCalls) > 0 {
				wrapper := map[string]any{"tool_calls": toolCalls}
				wb, _ := json.Marshal(wrapper)
				log.Logf("DEBUG: Parsed %d tool calls from text for %s", len(toolCalls), provider)
				return string(wb), &full.Usage, nil
			}
		}

		return msg.Content, &full.Usage, nil
	}
	return "", &full.Usage, nil
}

// --- Main Dispatcher ---

func GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer, imagePath ...string) (*types.TokenUsage, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	var totalInputTokens int
	for _, msg := range messages {
		totalInputTokens += GetMessageTokens(msg.Role, GetMessageText(msg.Content))
	}
	ui.Out().Print(prompts.TokenEstimate(totalInputTokens, modelName))
	if totalInputTokens > DefaultTokenLimit && !cfg.SkipPrompt {
		reader := bufio.NewReader(os.Stdin)
		ui.Out().Print(prompts.TokenLimitWarning(totalInputTokens, DefaultTokenLimit))
		confirm, err := reader.ReadString('\n')
		if err != nil {
			ui.Out().Print(prompts.APIKeyError(err))
			return nil, err
		}
		if strings.TrimSpace(confirm) != "y" {
			ui.Out().Print(prompts.OperationCancelled() + "\n")
			return nil, nil
		}
		ui.Out().Print(prompts.ContinuingRequest())

		// User confirmed, continue with the request
	}

	var err error
	var tokenUsage *types.TokenUsage

	// Inform UI of the active model so the header can render it persistently
	if ui.IsUIActive() && strings.TrimSpace(modelName) != "" {
		ui.PublishModel(modelName)
	}

	parts := strings.SplitN(modelName, ":", 3) // Changed from 2 to 3
	provider := parts[0]
	model := ""
	if len(parts) > 1 {
		model = parts[1]
	}
	if len(parts) > 2 { // If there are 3 parts, the last one is the model
		model = parts[2]
	}

	ollamaUrl := fmt.Sprintf("%s/v1/chat/completions", cfg.OllamaServerURL)

	if !providerOpen(provider) {
		// short-circuit to fallback if available
		fallbackModel := cfg.LocalModel
		if fallbackModel != "" && provider != "ollama" {
			ui.Out().Printf("[llm] provider '%s' circuit open; using local fallback %s\n", provider, fallbackModel)
			tu, ferr := callOpenAICompatibleStream(ollamaUrl, "ollama", fallbackModel, messages, cfg, timeout, writer)
			return tu, ferr
		}
	}

	// Run-log the outbound request (messages are marshaled for readability)
	if rl := utils.GetRunLogger(); rl != nil {
		msgDump, _ := json.Marshal(messages)
		rl.LogEvent("llm_request", map[string]any{"provider": provider, "model": modelName, "filename": filename, "messages": string(msgDump)})
	}

	switch provider {
	case "openai":
		if cfg.HealthChecks {
			_ = CheckEndpointReachable("https://api.openai.com/v1/models", 2*time.Second)
		}
		apiKey, err := apikeys.GetAPIKey("openai", true) // Pass cfg.Interactive
		if err != nil {
			ui.Out().Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.openai.com/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "groq":
		if cfg.HealthChecks {
			_ = CheckEndpointReachable("https://api.groq.com/", 2*time.Second)
		}
		apiKey, err := apikeys.GetAPIKey("groq", true) // Pass cfg.Interactive
		if err != nil {
			ui.Out().Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.groq.com/openai/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "gemini":
		// Gemini streaming not implemented, using non-streaming call and writing the whole response.
		var content string
		content, err = callGeminiAPI(model, messages, timeout, false) // Removed undefined useSearchGrounding variable
		if err == nil && content != "" {
			logger := utils.GetLogger(cfg.SkipPrompt)
			logger.Log(fmt.Sprintf("Gemini API response: %s", content)) // Log the response
			content = removeThinkTags(content)
			_, _ = writer.Write([]byte(content))
		}
		// Estimate token usage for Gemini (mark as estimated)
		if err == nil {
			est := estimateUsageFromMessages(messages)
			est.Estimated = true
			tokenUsage = est
		}
	case "lambda-ai":
		if cfg.HealthChecks {
			_ = CheckEndpointReachable("https://api.lambda.ai/", 2*time.Second)
		}
		apiKey, err := apikeys.GetAPIKey("lambda-ai", true) // Pass cfg.Interactive
		if err != nil {
			ui.Out().Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.lambda.ai/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "cerebras":
		if cfg.HealthChecks {
			_ = CheckEndpointReachable("https://api.cerebras.ai/", 2*time.Second)
		}
		apiKey, err := apikeys.GetAPIKey("cerebras", true) // Pass cfg.Interactive
		if err != nil {
			ui.Out().Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.cerebras.ai/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "deepseek":
		if cfg.HealthChecks {
			_ = CheckEndpointReachable("https://api.deepseek.com/", 2*time.Second)
		}
		apiKey, err := apikeys.GetAPIKey("deepseek", true) // Pass cfg.Interactive
		if err != nil {
			ui.Out().Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.deepseek.com/openai/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "deepinfra":
		logger.Log("DEBUG: Routing to DeepInfra provider")
		if cfg.HealthChecks {
			_ = CheckEndpointReachable("https://api.deepinfra.com/", 2*time.Second)
		}
		apiKey, err := apikeys.GetAPIKey("deepinfra", true) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		logger.Log("DEBUG: About to call callOpenAICompatibleStream for DeepInfra")
		tokenUsage, err = callOpenAICompatibleStream("https://api.deepinfra.com/v1/openai/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "custom": // New case for custom provider:url:model
		var endpointURL string

		customParts := strings.SplitN(modelName, ":", 4)

		if len(customParts) == 4 {
			// Format: custom:base_url:path_suffix:model
			endpointURL = customParts[1] + customParts[2]
			model = customParts[3]
		} else if len(customParts) == 3 {
			// Format: custom:full_url:model
			endpointURL = customParts[1]
			model = customParts[2]
		} else {
			err = fmt.Errorf("invalid model name format for 'custom' provider. Expected 'custom:base_url:path_suffix:model' or 'custom:full_url:model', got '%s'", modelName)
			ui.Out().Print(prompts.LLMResponseError(err))
			return nil, err
		}

		apiKey, err := apikeys.GetAPIKey("custom", true) // Use "custom" as the provider for API key lookup
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream(endpointURL, apiKey, model, messages, cfg, timeout, writer)
	case "ollama":
		tokenUsage, err = callOllamaAPI(model, messages, cfg, timeout, writer)
	default:
		// Fallback to openai-compatible ollama api
		ui.Out().Print(prompts.ProviderNotRecognized() + "\n")
		modelName = cfg.LocalModel
		tokenUsage, err = callOpenAICompatibleStream(ollamaUrl, "ollama", modelName, messages, cfg, timeout, writer)
	}

	if err != nil {
		recordFailure(provider)
		// Provider failover: try local/ollama fallback once
		fallbackModel := cfg.LocalModel
		if fallbackModel != "" && provider != "ollama" {
			ui.Out().Printf("[llm] provider '%s' failed; attempting failover to local model via ollama: %s\n", provider, fallbackModel)
			ollamaURL := fmt.Sprintf("%s/v1/chat/completions", cfg.OllamaServerURL)
			if tu, ferr := callOpenAICompatibleStream(ollamaURL, "ollama", fallbackModel, messages, cfg, timeout, writer); ferr == nil {
				return tu, nil
			} else {
				ui.Out().Printf("[llm] failover to ollama failed: %v\n", ferr)
			}
		}
		ui.Out().Print(prompts.LLMResponseError(err))
		return tokenUsage, err
	}
	recordSuccess(provider)

	// Run-log the raw content returned (already streamed into writer). We also include usage.
	if rl := utils.GetRunLogger(); rl != nil {
		// We cannot easily capture full stream here; GetLLMResponse wraps and returns full content.
		// Log token usage as a proxy and note the model used.
		rl.LogEvent("llm_response_meta", map[string]any{"provider": provider, "model": modelName, "usage": tokenUsage})
	}

	// Ensure token usage estimation is complete (but don't publish individual costs to UI)
	if tokenUsage != nil {
		// If provider returned usage, trust it. Otherwise, estimate from messages
		if tokenUsage.TotalTokens == 0 {
			est := 0
			for _, m := range messages {
				est += GetMessageTokens(m.Role, GetMessageText(m.Content))
			}
			if est < 1 {
				est = 1
			}
			tokenUsage.TotalTokens = est
			if tokenUsage.PromptTokens == 0 && tokenUsage.CompletionTokens == 0 {
				tokenUsage.PromptTokens = est
			} else if tokenUsage.CompletionTokens == 0 && tokenUsage.PromptTokens > 0 {
				// Some providers (like DeepInfra) don't return completion tokens
				// Estimate completion tokens as difference between total and prompt
				if tokenUsage.TotalTokens > tokenUsage.PromptTokens {
					tokenUsage.CompletionTokens = tokenUsage.TotalTokens - tokenUsage.PromptTokens
				} else if tokenUsage.TotalTokens == tokenUsage.PromptTokens {
					// Provider is likely only reporting prompt tokens as total
					// Estimate completion tokens based on typical response size
					estimatedCompletion := max(100, tokenUsage.PromptTokens/4) // At least 100 tokens or 25% of prompt
					tokenUsage.CompletionTokens = estimatedCompletion
					tokenUsage.TotalTokens = tokenUsage.PromptTokens + tokenUsage.CompletionTokens
				} else {
					// Fallback: estimate completion tokens as ~30% of total for typical responses
					tokenUsage.CompletionTokens = int(float64(tokenUsage.TotalTokens) * 0.3)
					tokenUsage.PromptTokens = tokenUsage.TotalTokens - tokenUsage.CompletionTokens
				}
			}
		}
		// Note: Individual cost publishing removed - let agent system handle aggregate cost display
	}

	// Display token usage information to user
	if tokenUsage != nil {
		cost := CalculateCost(*tokenUsage, modelName)
		ui.Out().Print(prompts.TokenUsage(tokenUsage.PromptTokens, tokenUsage.CompletionTokens, tokenUsage.TotalTokens, modelName, cost))
	}

	return tokenUsage, nil
}

func GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, imagePath ...string) (string, *types.TokenUsage, error) {
	var contentBuffer strings.Builder
	// Stream to UI when enabled, while also capturing content in buffer
	var writer io.Writer = &contentBuffer
	var stream *ui.StreamWriter
	if ui.IsUIActive() {
		stream = ui.NewStreamWriter()
		writer = io.MultiWriter(stream, &contentBuffer)
	}
	// GetLLMResponseStream handles the token limit prompt and provider logic
	tokenUsage, err := GetLLMResponseStream(modelName, messages, filename, cfg, timeout, writer, imagePath...)
	if stream != nil {
		stream.Flush()
	}
	if err != nil {
		// GetLLMResponseStream already prints the error if it happens
		return modelName, tokenUsage, err
	}

	// This can happen if user cancels at the prompt in GetLLMResponseStream
	if contentBuffer.Len() == 0 {
		return modelName, tokenUsage, nil
	}

	content := contentBuffer.String()

	// Run-log the full response content for this call for forensic analysis
	if rl := utils.GetRunLogger(); rl != nil {
		rl.LogEvent("llm_response", map[string]any{"model": modelName, "filename": filename, "response": content})
	}

	// Remove any think tags before returning the content
	content = removeThinkTags(content)

	return content, tokenUsage, nil
}

// GetCommitMessage generates a git commit message based on code changes using an LLM.
func GetCommitMessage(cfg *config.Config, changelog string, originalPrompt string, filename string) (string, error) {
	modelName := cfg.WorkspaceModel
	if modelName == "" {
		modelName = cfg.EditingModel // Fallback if workspace model is not configured
	}

	messages := prompts.BuildCommitMessages(changelog, originalPrompt)

	// Use a special version that explicitly disables JSON mode and tools for commit messages
	response, _, err := getCommitMessageFromLLM(modelName, messages, cfg, 1*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get commit message from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}

// getCommitMessageFromLLM is a specialized version that disables JSON mode and tools
func getCommitMessageFromLLM(modelName string, messages []prompts.Message, cfg *config.Config, timeout time.Duration) (string, *TokenUsage, error) {
	var contentBuffer strings.Builder
	tokenUsage, err := getCommitMessageStreamFromLLM(modelName, messages, cfg, timeout, &contentBuffer)
	if err != nil {
		return "", tokenUsage, err
	}
	content := contentBuffer.String()
	content = removeThinkTags(content)
	return content, tokenUsage, nil
}

// getCommitMessageStreamFromLLM is like GetLLMResponseStream but explicitly disables JSON mode and tools
func getCommitMessageStreamFromLLM(modelName string, messages []prompts.Message, cfg *config.Config, timeout time.Duration, writer io.Writer) (*TokenUsage, error) {
	var totalInputTokens int
	for _, msg := range messages {
		totalInputTokens += GetMessageTokens(msg.Role, GetMessageText(msg.Content))
	}

	parts := strings.SplitN(modelName, ":", 3)
	provider := parts[0]
	model := ""
	if len(parts) > 1 {
		model = parts[1]
	}
	if len(parts) > 2 {
		model = parts[2]
	}

	var tokenUsage *TokenUsage
	var err error

	switch provider {
	case "deepinfra":
		apiKey, keyErr := apikeys.GetAPIKey("deepinfra", true)
		if keyErr != nil {
			return nil, keyErr
		}
		tokenUsage, err = callOpenAICompatibleStreamNoTools("https://api.deepinfra.com/v1/openai/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "openai":
		apiKey, keyErr := apikeys.GetAPIKey("openai", true)
		if keyErr != nil {
			return nil, keyErr
		}
		tokenUsage, err = callOpenAICompatibleStreamNoTools("https://api.openai.com/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	default:
		// For other providers, fall back to the regular implementation
		return GetLLMResponseStream(modelName, messages, "", cfg, timeout, writer)
	}

	return tokenUsage, err
}

// GenerateSearchQuery uses an LLM to generate a concise search query based on the provided context.
func GenerateSearchQuery(cfg *config.Config, context string) ([]string, error) {
	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at generating concise search queries to resolve software development issues. Your output should be a JSON array of 1 to 2 concise search queries (2-15 words each), based on the provided context. For example: `[\"query one\", \"query two\"]`"},
		{Role: "user", Content: fmt.Sprintf("Generate search queries based on the following context: %s", context)},
	}

	modelName := cfg.EditingModel // Use the editing model for generating search queries

	// Use a short timeout for generating a search query
	queryResponse, _, err := GetLLMResponse(modelName, messages, "", cfg, GetSmartTimeout(cfg, modelName, "search")) // Query generation does not use search grounding
	if err != nil {
		return nil, fmt.Errorf("failed to generate search query from LLM: %w", err)
	}

	// The response might be inside a code block, let's be robust.
	if strings.Contains(queryResponse, "```json") {
		parts := strings.SplitN(queryResponse, "```json", 2)
		if len(parts) > 1 {
			queryResponse = strings.Split(parts[1], "```")[0]
		} else if strings.HasPrefix(queryResponse, "```") && strings.HasSuffix(queryResponse, "```") {
			queryResponse = strings.TrimPrefix(queryResponse, "```")
			queryResponse = strings.TrimSuffix(queryResponse, "```")
		}
	}

	var searchQueries []string
	if err := json.Unmarshal([]byte([]byte(queryResponse)), &searchQueries); err != nil {
		return nil, fmt.Errorf("failed to parse search queries from LLM response: %w, response: %s", err, queryResponse)
	}

	return searchQueries, nil
}

// GetScriptRiskAnalysis sends a shell script to the summary model for risk analysis.
func GetScriptRiskAnalysis(cfg *config.Config, scriptContent string) (string, error) {
	messages := prompts.BuildScriptRiskAnalysisMessages(scriptContent)
	modelName := cfg.SummaryModel // Use the summary model for this task
	if modelName == "" {
		// Fallback if summary model is not configured
		modelName = cfg.EditingModel
		ui.Out().Printf(prompts.NoSummaryModelFallback(modelName)) // New prompt
	}

	response, _, err := GetLLMResponse(modelName, messages, "", cfg, GetSmartTimeout(cfg, modelName, "analysis")) // Analysis does not use search grounding
	if err != nil {
		return "", fmt.Errorf("failed to get script risk analysis from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}

// GetCodeReview asks the LLM to review a combined diff of changes against the original prompt.
func GetCodeReview(cfg *config.Config, combinedDiff, originalPrompt, workspaceContext string) (*types.CodeReviewResult, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	// Use a dedicated CodeReviewModel if available, otherwise fall back to EditingModel
	modelName := cfg.CodeReviewModel
	if modelName == "" {
		modelName = cfg.EditingModel
	}

	messages := prompts.BuildCodeReviewMessages(combinedDiff, originalPrompt, workspaceContext, workspaceContext)

	response, _, err := GetLLMResponse(modelName, messages, "", cfg, GetSmartTimeout(cfg, modelName, "code_review"))
	if err != nil {
		return nil, fmt.Errorf("failed to get code review from LLM: %w", err)
	}

	if response == "" {
		return nil, fmt.Errorf("LLM returned an empty response for code review")
	}

	// Robust JSON extraction: prefer centralized utils extractor, then fallback cleaner with required fields
	jsonStr, extractErr := utils.ExtractJSON(response)
	if jsonStr == "" || extractErr != nil {
		// Fallback: clean and validate presence of required fields
		cleaned, cleanErr := utils.ExtractJSON(response)
		if cleanErr == nil {
			cleanErr = utils.ValidateJSONFields(cleaned, []string{"status", "feedback"})
		}
		if cleanErr != nil {
			return nil, fmt.Errorf("failed to extract JSON from LLM response: %v; fallback clean failed: %v. Full response: %s", extractErr, cleanErr, response)
		}
		jsonStr = cleaned
	}

	// Add debug logging for JSON parsing issues
	if os.Getenv("DEBUG_JSON_PARSING") == "true" {
		logger.Logf("DEBUG: Extracted JSON string: %s", jsonStr)
		logger.Logf("DEBUG: JSON length: %d", len(jsonStr))
	}

	var reviewResult types.CodeReviewResult
	if err := json.Unmarshal([]byte(jsonStr), &reviewResult); err != nil {
		return nil, fmt.Errorf("failed to parse code review JSON from LLM response: %w\nExtracted JSON was: %s\nFull response was: %s", err, jsonStr, response)
	}

	// Ensure required fields are minimally present
	if reviewResult.Status == "" {
		reviewResult.Status = "needs_revision"
	}
	if reviewResult.Feedback == "" {
		reviewResult.Feedback = "No feedback provided."
	}
	// If detailed guidance missing on needs_revision, provide guidance for the LLM
	if reviewResult.Status == "needs_revision" && strings.TrimSpace(reviewResult.DetailedGuidance) == "" {
		reviewResult.DetailedGuidance = "Apply the minimal changes required by the original prompt and ensure output format strictly matches the prompt."
	}

	return &reviewResult, nil
}

// GetStagedCodeReview performs a code review on staged Git changes using a human-readable prompt.
// This is specifically designed for the review-staged command.
func GetStagedCodeReview(cfg *config.Config, stagedDiff, reviewPrompt, workspaceContext string) (*types.CodeReviewResult, error) {
	modelName := cfg.EditingModel
	if modelName == "" {
		return nil, fmt.Errorf("no editing model specified in config")
	}

	// Build messages for the staged code review
	var messages []prompts.Message

	// Add system message with the review prompt
	messages = append(messages, prompts.Message{
		Role:    "system",
		Content: reviewPrompt,
	})

	// Add user message with the staged diff and optional workspace context
	userContent := fmt.Sprintf("Please review the following staged Git changes:\n\n```diff\n%s\n```", stagedDiff)
	if strings.TrimSpace(workspaceContext) != "" {
		userContent = fmt.Sprintf("Workspace Context:\n%s\n\n%s", workspaceContext, userContent)
	}

	messages = append(messages, prompts.Message{
		Role:    "user",
		Content: userContent,
	})

	response, _, err := GetLLMResponse(modelName, messages, "", cfg, GetSmartTimeout(cfg, modelName, "code_review"))
	if err != nil {
		return nil, fmt.Errorf("failed to get staged code review from LLM: %w", err)
	}

	if response == "" {
		return nil, fmt.Errorf("LLM returned an empty response for staged code review")
	}

	// Parse the response to extract status and feedback
	// Since we're using a human-readable prompt, we need to parse the text response
	return parseStagedCodeReviewResponse(response)
}

// parseStagedCodeReviewResponse parses the human-readable code review response
func parseStagedCodeReviewResponse(response string) (*types.CodeReviewResult, error) {
	result := &types.CodeReviewResult{}

	// Look for status indicators in the response
	responseLower := strings.ToLower(response)

	if strings.Contains(responseLower, "status") && strings.Contains(responseLower, "approved") {
		result.Status = "approved"
	} else if strings.Contains(responseLower, "status") && strings.Contains(responseLower, "needs_revision") {
		result.Status = "needs_revision"
	} else if strings.Contains(responseLower, "status") && strings.Contains(responseLower, "rejected") {
		result.Status = "rejected"
	} else {
		// Default to needs_revision if we can't determine status
		result.Status = "needs_revision"
	}

	// The entire response is the feedback
	result.Feedback = strings.TrimSpace(response)

	// For rejected status, suggest a new prompt (this is a simple implementation)
	if result.Status == "rejected" {
		result.NewPrompt = "Please address the issues identified in the code review and resubmit the changes."
	}

	return result, nil
}
