//go:build !js

package tools

import (
	"context"
	"errors"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// vision_model_probe_test.go — a configured vision model the provider no
// longer serves must not be selected, and the failure must name it.

// listingClient is a vision-capable client whose model listing is scripted.
// A nil listErr with a nil slice models "listing returned nothing".
type listingClient struct {
	provider string
	model    string
	ids      []string
	listErr  error
}

func (c *listingClient) SendChatRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}

func (c *listingClient) SendChatRequestStream(context.Context, []api.Message, []api.Tool, string, bool, api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}

func (c *listingClient) SendVisionRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{}, nil
}

func (c *listingClient) CheckConnection() error                     { return nil }
func (c *listingClient) SetDebug(bool)                              {}
func (c *listingClient) SetModel(model string) error                { c.model = model; return nil }
func (c *listingClient) GetModel() string                           { return c.model }
func (c *listingClient) GetProvider() string                        { return c.provider }
func (c *listingClient) GetModelContextLimit() (int, error)         { return 0, nil }
func (c *listingClient) SupportsVision() bool                       { return true }
func (c *listingClient) VisionCapabilities() api.VisionCapabilities { return api.VisionCapabilities{} }
func (c *listingClient) GetVisionModel() string                     { return c.model }
func (c *listingClient) GetLastTPS() float64                        { return 0 }
func (c *listingClient) GetAverageTPS() float64                     { return 0 }
func (c *listingClient) GetTPSStats() map[string]float64            { return nil }
func (c *listingClient) ResetTPSStats()                             {}

func (c *listingClient) ListModels(context.Context) ([]api.ModelInfo, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	out := make([]api.ModelInfo, len(c.ids))
	for i, id := range c.ids {
		out[i] = api.ModelInfo{ID: id}
	}
	return out, nil
}

// configFallbackClient reports that its listing came from config, not the
// provider's endpoint — the shape GenericProvider takes when discovery is
// disabled or the endpoint listing failed.
type configFallbackClient struct {
	listingClient
}

func (c *configFallbackClient) ModelsFromEndpoint() bool { return false }

func TestVisionModelServed_RetiredModelIsRejected(t *testing.T) {
	ResetVisionModelListCacheForTest()
	t.Cleanup(ResetVisionModelListCacheForTest)

	client := &listingClient{
		provider: "retired-provider",
		model:    "retired-vision-model",
		ids:      []string{"live-text-model", "live-vision-model"},
	}
	if visionModelServed(context.Background(), client, "retired-vision-model") {
		t.Fatal("a model the provider no longer lists must not read as served")
	}
	if !visionModelServed(context.Background(), client, "live-vision-model") {
		t.Fatal("a listed model must read as served")
	}
}

func TestVisionModelServed_ConfigFallbackIsNotAConfirmation(t *testing.T) {
	ResetVisionModelListCacheForTest()
	t.Cleanup(ResetVisionModelListCacheForTest)

	// A client whose listing came from config, not the endpoint, returns the
	// model it was configured with. That restates the config's own claim and
	// must not be read as "the provider still serves it" — otherwise the
	// probe would confirm a retired model against the config that pinned it.
	client := &configFallbackClient{
		listingClient: listingClient{
			provider: "fallback-provider",
			model:    "retired-vision-model",
			ids:      []string{"retired-vision-model"},
		},
	}
	if !visionModelServed(context.Background(), client, "retired-vision-model") {
		t.Fatal("a config-derived listing must fail open, not confirm the model")
	}

	// The same listing from a real endpoint IS a confirmation.
	ResetVisionModelListCacheForTest()
	endpoint := &listingClient{
		provider: "endpoint-provider",
		model:    "retired-vision-model",
		ids:      []string{"retired-vision-model"},
	}
	if !visionModelServed(context.Background(), endpoint, "retired-vision-model") {
		t.Fatal("a real endpoint listing that names the model must read as served")
	}
}

func TestVisionModelServed_ClientWithoutSourceIsTrusted(t *testing.T) {
	ResetVisionModelListCacheForTest()
	t.Cleanup(ResetVisionModelListCacheForTest)

	// A client that cannot report its listing source keeps the prior
	// behavior: its listing is taken at face value.
	client := &listingClient{
		provider: "opaque-provider",
		model:    "gone-model",
		ids:      []string{"other-model"},
	}
	if visionModelServed(context.Background(), client, "gone-model") {
		t.Fatal("an opaque client's listing must still be able to refute a model")
	}
}

func TestVisionModelServed_FailsOpen(t *testing.T) {
	t.Cleanup(ResetVisionModelListCacheForTest)

	cases := map[string]api.ClientInterface{
		"listing errors": &listingClient{provider: "err-provider", ids: nil, listErr: errors.New("HTTP 500")},
		"empty listing":  &listingClient{provider: "empty-provider", ids: nil},
		"nil client":     nil,
	}
	for name, client := range cases {
		t.Run(name, func(t *testing.T) {
			ResetVisionModelListCacheForTest()
			if !visionModelServed(context.Background(), client, "any-model") {
				t.Fatalf("%s must fail open — an unknown answer never disables a model", name)
			}
		})
	}
}

func TestVisionModelServed_CachesListing(t *testing.T) {
	ResetVisionModelListCacheForTest()
	t.Cleanup(ResetVisionModelListCacheForTest)

	client := &listingClient{provider: "cache-provider", ids: []string{"model-a"}}
	if !visionModelServed(context.Background(), client, "model-a") {
		t.Fatal("model-a should be served")
	}

	// A second client for the same provider reuses the cached listing, so a
	// newly listed model stays unknown until the TTL expires.
	second := &listingClient{provider: "cache-provider", ids: []string{"model-a", "model-b"}}
	if visionModelServed(context.Background(), second, "model-b") {
		t.Fatal("listing must be memoized per provider for the TTL")
	}
}

func TestVisionModelServed_ModelMatchIsCaseInsensitive(t *testing.T) {
	ResetVisionModelListCacheForTest()
	t.Cleanup(ResetVisionModelListCacheForTest)

	client := &listingClient{provider: "case-provider", ids: []string{"MiniMax-VL-01"}}
	if !visionModelServed(context.Background(), client, " minimax-vl-01 ") {
		t.Fatal("model identity must match case- and whitespace-insensitively")
	}
}

// TestVisionModelServed_RetiredPinIsNotServed pins the operator-facing half of
// the fix: a retired pinned model must not read as served, so a provider's own
// vision resolution does not promise a model its endpoint dropped.
func TestVisionModelServed_RetiredPinIsNotServed(t *testing.T) {
	ResetVisionModelListCacheForTest()
	t.Cleanup(ResetVisionModelListCacheForTest)

	client := &listingClient{provider: "names-pin-provider", model: "retired-vision-model", ids: []string{"live-model"}}
	// The retired model must not be selected even though the client is
	// vision-capable by declaration.
	if visionModelServed(context.Background(), client, client.model) {
		t.Fatal("retired model must not read as served")
	}
	// A model the provider does list still reads as served.
	if !visionModelServed(context.Background(), client, "live-model") {
		t.Fatal("a listed model must read as served")
	}
}
