package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// ---------------------------------------------------------------------------
// canonicalToConfigEntry
// ---------------------------------------------------------------------------

func TestCanonicalToConfigEntry_Minimal(t *testing.T) {
	m := modelcontract.CanonicalModel{
		ID:            "test-model",
		ContextWindow: 128000,
	}
	entry := canonicalToConfigEntry(m)
	if entry["id"] != "test-model" {
		t.Errorf("id = %v, want test-model", entry["id"])
	}
	if entry["context_length"].(int) != 128000 {
		t.Errorf("context_length = %v, want 128000", entry["context_length"])
	}
	if entry["name"] != "test-model" {
		t.Errorf("name should default to id, got %v", entry["name"])
	}
}

func TestCanonicalToConfigEntry_FullMetadata(t *testing.T) {
	streaming := true
	m := modelcontract.CanonicalModel{
		ID:            "gpt-test",
		DisplayName:   "GPT Test",
		Description:   "Test model",
		ContextWindow: 200000,
		Pricing: &modelcontract.Pricing{
			InputPerMTok:  1.0,
			OutputPerMTok: 5.0,
			CachedPerMTok: 0.1,
			Estimated:     true,
		},
		Capabilities: modelcontract.Capabilities{
			Tools:     modelcontract.Bool(true),
			Vision:    modelcontract.Bool(true),
			Streaming: &streaming,
		},
	}
	entry := canonicalToConfigEntry(m)
	if entry["name"] != "GPT Test" {
		t.Errorf("name = %v, want GPT Test", entry["name"])
	}
	if entry["description"] != "Test model" {
		t.Errorf("description = %v", entry["description"])
	}
	if entry["input_cost"].(float64) != 1.0 {
		t.Errorf("input_cost = %v", entry["input_cost"])
	}
	if entry["output_cost"].(float64) != 5.0 {
		t.Errorf("output_cost = %v", entry["output_cost"])
	}
	if entry["cached_input_cost"].(float64) != 0.1 {
		t.Errorf("cached_input_cost = %v", entry["cached_input_cost"])
	}
	tags, ok := entry["tags"].([]string)
	if !ok || len(tags) != 2 {
		t.Errorf("expected 2 tags [tools vision], got %v", entry["tags"])
	}
}

func TestCanonicalToConfigEntry_NoCachedCostWhenZero(t *testing.T) {
	m := modelcontract.CanonicalModel{
		ID: "test",
		Pricing: &modelcontract.Pricing{
			InputPerMTok:  1.0,
			OutputPerMTok: 2.0,
		},
	}
	entry := canonicalToConfigEntry(m)
	if _, has := entry["cached_input_cost"]; has {
		t.Error("should not include cached_input_cost when 0")
	}
}

func TestCanonicalToConfigEntry_NilPricing(t *testing.T) {
	m := modelcontract.CanonicalModel{
		ID: "test",
	}
	entry := canonicalToConfigEntry(m)
	if _, has := entry["input_cost"]; has {
		t.Error("should not include input_cost when pricing is nil")
	}
	if _, has := entry["output_cost"]; has {
		t.Error("should not include output_cost when pricing is nil")
	}
}

// ---------------------------------------------------------------------------
// addModelsToConfig
// ---------------------------------------------------------------------------

func TestAddModelsToConfig_AddsNewModel(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "existing", InputCost: 1.0, OutputCost: 2.0},
	})
	path := filepath.Join(dir, "p.json")
	newModels := []modelcontract.CanonicalModel{
		{ID: "new-model", DisplayName: "New Model", ContextWindow: 128000,
			Pricing: &modelcontract.Pricing{InputPerMTok: 0.5, OutputPerMTok: 1.5}},
	}
	n, err := addModelsToConfig(path, newModels)
	if err != nil {
		t.Fatalf("addModelsToConfig: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 added, got %d", n)
	}
	cfg, err := providers.LoadProviderConfig(path)
	if err != nil {
		t.Fatalf("LoadProviderConfig: %v", err)
	}
	if len(cfg.Models.ModelInfo) != 2 {
		t.Fatalf("expected 2 models, got %d", len(cfg.Models.ModelInfo))
	}
	// Original model preserved.
	if cfg.Models.ModelInfo[0].ID != "existing" {
		t.Errorf("original model lost: %v", cfg.Models.ModelInfo[0].ID)
	}
	if cfg.Models.ModelInfo[0].InputCost != 1.0 {
		t.Errorf("original pricing changed: %f", cfg.Models.ModelInfo[0].InputCost)
	}
	// New model present.
	if cfg.Models.ModelInfo[1].ID != "new-model" {
		t.Errorf("new model missing: %v", cfg.Models.ModelInfo[1].ID)
	}
}

func TestAddModelsToConfig_DoesNotDuplicateExisting(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "existing", InputCost: 1.0, OutputCost: 2.0},
	})
	path := filepath.Join(dir, "p.json")
	newModels := []modelcontract.CanonicalModel{
		{ID: "existing", DisplayName: "Should not duplicate"},
	}
	n, err := addModelsToConfig(path, newModels)
	if err != nil {
		t.Fatalf("addModelsToConfig: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 added, got %d", n)
	}
	cfg, _ := providers.LoadProviderConfig(path)
	if len(cfg.Models.ModelInfo) != 1 {
		t.Errorf("expected 1 model, got %d", len(cfg.Models.ModelInfo))
	}
}

func TestAddModelsToConfig_EmptyInput(t *testing.T) {
	n, err := addModelsToConfig("/dev/null", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 added for empty input, got %d", n)
	}
}

func TestAddModelsToConfig_PreservesAvailableModels(t *testing.T) {
	dir := t.TempDir()
	cfg := providers.ProviderConfig{
		Name: "p", Endpoint: "https://api.example.com/v1",
		Auth: providers.AuthConfig{Type: "none"},
		Models: providers.ModelConfig{
			DefaultContextLimit: 100000,
			ModelInfo: []providers.ModelInfo{
				{ID: "m1", InputCost: 1.0, OutputCost: 2.0},
			},
			AvailableModels: []string{"m1"},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	path := filepath.Join(dir, "p.json")
	os.WriteFile(path, append(data, '\n'), 0o644)

	newModels := []modelcontract.CanonicalModel{
		{ID: "m2", ContextWindow: 200000,
			Pricing: &modelcontract.Pricing{InputPerMTok: 0.5, OutputPerMTok: 1.5}},
	}
	n, err := addModelsToConfig(path, newModels)
	if err != nil {
		t.Fatalf("addModelsToConfig: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 added, got %d", n)
	}
	loaded, _ := providers.LoadProviderConfig(path)
	if len(loaded.Models.AvailableModels) != 2 {
		t.Errorf("expected 2 available_models, got %d", len(loaded.Models.AvailableModels))
	}
	if loaded.Models.AvailableModels[1] != "m2" {
		t.Errorf("new model not in available_models: %v", loaded.Models.AvailableModels)
	}
}

func TestAddModelsToConfig_DoesNotExtendEmptyAvailableModels(t *testing.T) {
	dir := t.TempDir()
	cfg := providers.ProviderConfig{
		Name: "p", Endpoint: "https://api.example.com/v1",
		Auth: providers.AuthConfig{Type: "none"},
		Models: providers.ModelConfig{
			DefaultContextLimit: 100000,
			ModelInfo: []providers.ModelInfo{
				{ID: "m1"},
			},
			AvailableModels: []string{}, // empty — live-discovery
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	path := filepath.Join(dir, "p.json")
	os.WriteFile(path, append(data, '\n'), 0o644)

	newModels := []modelcontract.CanonicalModel{
		{ID: "m2", ContextWindow: 200000},
	}
	addModelsToConfig(path, newModels)
	loaded, _ := providers.LoadProviderConfig(path)
	if len(loaded.Models.AvailableModels) != 0 {
		t.Errorf("empty available_models should stay empty (live-discovery), got %v",
			loaded.Models.AvailableModels)
	}
}

func TestAddModelsToConfig_MultipleNewModels(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "existing"},
	})
	path := filepath.Join(dir, "p.json")
	newModels := []modelcontract.CanonicalModel{
		{ID: "new1", ContextWindow: 100000},
		{ID: "new2", ContextWindow: 200000},
		{ID: "existing"}, // should be skipped
	}
	n, err := addModelsToConfig(path, newModels)
	if err != nil {
		t.Fatalf("addModelsToConfig: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 added (existing skipped), got %d", n)
	}
	cfg, _ := providers.LoadProviderConfig(path)
	if len(cfg.Models.ModelInfo) != 3 {
		t.Errorf("expected 3 total models, got %d", len(cfg.Models.ModelInfo))
	}
}

func TestAddModelsToConfig_ProducesValidJSON(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "p", []providers.ModelInfo{
		{ID: "m1"},
	})
	path := filepath.Join(dir, "p.json")
	newModels := []modelcontract.CanonicalModel{
		{ID: "m2", ContextWindow: 200000,
			Pricing: &modelcontract.Pricing{InputPerMTok: 1.0, OutputPerMTok: 2.0}},
	}
	if _, err := addModelsToConfig(path, newModels); err != nil {
		t.Fatalf("addModelsToConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if data[len(data)-1] != '\n' {
		t.Error("missing trailing newline")
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("invalid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// addModelsToManifest
// ---------------------------------------------------------------------------

func TestAddModelsToManifest_AddsNewProvider(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	// Start with an empty manifest.
	os.WriteFile(manifestPath, []byte("{}"), 0o644)

	newModels := []modelcontract.CanonicalModel{
		{ID: "m1", Pricing: &modelcontract.Pricing{InputPerMTok: 1.0, OutputPerMTok: 2.0}},
	}
	n, err := addModelsToManifest(manifestPath, "newprovider", newModels)
	if err != nil {
		t.Fatalf("addModelsToManifest: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 added, got %d", n)
	}

	data, _ := os.ReadFile(manifestPath)
	var m map[string]ProviderManifest
	json.Unmarshal(data, &m)
	pm, ok := m["newprovider"]
	if !ok {
		t.Fatal("newprovider not in manifest")
	}
	if len(pm.Models) != 1 || pm.Models[0].ID != "m1" {
		t.Errorf("unexpected models: %v", pm.Models)
	}
	if pm.Models[0].InputPerMTok != 1.0 {
		t.Errorf("input price wrong: %f", pm.Models[0].InputPerMTok)
	}
}

func TestAddModelsToManifest_AddsToExistingProvider(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	existing := map[string]ProviderManifest{
		"p": {
			Source:       "https://example.com",
			LastVerified: "2026-01-01",
			Models: []PricingEntry{
				{ID: "old", InputPerMTok: 1.0, OutputPerMTok: 2.0},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(manifestPath, append(data, '\n'), 0o644)

	newModels := []modelcontract.CanonicalModel{
		{ID: "new", Pricing: &modelcontract.Pricing{InputPerMTok: 3.0, OutputPerMTok: 6.0}},
	}
	n, err := addModelsToManifest(manifestPath, "p", newModels)
	if err != nil {
		t.Fatalf("addModelsToManifest: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 added, got %d", n)
	}

	data, _ = os.ReadFile(manifestPath)
	var m map[string]ProviderManifest
	json.Unmarshal(data, &m)
	if len(m["p"].Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(m["p"].Models))
	}
}

func TestAddModelsToManifest_DoesNotDuplicate(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	existing := map[string]ProviderManifest{
		"p": {Models: []PricingEntry{{ID: "m1", InputPerMTok: 1.0}}},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(manifestPath, append(data, '\n'), 0o644)

	newModels := []modelcontract.CanonicalModel{
		{ID: "m1", Pricing: &modelcontract.Pricing{InputPerMTok: 99.0}},
	}
	n, err := addModelsToManifest(manifestPath, "p", newModels)
	if err != nil {
		t.Fatalf("addModelsToManifest: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 added (duplicate skipped), got %d", n)
	}
}

func TestAddModelsToManifest_EmptyInput(t *testing.T) {
	n, err := addModelsToManifest("/dev/null", "p", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 added for empty input, got %d", n)
	}
}

func TestAddModelsToManifest_NilPricing(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	os.WriteFile(manifestPath, []byte("{}"), 0o644)

	newModels := []modelcontract.CanonicalModel{
		{ID: "m1"}, // no pricing
	}
	n, err := addModelsToManifest(manifestPath, "p", newModels)
	if err != nil {
		t.Fatalf("addModelsToManifest: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 added, got %d", n)
	}

	data, _ := os.ReadFile(manifestPath)
	var m map[string]ProviderManifest
	json.Unmarshal(data, &m)
	if pm := m["p"]; len(pm.Models) != 1 || pm.Models[0].InputPerMTok != 0 {
		t.Errorf("expected 0 pricing for nil-pricing model, got %v", pm.Models[0])
	}
}

// ---------------------------------------------------------------------------
// formatDiscoverReport
// ---------------------------------------------------------------------------

func TestFormatDiscoverReport_NewModels(t *testing.T) {
	results := []DiscoverResult{
		{ProviderID: "p", NewModels: []modelcontract.CanonicalModel{
			{ID: "new1", ContextWindow: 128000,
				Pricing: &modelcontract.Pricing{InputPerMTok: 1.0, OutputPerMTok: 2.0}},
		}},
	}
	report := formatDiscoverReport(results)
	if !contains(report, "p") || !contains(report, "new1") || !contains(report, "+ new1") {
		t.Errorf("report missing expected content: %s", report)
	}
	if !contains(report, "Discovery: 1 new") {
		t.Errorf("summary missing: %s", report)
	}
}

func TestFormatDiscoverReport_StaleModels(t *testing.T) {
	results := []DiscoverResult{
		{ProviderID: "p", StaleModels: []string{"old-model"}},
	}
	report := formatDiscoverReport(results)
	if !contains(report, "- old-model") || !contains(report, "not in live API") {
		t.Errorf("stale model not in report: %s", report)
	}
	if !contains(report, "1 stale") {
		t.Errorf("stale count missing: %s", report)
	}
}

func TestFormatDiscoverReport_APIError(t *testing.T) {
	results := []DiscoverResult{
		{ProviderID: "p", APIError: "HTTP 401"},
	}
	report := formatDiscoverReport(results)
	if !contains(report, "API ERROR") || !contains(report, "HTTP 401") {
		t.Errorf("API error not in report: %s", report)
	}
	if !contains(report, "1 errors") {
		t.Errorf("error count missing: %s", report)
	}
}

func TestFormatDiscoverReport_CleanProvider(t *testing.T) {
	results := []DiscoverResult{
		{ProviderID: "p"}, // no new, no stale, no error
	}
	report := formatDiscoverReport(results)
	if contains(report, "\np\n") {
		t.Errorf("clean provider should not appear in report: %s", report)
	}
}

func TestFormatDiscoverReport_EstimatedFlag(t *testing.T) {
	results := []DiscoverResult{
		{ProviderID: "p", NewModels: []modelcontract.CanonicalModel{
			{ID: "m1", Pricing: &modelcontract.Pricing{Estimated: true, InputPerMTok: 1.0}},
		}},
	}
	report := formatDiscoverReport(results)
	if !contains(report, "[estimated]") {
		t.Errorf("estimated flag missing: %s", report)
	}
}

func TestFormatDiscoverReport_EmptyResults(t *testing.T) {
	report := formatDiscoverReport(nil)
	if !contains(report, "Discovery: 0 new, 0 stale, 0 errors") {
		t.Errorf("empty report summary wrong: %s", report)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
