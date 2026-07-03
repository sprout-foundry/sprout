package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// TestCarryForwardProbeData verifies that refresh_provider_catalog preserves
// probe + RecommendedRoles across a refresh. The CI's enrich_registry step
// handles the live-baseline merge; this is the local-file guarantee so running
// refresh_provider_catalog alone (locally, in a PR, in a debug workflow) does
// not silently drop probe data.
func TestCarryForwardProbeData(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	prior := modelcontract.ProviderFile{
		SchemaVersion: 2,
		Provider:      "openrouter",
		Models: []modelcontract.CanonicalModel{
			{
				ID: "openai/gpt-4o",
				Probe: &modelcontract.ProbeResult{
					Passed: true, Complex: true, Score: 0.95,
					LastProbedAt: "2026-06-20T00:00:00Z", ProbeVersion: "gates+todos-v5",
				},
				RecommendedRoles: []string{"primary", "subagent"},
			},
			{
				ID: "anthropic/claude-sonnet-4",
				Probe: &modelcontract.ProbeResult{
					Passed: true, Complex: false, Score: 0.6,
					LastProbedAt: "2026-06-20T00:00:00Z", ProbeVersion: "gates+todos-v5",
				},
				RecommendedRoles: []string{"subagent"},
			},
			{
				ID: "old/legacy-model",
				Probe: &modelcontract.ProbeResult{
					Passed: false, Complex: false, Score: 0.2,
				},
				RecommendedRoles: nil, // failed probe → no recommendation
			},
		},
	}
	priorBytes, err := json.MarshalIndent(prior, "", "  ")
	if err != nil {
		t.Fatalf("marshal prior: %v", err)
	}
	priorPath := filepath.Join(modelsDir, "openrouter.json")
	if err := os.WriteFile(priorPath, priorBytes, 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	// Fresh canonical list (e.g. from adapter): includes the two surviving
	// models plus a brand-new model. None carry probe data — that's the bug.
	fresh := []modelcontract.CanonicalModel{
		{ID: "openai/gpt-4o", ContextWindow: 128000},
		{ID: "anthropic/claude-sonnet-4", ContextWindow: 200000},
		{ID: "newcomer/just-launched", ContextWindow: 256000},
	}

	out := carryForwardProbeData(dir, "openrouter", fresh)

	if len(out) != 3 {
		t.Fatalf("expected 3 models in output, got %d", len(out))
	}

	// gpt-4o: probe carried forward with full detail
	if out[0].Probe == nil {
		t.Errorf("gpt-4o: Probe is nil after carry-forward")
	} else if !out[0].Probe.Passed || !out[0].Probe.Complex {
		t.Errorf("gpt-4o: Probe fields lost in carry-forward: %+v", out[0].Probe)
	}
	if got := out[0].RecommendedRoles; len(got) != 2 || got[0] != "primary" || got[1] != "subagent" {
		t.Errorf("gpt-4o: RecommendedRoles = %v, want [primary subagent]", got)
	}

	// claude-sonnet-4: subagent-only recommendation carried forward
	if out[1].Probe == nil || !out[1].Probe.Passed || out[1].Probe.Complex {
		t.Errorf("claude-sonnet-4: probe not preserved correctly: %+v", out[1].Probe)
	}
	if got := out[1].RecommendedRoles; len(got) != 1 || got[0] != "subagent" {
		t.Errorf("claude-sonnet-4: RecommendedRoles = %v, want [subagent]", got)
	}

	// newcomer: not in prior file → no probe data stamped
	if out[2].Probe != nil {
		t.Errorf("newcomer: should not have probe data (not in prior), got %+v", out[2].Probe)
	}
	if len(out[2].RecommendedRoles) != 0 {
		t.Errorf("newcomer: should not have RecommendedRoles, got %v", out[2].RecommendedRoles)
	}
}

// TestCarryForwardProbeData_NoPriorFile verifies the no-op path: when there's
// no prior per-provider file, fresh models are returned untouched.
func TestCarryForwardProbeData_NoPriorFile(t *testing.T) {
	dir := t.TempDir()
	// Intentionally do NOT create models/<provider>.json.
	fresh := []modelcontract.CanonicalModel{
		{ID: "some/model", ContextWindow: 100000},
	}
	out := carryForwardProbeData(dir, "openrouter", fresh)
	if len(out) != 1 || out[0].ID != "some/model" {
		t.Fatalf("expected fresh passthrough, got %+v", out)
	}
	if out[0].Probe != nil || len(out[0].RecommendedRoles) != 0 {
		t.Errorf("fresh model should not gain probe data when no prior file exists")
	}
}

// TestCarryForwardProbeData_CorruptPriorFile verifies that a malformed prior
// file is logged and ignored (the refresh continues with fresh data only).
func TestCarryForwardProbeData_CorruptPriorFile(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	priorPath := filepath.Join(modelsDir, "openrouter.json")
	if err := os.WriteFile(priorPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt prior: %v", err)
	}

	fresh := []modelcontract.CanonicalModel{{ID: "some/model", ContextWindow: 100000}}
	out := carryForwardProbeData(dir, "openrouter", fresh)

	if len(out) != 1 || out[0].Probe != nil {
		t.Errorf("corrupt prior should be ignored; got %+v", out[0].Probe)
	}
}

// TestCarryForwardProbeData_PreservesFreshFields verifies the carry-forward
// only adds Probe + RecommendedRoles; everything else (context, tags, pricing)
// comes from the fresh adapter output and is left alone.
func TestCarryForwardProbeData_PreservesFreshFields(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	prior := modelcontract.ProviderFile{
		SchemaVersion: 2,
		Provider:      "openrouter",
		Models: []modelcontract.CanonicalModel{
			{
				ID: "openai/gpt-4o",
				Probe:            &modelcontract.ProbeResult{Passed: true, Complex: true},
				RecommendedRoles: []string{"primary", "subagent"},
				// ContextWindow left zero in the prior — should NOT overwrite the
				// fresh value via the carry-forward (Probe + RecommendedRoles only).
			},
		},
	}
	priorBytes, _ := json.MarshalIndent(prior, "", "  ")
	if err := os.WriteFile(filepath.Join(modelsDir, "openrouter.json"), priorBytes, 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	fresh := []modelcontract.CanonicalModel{
		{
			ID:           "openai/gpt-4o",
			ContextWindow: 128000, // fresh, must be preserved
			Capabilities: modelcontract.Capabilities{Tools: modelcontract.Bool(true)},
		},
	}
	out := carryForwardProbeData(dir, "openrouter", fresh)

	if out[0].ContextWindow != 128000 {
		t.Errorf("ContextWindow from fresh data lost: got %d, want 128000", out[0].ContextWindow)
	}
	if out[0].Capabilities.Tools == nil || !*out[0].Capabilities.Tools {
		t.Errorf("Capabilities from fresh data lost: %+v", out[0].Capabilities)
	}
	if out[0].Probe == nil || !out[0].Probe.Passed {
		t.Errorf("Probe from prior not stamped onto fresh: %+v", out[0].Probe)
	}
}

// helperResetOpenRouterCache resets the OpenRouter cache and restores the URL
// after the test. Call it at the start of each test; it registers cleanup on t.
func helperResetOpenRouterCache(t *testing.T) {
	openRouterModelsCache = nil
	prevURL := openRouterModelsURL
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	t.Cleanup(func() {
		openRouterModelsURL = prevURL
		openRouterModelsCache = nil
	})
}

// TestEnrichFromOpenRouter_FillsPricingGaps verifies that a model with
// Pricing == nil gets pricing filled from a matching OpenRouter entry.
func TestEnrichFromOpenRouter_FillsPricingGaps(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [{
				"id": "deepseek/deepseek-v4-flash",
				"pricing": {
					"prompt": "0.00000009",
					"completion": "0.00000018",
					"input_cache_read": "0.000000018"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	models := []modelcontract.CanonicalModel{
		{ID: "deepseek-v4-flash"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing == nil {
		t.Fatalf("expected pricing to be filled, got nil")
	}
	if out[0].Pricing.Source != "openrouter-cross-ref" {
		t.Errorf("Source = %q, want %q", out[0].Pricing.Source, "openrouter-cross-ref")
	}
	if out[0].Pricing.InputPerMTok != 0.09 {
		t.Errorf("InputPerMTok = %f, want 0.09", out[0].Pricing.InputPerMTok)
	}
	if out[0].Pricing.OutputPerMTok != 0.18 {
		t.Errorf("OutputPerMTok = %f, want 0.18", out[0].Pricing.OutputPerMTok)
	}
	if out[0].Pricing.CachedPerMTok != 0.018 {
		t.Errorf("CachedPerMTok = %f, want 0.018", out[0].Pricing.CachedPerMTok)
	}
	if out[0].Pricing.Currency != "USD" {
		t.Errorf("Currency = %q, want %q", out[0].Pricing.Currency, "USD")
	}
}

// TestEnrichFromOpenRouter_DoesNotOverwriteExisting verifies that models
// that already have Pricing are NOT overwritten.
func TestEnrichFromOpenRouter_DoesNotOverwriteExisting(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [{
				"id": "deepseek/deepseek-v4-flash",
				"pricing": {
					"prompt": "0.00000099",
					"completion": "0.00000099"
				}
			}]
		}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	existing := &modelcontract.Pricing{
		InputPerMTok:  1.0,
		OutputPerMTok: 2.0,
		Currency:      "USD",
		Source:        "native-api",
	}
	models := []modelcontract.CanonicalModel{
		{ID: "deepseek-v4-flash", Pricing: existing},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing != existing {
		t.Errorf("existing pricing was replaced")
	}
	if out[0].Pricing.InputPerMTok != 1.0 {
		t.Errorf("InputPerMTok = %f, want 1.0 (should not be overwritten)", out[0].Pricing.InputPerMTok)
	}
	if out[0].Pricing.Source != "native-api" {
		t.Errorf("Source = %q, want %q", out[0].Pricing.Source, "native-api")
	}
}

// TestEnrichFromOpenRouter_NoMatch verifies that a model with no OpenRouter
// match stays at Pricing == nil.
func TestEnrichFromOpenRouter_NoMatch(t *testing.T) {
	helperResetOpenRouterCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data": [{"id": "other/model", "pricing": {"prompt": "0.01", "completion": "0.02"}}]}`)
	}))
	defer srv.Close()

	openRouterModelsURL = srv.URL

	models := []modelcontract.CanonicalModel{
		{ID: "unknown-model-xyz"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing != nil {
		t.Errorf("expected no pricing (no match), got %+v", out[0].Pricing)
	}
}

// TestEnrichFromOpenRouter_APIUnreachable verifies that when the OpenRouter
// API is unreachable, models are returned unchanged (no panic).
func TestEnrichFromOpenRouter_APIUnreachable(t *testing.T) {
	helperResetOpenRouterCache(t)

	// Point at an address that will refuse connections.
	openRouterModelsURL = "http://127.0.0.1:1"

	models := []modelcontract.CanonicalModel{
		{ID: "some/model"},
	}

	out := enrichFromOpenRouter(context.Background(), models)

	if out[0].Pricing != nil {
		t.Errorf("expected no pricing when API is unreachable, got %+v", out[0].Pricing)
	}
}
