package localmodel

import (
	"context"
	"reflect"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestLocalModelsProviderServesFullCatalog pins what the WebUI model picker
// sees for sprout-local. The hook must serve TieredModelInfos — the pure-Go
// RAM-tier catalog — not GetLocalProvider().ListModels: in a cgo-less build
// the provider singleton is the stub whose ListModels returns (nil, nil),
// which GetModelsForProviderCtx reports as a valid empty list. That left the
// picker empty while the settings tab (independent catalog read) showed
// every model — the two surfaces disagreed on exactly the machines running
// the cross-compiled release binaries.
func TestLocalModelsProviderServesFullCatalog(t *testing.T) {
	if api.LocalModelsProvider == nil {
		t.Fatal("api.LocalModelsProvider not registered — pkg/localmodel init() did not run")
	}

	models, err := api.LocalModelsProvider(context.Background())
	if err != nil {
		t.Fatalf("LocalModelsProvider: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("LocalModelsProvider returned 0 models — picker would show an empty list")
	}

	want := TieredModelInfos(TotalSystemRAM())
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("LocalModelsProvider output diverges from TieredModelInfos: got %d models, want %d", len(models), len(want))
	}

	// Catalog name is the stable selection ID everywhere else (/model,
	// SetModel, ResolveModelID) — every row must carry one.
	for _, m := range models {
		if m.ID == "" {
			t.Errorf("model %q has empty ID", m.Name)
		}
	}
}
