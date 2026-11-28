package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// generic_provider_conn_check.go — the connection check's two paths and the
// process-wide success cache. The full-chat "Hi" probe this replaces cost a
// whole generation (thinking included) plus a server lane on every agent
// creation; the bounded, non-generating check is the default.

// connCheckCacheTTL is how long a successful check is trusted for the same
// provider before re-probing — agent creation creates a fresh provider per
// session, so without this every agent/sub-agent re-runs the probe.
const connCheckCacheTTL = 5 * time.Minute

// connCheckModelsTimeout bounds the GET /models probe.
const connCheckModelsTimeout = 10 * time.Second

var (
	connCheckCacheMu sync.Mutex
	connCheckCache   = map[string]time.Time{}
)

// connCheckIdentity identifies a provider for the success cache: name +
// endpoint + model. The check verifies a specific model works, so a model
// switch must re-probe even on a recently-verified endpoint.
func (p *GenericProvider) connCheckIdentity() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config.Name + "|" + p.config.Endpoint + "|" + p.model
}

func connCheckSucceededRecently(identity string) bool {
	connCheckCacheMu.Lock()
	defer connCheckCacheMu.Unlock()
	succ, ok := connCheckCache[identity]
	if !ok {
		return false
	}
	if time.Since(succ) > connCheckCacheTTL {
		delete(connCheckCache, identity)
		return false
	}
	return true
}

func connCheckMarkSuccess(identity string) {
	connCheckCacheMu.Lock()
	defer connCheckCacheMu.Unlock()
	connCheckCache[identity] = time.Now()
}

// ResetConnCheckCacheForTests clears the success cache. Test-only.
func ResetConnCheckCacheForTests() {
	connCheckCacheMu.Lock()
	defer connCheckCacheMu.Unlock()
	connCheckCache = map[string]time.Time{}
}

// checkViaModelsEndpoint probes GET /models and confirms the configured
// model is in the list. It does NOT fall back to config when the probe
// fails — this is a strict probe; the caller falls back to the bounded chat
// check instead.
func (p *GenericProvider) checkViaModelsEndpoint() error {
	p.mu.RLock()
	endpoint := p.config.Endpoint
	client := p.httpClient
	currentModel := p.model
	p.mu.RUnlock()

	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("no endpoint configured")
	}

	modelsEndpoint := strings.TrimSuffix(endpoint, "/chat/completions") + "/models"
	ctx, cancel := context.WithTimeout(context.Background(), connCheckModelsTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", modelsEndpoint, nil)
	if err != nil {
		return agenterrors.NewNetwork("check connection: invalid models endpoint", err)
	}

	token, err := p.config.GetAuthToken()
	if err != nil {
		if strings.Contains(endpoint, "127.0.0.1") || strings.Contains(endpoint, "localhost") {
			// No auth needed for local instances
		} else {
			return agenterrors.NewNetwork("check connection: models probe needs auth", err)
		}
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return agenterrors.NewNetwork("check connection: models probe failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agenterrors.NewNetwork(fmt.Sprintf("check connection: models probe got HTTP %d", resp.StatusCode), nil)
	}

	var modelsResponse struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return agenterrors.NewNetwork("check connection: models probe decode failed", err)
	}

	// Compare canonicalized IDs: the request path canonicalizes the model
	// before sending, so an alias must match the catalog's form here too —
	// otherwise alias configs always miss and fall through to the chat check.
	want := p.canonicalizeModelID(strings.TrimSpace(currentModel))
	for _, m := range modelsResponse.Data {
		if strings.TrimSpace(m.ID) == want {
			return nil
		}
	}
	return agenterrors.NewNetwork(fmt.Sprintf("check connection: model %s not in /models response", want), nil)
}

// checkViaBoundedChat is the fallback check: a single "Hi" chat request with
// max_tokens: 1 and thinking disabled, so it cannot cost more than a token
// of output or a lane held for a full reply.
func (p *GenericProvider) checkViaBoundedChat() error {
	ensureErr := p.ensureModel()
	if ensureErr != nil {
		return agenterrors.NewNetwork("check connection: failed to ensure model", ensureErr)
	}

	testMessages := []api.Message{
		{
			Role:    "user",
			Content: "Hi",
		},
	}

	p.SetMaxTokensHint(1)
	defer p.SetMaxTokensHint(0)

	_, err := p.SendChatRequest(context.Background(), testMessages, nil, "", true)
	if err != nil {
		return agenterrors.NewNetwork("check connection: test request failed", err)
	}
	return nil
}

// CheckConnection tests provider connection with current model. It prefers a
// non-generating check (GET /models with the configured model present) and
// falls back to a bounded chat request (max_tokens: 1, thinking disabled).
// A successful check is cached for a short TTL per process/provider.
func (p *GenericProvider) CheckConnection() error {
	if err := p.ensureModel(); err != nil {
		return agenterrors.NewNetwork("check connection: failed to ensure model", err)
	}

	identity := p.connCheckIdentity()
	if connCheckSucceededRecently(identity) {
		return nil
	}

	if !p.config.Models.DisableDiscovery {
		if err := p.checkViaModelsEndpoint(); err == nil {
			connCheckMarkSuccess(identity)
			return nil
		}
		// The probe failed — fall through to the bounded chat check. Its
		// error (the real connection test) is the one reported.
	}
	if err := p.checkViaBoundedChat(); err != nil {
		return err
	}
	connCheckMarkSuccess(identity)
	return nil
}
