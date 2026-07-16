// Package agent_api: Ollama local client constructors, environment setup, and request-building (split from ollama_local.go)
package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
			bracketWarn(os.Stderr, fmt.Sprintf("Model '%s' not found locally. Available models: %v", model, availableModels))
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
