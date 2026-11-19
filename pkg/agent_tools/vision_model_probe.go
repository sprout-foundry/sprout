//go:build !js

package tools

import (
	"context"
	"strings"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// vision_model_probe.go — is a configured vision model still served?
//
// Client construction is local: it validates config and resolves credentials,
// so a provider can hand back a client for a model the endpoint retired. The
// vision tier then spends every call on a 404 ("model is no longer available")
// and the caller is told vision is broken when a live model was one step away
// in the candidate list.
//
// The probe is the provider's own model listing — the same call the model
// picker already makes — and it fails open: only a listing that succeeded and
// does not name the model marks it unavailable. No listing, an error, or an
// empty list leaves the model presumed available, so a provider that does not
// serve /models keeps working exactly as before.

const (
	// visionModelListTimeout bounds one provider's listing during a probe.
	visionModelListTimeout = 5 * time.Second
	// visionModelListTTL is how long a listing is reused. Long enough that a
	// caller resolving the vision client repeatedly (per turn) pays one
	// listing per provider, short enough that a provider that restores a
	// model is picked up without a restart.
	visionModelListTTL = 10 * time.Minute
)

// modelListing is the memoized result of one provider's model listing.
type modelListing struct {
	ids     map[string]struct{}
	ok      bool // listing succeeded and was non-empty
	fetched time.Time
}

var visionModelListCache = struct {
	mu     sync.Mutex
	byProv map[string]modelListing
}{byProv: map[string]modelListing{}}

// ResetVisionModelListCacheForTest clears the listing cache so a test can
// change a provider's served models and re-probe.
func ResetVisionModelListCacheForTest() {
	visionModelListCache.mu.Lock()
	defer visionModelListCache.mu.Unlock()
	visionModelListCache.byProv = map[string]modelListing{}
}

// modelListingSource is implemented by clients that can say whether their
// model list came from the provider's own endpoint. A client that does not
// implement it is assumed to return endpoint listings, so the probe behaves
// as before for any client that cannot make the distinction.
type modelListingSource interface {
	ModelsFromEndpoint() bool
}

// listingIsEndpoint reports whether a client's model list reflects the
// provider's own endpoint. A config-derived list restates what the config
// already claims, so it can never confirm a model is still served.
func listingIsEndpoint(client api.ClientInterface) bool {
	if src, ok := client.(modelListingSource); ok {
		return src.ModelsFromEndpoint()
	}
	return true
}

// visionModelServed reports whether a provider currently lists the model.
// It returns true when the answer is unknown (listing failed, empty, came
// from config rather than the endpoint, or the client cannot list) — the
// probe only ever removes a model the provider positively does not serve.
func visionModelServed(ctx context.Context, client api.ClientInterface, model string) bool {
	if client == nil || model == "" {
		return true
	}
	provider := strings.TrimSpace(client.GetProvider())
	if provider == "" {
		return true
	}

	if listing, ok := cachedModelListing(provider); ok {
		return listingAllows(listing, model)
	}

	listing := fetchModelListing(ctx, client)
	storeModelListing(provider, listing)
	return listingAllows(listing, model)
}

// listingAllows applies the fail-open rule to one listing.
func listingAllows(listing modelListing, model string) bool {
	if !listing.ok {
		return true
	}
	_, found := listing.ids[strings.ToLower(strings.TrimSpace(model))]
	return found
}

func cachedModelListing(provider string) (modelListing, bool) {
	visionModelListCache.mu.Lock()
	defer visionModelListCache.mu.Unlock()
	listing, ok := visionModelListCache.byProv[provider]
	if !ok || time.Since(listing.fetched) > visionModelListTTL {
		return modelListing{}, false
	}
	return listing, true
}

func storeModelListing(provider string, listing modelListing) {
	visionModelListCache.mu.Lock()
	defer visionModelListCache.mu.Unlock()
	visionModelListCache.byProv[provider] = listing
}

// fetchModelListing asks the provider for its models. Any failure — an
// error, an empty list, or a list synthesized from config rather than the
// endpoint — yields a listing that allows every model, because only the
// provider's own answer can refute a configured model.
func fetchModelListing(ctx context.Context, client api.ClientInterface) modelListing {
	listCtx, cancel := context.WithTimeout(ctx, visionModelListTimeout)
	defer cancel()

	models, err := client.ListModels(listCtx)
	if err != nil || len(models) == 0 {
		return modelListing{fetched: time.Now()}
	}
	// A config fallback (DisableDiscovery, a failed or unauthorized listing)
	// returns the config's own model_info, which would "confirm" the model
	// against the config that declared it. Treat that as unknown.
	if !listingIsEndpoint(client) {
		return modelListing{fetched: time.Now()}
	}
	ids := make(map[string]struct{}, len(models))
	for _, m := range models {
		if id := strings.ToLower(strings.TrimSpace(m.ID)); id != "" {
			ids[id] = struct{}{}
		}
	}
	return modelListing{ids: ids, ok: len(ids) > 0, fetched: time.Now()}
}
