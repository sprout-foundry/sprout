package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/logging"
	"github.com/sprout-foundry/sprout/pkg/modelregistry"
)

// generic_provider.go — the GenericProvider core: the provider type,
// constructor + model-cache warming, the chat-request send path, the
// connection check, and the config getters/setters. Model listing lives in
// generic_provider_models.go; capability/TPS stats in generic_provider_tps.go.
// (The vision request-path methods are in the pre-existing
// generic_provider_vision.go.)

// GenericProvider implements ClientInterface using JSON configuration
type GenericProvider struct {
	config          *ProviderConfig
	httpClient      *http.Client
	streamingClient *http.Client
	debug           bool
	model           string

	// mu guards models/modelsCached written by the background warmModelsCache goroutine.
	mu           sync.RWMutex
	models       []api.ModelInfo
	modelsCached bool

	// warmPending prevents goroutine accumulation from rapid warmModelsCache calls.
	warmPending atomic.Bool

	// detectedBackend caches the auto-detected backend type (guarded by mu).
	detectedBackend BackendType
	backendDetected bool

	// maxTokensHint overrides the max_tokens computation in buildChatRequest when > 0.
	maxTokensHint   int
	maxTokensHintMu sync.RWMutex
}

// HTTP error formatting helpers are in generic_provider_http_errors.go.

// NewGenericProvider creates a new generic provider from configuration
func NewGenericProvider(config *ProviderConfig) (*GenericProvider, error) {
	if err := config.Validate(); err != nil {
		return nil, agenterrors.NewValidation(fmt.Sprintf("invalid provider config: %v", err), nil)
	}

	timeout := config.GetTimeout()
	streamingTimeout := config.GetStreamingTimeout()

	p := &GenericProvider{
		config: config,
		// Dedicated transports (see newProviderTransport): the 30 s idle
		// window and 15 s TCP keepalive keep the pooled connections from
		// outliving Cloudflare-fronted providers' and home NATs' idle
		// limits, so a reused connection is not silently reset on its first
		// read. Separate instances keep the streaming and non-streaming
		// pools independent.
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: newProviderTransport(),
		},
		streamingClient: &http.Client{
			Timeout:   streamingTimeout,
			Transport: newProviderTransport(),
		},
		debug: false,
		model: config.Defaults.Model,
	}
	// Warm the models cache in the background (skip for local endpoints).
	if p.model != "" && p.config.Endpoint != "" &&
		!strings.Contains(p.config.Endpoint, "127.0.0.1") &&
		!strings.Contains(p.config.Endpoint, "localhost") {
		p.warmModelsCache()
	}
	return p, nil
}

// warmModelsCache fetches /models in the background to populate the cache.
func (p *GenericProvider) warmModelsCache() {
	p.mu.RLock()
	cached := p.modelsCached
	p.mu.RUnlock()
	if cached {
		return
	}
	// Deduplicate: prevent goroutine accumulation under rapid SetModel calls.
	if !p.warmPending.CompareAndSwap(false, true) {
		return // Another warm-up is already in flight
	}
	go func() {
		defer p.warmPending.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = p.ListModels(ctx)
	}()
}

// SendChatRequest sends a non-streaming chat request
func (p *GenericProvider) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	resp, err := p.sendChatRequestImpl(ctx, messages, tools, reasoning, disableThinking)
	return p.reconcileVisionCapability(messages, resp, err, func(msgs []api.Message) (*api.ChatResponse, error) {
		return p.sendChatRequestImpl(ctx, msgs, tools, reasoning, disableThinking)
	})
}

// sendChatRequestImpl is the plain chat path; SendChatRequest wraps it with
// vision-capability verification (SP-140 Phase 2).
func (p *GenericProvider) sendChatRequestImpl(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (chatResp *api.ChatResponse, err error) {
	// Snapshot model under lock to prevent races with SetModel.
	p.mu.RLock()
	currentModel := p.model
	p.mu.RUnlock()

	// The audit event records the wire facts of this call (digests, byte
	// counts, tokens, outcome) — never the request or response content. The
	// deferred emit runs on every return path, so a call that fails over or
	// errors still produces exactly one event.
	var (
		auditSentBody []byte
		auditRespBody []byte
		auditFailover bool
	)
	defer func() {
		p.emitCallAudit(ctx, messages, currentModel, auditSentBody, auditRespBody, chatResp, err, auditFailover, false)
	}()

	requestBody, err := p.buildChatRequest(messages, tools, reasoning, disableThinking, false)
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to build chat request")
	}

	req, sentBody, err := p.buildHTTPRequestCtx(ctx, requestBody, false)
	if err != nil {
		// Log request on build error
		logging.LogRequestPayloadOnError(sentBody, p.config.Name, currentModel, false, "build_http_request", err)
		return nil, agenterrors.Wrap(err, "failed to build HTTP request")
	}
	auditSentBody = sentBody

	// Read httpClient under lock, then release before the network call.
	p.mu.RLock()
	client := p.httpClient
	p.mu.RUnlock()

	resp, diag, err := p.executeChatRequest(client, req, sentBody, false)
	if err != nil {
		// For local providers, attempt to auto-start the server and retry once.
		if recovered := p.tryLocalServerRecovery(); recovered {
			auditFailover = true
			req2, sentBody2, err2 := p.buildHTTPRequestCtx(ctx, requestBody, false)
			if err2 == nil {
				p.mu.RLock()
				c2 := p.httpClient
				p.mu.RUnlock()
				if resp2, err3 := c2.Do(req2); err3 == nil {
					resp = resp2
					err = nil
					auditSentBody = sentBody2
				}
			}
		}
		if err != nil {
			// Log request on HTTP error
			logging.LogRequestPayloadOnError(sentBody, p.config.Name, currentModel, false, "http_request_failed", err)
			return nil, p.networkErrorWithConnDiag("HTTP request failed", err, diag)
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Retry with max_completion_tokens for backends that require it
		retryBody, retryResp, retried, retryErr := p.tryMaxCompletionTokensRetry(sentBody, false, body)
		if retried {
			auditFailover = true
			requestBody = retryBody
			if retryErr != nil {
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, currentModel, false,
					"retry_max_completion_tokens_build", retryErr)
				return nil, agenterrors.NewNetwork("failed retry with max_completion_tokens", retryErr)
			}
			defer retryResp.Body.Close()
			if retryResp.StatusCode != http.StatusOK {
				retryErrBody, _ := io.ReadAll(retryResp.Body)
				auditRespBody = retryErrBody
				formattedErr := formatProviderHTTPError(retryResp.StatusCode, retryResp.Header, retryErrBody)
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, currentModel, false,
					fmt.Sprintf("api_error_%d", retryResp.StatusCode), formattedErr)
				return nil, formattedErr
			}

			rawRetry, readErr := io.ReadAll(retryResp.Body)
			if readErr != nil {
				return nil, agenterrors.NewNetwork("failed to read response", readErr)
			}
			auditRespBody = rawRetry
			retryResponse, err := decodeChatResponseWithCost(bytes.NewReader(rawRetry))
			if err != nil {
				logging.LogRequestPayloadOnError(requestBody, p.config.Name, currentModel, false, "decode_response", err)
				return nil, agenterrors.NewNetwork("failed to decode response", err)
			}
			return retryResponse, nil
		}

		// Log request on API error
		auditRespBody = body
		formattedErr := formatProviderHTTPError(resp.StatusCode, resp.Header, body)
		logging.LogRequestPayloadOnError(sentBody, p.config.Name, currentModel, false,
			fmt.Sprintf("api_error_%d", resp.StatusCode), formattedErr)
		return nil, formattedErr
	}
	defer resp.Body.Close()

	// Decode response (skip logging on success to avoid leaking payloads)
	rawResponse, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to read response", err)
	}
	auditRespBody = rawResponse
	response, err := decodeChatResponseWithCost(bytes.NewReader(rawResponse))
	if err != nil {
		// Log request on decode error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, currentModel, false, "decode_response", err)
		return nil, agenterrors.NewNetwork("failed to decode response", err)
	}

	// Success - don't log the request
	return response, nil
}

// decodeChatResponseWithCost decodes a chat response, then probes raw JSON for cost
// when the canonical typed fields don't include one (see api.CostFromJSON).
func decodeChatResponseWithCost(r io.Reader) (*api.ChatResponse, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var response api.ChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if api.UsageCost(response.Usage) == 0 {
		if cost, ok := api.CostFromJSON(body); ok {
			response.Usage.EstimatedCost = cost
		}
	}
	// Extract image tokens from provider-specific fields
	if response.Usage.ImageTokens == 0 {
		var raw struct {
			Usage struct {
				ImageTokens int `json:"image_tokens"`
				InputImages int `json:"input_images"`
			} `json:"usage"`
		}
		if json.Unmarshal(body, &raw) == nil {
			if raw.Usage.ImageTokens > 0 {
				response.Usage.ImageTokens = raw.Usage.ImageTokens
			} else if raw.Usage.InputImages > 0 {
				response.Usage.ImageTokens = raw.Usage.InputImages
			}
		}
	}
	// Structured reasoning blocks (OpenRouter reasoning_details) live outside
	// the typed core.Message — probe the raw body and stash on Meta.
	if len(response.Choices) > 0 {
		var raw struct {
			Choices []struct {
				Message struct {
					ReasoningDetails []map[string]interface{} `json:"reasoning_details"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(body, &raw) == nil && len(raw.Choices) > 0 && len(raw.Choices[0].Message.ReasoningDetails) > 0 {
			if encoded, err := json.Marshal(raw.Choices[0].Message.ReasoningDetails); err == nil {
				msg := &response.Choices[0].Message
				if msg.Meta == nil {
					msg.Meta = make(map[string]string)
				}
				msg.Meta[api.ReasoningDetailsMetaKey] = string(encoded)
			}
		}
	}
	return &response, nil
}

// SendChatRequestStream is defined in generic_provider_streaming.go

// connDiagnostics carries the httptrace facts about how a provider request's
// transport behaved, so the error record (rel.5 c) can tell a stale-pool
// reset, a long silent prefill, and a mid-stream drop apart.
type connDiagnostics struct {
	// Reused reports whether the connection serving the request was pulled
	// from the keep-alive pool rather than freshly dialed.
	Reused bool
	// RequestStartedAt is when the HTTP request began.
	RequestStartedAt time.Time
}

// freshRetryClientFactory builds the one-shot keep-alive-disabled client a
// stale-connection retry uses. It is a package var (not inline) so tests can
// swap in a client with a controllable transport without dialing a real
// connection.
var freshRetryClientFactory = func(parent *http.Client) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		Timeout: parent.Timeout,
	}
}

// shouldRetryOnFreshConnection is the pure decision the stale-pool retry
// makes: only a failure that happened on a REUSED (pooled) connection,
// before any response byte, is a transport artifact worth an immediate
// fresh-connection retry. A freshly dialed connection failing is a genuine
// network fault and must NOT be masked — it is surfaced to the LLM retry
// loop instead, which applies backoff.
func shouldRetryOnFreshConnection(reused bool, err error) bool {
	return err != nil && reused
}

// networkErrorWithConnDiag builds the network TypedError for a failed request
// and attaches the connection-reuse diagnostics (rel.5 c) to its Details so
// the agent's provider-error record can tell a stale-pool reset apart from a
// fresh-dial fault and a long silent prefill.
func (p *GenericProvider) networkErrorWithConnDiag(msg string, cause error, diag *connDiagnostics) *agenterrors.TypedError {
	err := agenterrors.NewNetwork(msg, cause)
	if diag != nil {
		err = err.WithDetail("conn_reused", diag.Reused).
			WithDetail("conn_request_started_at", diag.RequestStartedAt.UnixMilli())
	}
	return err
}

// executeChatRequest runs client.Do(req) with httptrace reuse tracking and a
// single fresh-connection retry. A keep-alive connection can be silently
// reset by a middlebox (Cloudflare-fronted provider, home NAT) after it sits
// idle past the peer's own idle limit: the transport hands out the pooled
// connection, the first read sees a reset, and the request fails before any
// response byte. That is a transport artifact, not a provider response — so
// when the failed connection was REUSED (a fresh dial failing is a real
// network fault and must not be masked), we retry once, immediately, on a
// connection with keep-alive disabled, without counting it as an attempt.
//
// The fresh retry rebuilds the request from body so the body reader is not
// shared with (and possibly consumed by) the failed first attempt.
func (p *GenericProvider) executeChatRequest(client *http.Client, req *http.Request, body []byte, fresh bool) (*http.Response, *connDiagnostics, error) {
	execClient := client
	if fresh {
		// A one-shot keep-alive-disabled transport so the pooled (possibly
		// stale) connection is never reused. The request is rebuilt from
		// body so its reader is fresh.
		cloned := req.Clone(req.Context())
		if body != nil {
			cloned.Body = io.NopCloser(bytes.NewReader(body))
			cloned.ContentLength = int64(len(body))
		}
		req = cloned
		execClient = freshRetryClientFactory(client)
	}

	d := &connDiagnostics{RequestStartedAt: time.Now()}
	execReq := req
	if !fresh {
		trace := &httptrace.ClientTrace{}
		trace.GotConn = func(ci httptrace.GotConnInfo) {
			d.Reused = ci.Reused
		}
		execReq = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	}

	resp, err := execClient.Do(execReq)
	if err == nil {
		return resp, d, nil
	}
	// A reused-connection failure before any response byte is a stale-pool
	// artifact: retry once on a fresh connection (without counting it as an
	// attempt). A non-reused (freshly dialed) failure is a genuine network
	// fault — propagate it so the LLM retry loop handles it with backoff.
	if shouldRetryOnFreshConnection(d.Reused, err) {
		if freshResp, _, freshErr := p.executeChatRequest(client, req, body, true); freshErr == nil {
			return freshResp, d, nil
		}
	}
	return nil, d, err
}

// CheckConnection tests provider connection with current model
func (p *GenericProvider) CheckConnection() error {
	if err := p.ensureModel(); err != nil {
		return agenterrors.NewNetwork("check connection: failed to ensure model", err)
	}

	// Send a minimal test request to verify the model works
	testMessages := []api.Message{
		{
			Role:    "user",
			Content: "Hi",
		},
	}

	_, err := p.SendChatRequest(context.Background(), testMessages, nil, "", false)
	if err != nil {
		return agenterrors.NewNetwork("check connection: test request failed", err)
	}
	return nil
}

// SetDebug enables or disables debug mode
func (p *GenericProvider) SetDebug(debug bool) {
	p.debug = debug
}

// SetModel sets the current model
func (p *GenericProvider) SetModel(model string) error {
	p.mu.Lock()
	p.model = model
	hadCache := p.modelsCached
	p.modelsCached = false
	p.mu.Unlock()
	p.maxTokensHintMu.Lock()
	p.maxTokensHint = 0
	p.maxTokensHintMu.Unlock()
	// Re-fire the background warm-up so GetModelContextLimit picks up the new model's context_length.
	if hadCache {
		p.warmModelsCache()
	}
	return nil
}

// SetHTTPClient sets the HTTP client used for non-streaming requests.
func (p *GenericProvider) SetHTTPClient(c *http.Client) {
	if c == nil {
		return
	}
	p.mu.Lock()
	p.httpClient = c
	p.mu.Unlock()
}

// SetStreamingClient sets the HTTP client used for streaming requests.
func (p *GenericProvider) SetStreamingClient(c *http.Client) {
	if c == nil {
		return
	}
	p.mu.Lock()
	p.streamingClient = c
	p.mu.Unlock()
}

// RefreshAPIKey re-resolves the API key from the credential store for subsequent requests.
func (p *GenericProvider) RefreshAPIKey() error {
	if p.config == nil {
		return nil
	}
	if p.config.Auth.Type == "none" {
		return nil
	}

	resolved, err := credentials.ResolveProvider(p.config.Name)
	if err != nil {
		return agenterrors.NewNetwork(fmt.Sprintf("failed to re-resolve API key for %q", p.config.Name), err)
	}

	p.config.Auth.Key = resolved.Value
	return nil
}

// GetModel returns the current model
func (p *GenericProvider) GetModel() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.model
}

// GetProvider returns the provider name
func (p *GenericProvider) GetProvider() string {
	return p.config.Name
}

// GetEndpoint returns the API endpoint from the provider config.
func (p *GenericProvider) GetEndpoint() string {
	return p.config.Endpoint
}

// GetHTTPClient returns the HTTP client for non-streaming requests (useful for WASM verification).
func (p *GenericProvider) GetHTTPClient() *http.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.httpClient
}

// GetStreamingClient returns the HTTP client for streaming requests (useful for WASM verification).
func (p *GenericProvider) GetStreamingClient() *http.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.streamingClient
}

// GetModelContextLimit returns the context limit for the current model
func (p *GenericProvider) GetModelContextLimit() (int, error) {
	// 1. Check the cached model list from ListModels()
	p.mu.RLock()
	modelName := p.model
	if p.modelsCached {
		for _, model := range p.models {
			if model.ID == modelName && model.ContextLength > 0 {
				p.mu.RUnlock()
				return model.ContextLength, nil
			}
		}
	}
	p.mu.RUnlock()

	// 2. Consult the published model registry (best-effort; falls through on failure)
	if modelName != "" {
		ctx, cancel := context.WithTimeout(context.Background(), modelregistryFetchTimeout)
		defer cancel()
		if rawModels, err := modelregistry.FetchModels(ctx, p.config.Name); err == nil {
			for _, rm := range rawModels {
				if rm.ID == modelName && rm.ContextLength > 0 {
					return rm.ContextLength, nil
				}
			}
		}
	}

	// 3. Fall back to the static config (model_overrides → pattern_overrides → model_info → default)
	return p.config.GetContextLimit(modelName), nil
}
