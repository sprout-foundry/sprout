package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	pricingTable     PricingTable
	pricingTablePath = filepath.Join(".ledit", "model_pricing.json")
	pricingInitDone  = false
)

// init attempts to load pricing table once at package load
func init() {
	_ = InitPricingTable()
}

// InitPricingTable loads pricing from disk if available; creates an empty table if missing
func InitPricingTable() error {
	if pricingInitDone {
		return nil
	}
	pricingInitDone = true

	// Defaults
	pricingTable = PricingTable{Models: map[string]ModelPricing{}}

	// Ensure directory exists
	_ = os.MkdirAll(filepath.Dir(pricingTablePath), 0o755)

	data, err := os.ReadFile(pricingTablePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Write initial empty file
			return SavePricingTable()
		}
		return err
	}
	if len(data) == 0 {
		return SavePricingTable()
	}
	if err := json.Unmarshal(data, &pricingTable); err != nil {
		return fmt.Errorf("failed to parse pricing table %s: %w", pricingTablePath, err)
	}
	normalizePricingKeys()
	return nil
}

func normalizePricingKeys() {
	normalized := make(map[string]ModelPricing, len(pricingTable.Models))
	for k, v := range pricingTable.Models {
		normalized[strings.ToLower(strings.TrimSpace(k))] = v
	}
	pricingTable.Models = normalized
}

// SavePricingTable writes the current pricing table to disk
func SavePricingTable() error {
	if pricingTable.Models == nil {
		pricingTable.Models = map[string]ModelPricing{}
	}
	data, err := json.MarshalIndent(pricingTable, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pricingTablePath, data, 0o644)
}

// UpdatePricing sets or overrides pricing for a model and persists it
func UpdatePricing(model string, pricing ModelPricing) error {
	if pricingTable.Models == nil {
		pricingTable.Models = map[string]ModelPricing{}
	}
	pricingTable.Models[strings.ToLower(strings.TrimSpace(model))] = pricing
	return SavePricingTable()
}

// GetModelPricing consults the persisted pricing table first; falls back to heuristics
func GetModelPricing(model string) ModelPricing {
	if pricingTable.Models != nil {
		if p, ok := pricingTable.Models[strings.ToLower(strings.TrimSpace(model))]; ok {
			return p
		}
	}
	// Fallback heuristics for common families
	modelLower := strings.ToLower(model)
	switch {
	case strings.Contains(modelLower, "deepseek"):
		return ModelPricing{InputCostPer1K: 0.27 / 1000, OutputCostPer1K: 1.1 / 1000} // $0.27/$1.10 per 1M → per 1K
	case strings.Contains(modelLower, "llama"):
		return ModelPricing{InputCostPer1K: 0.30 / 1000, OutputCostPer1K: 0.60 / 1000}
	case strings.Contains(modelLower, "mixtral"):
		return ModelPricing{InputCostPer1K: 0.24 / 1000, OutputCostPer1K: 0.24 / 1000}
	case strings.Contains(modelLower, "qwen"):
		return ModelPricing{InputCostPer1K: 0.40 / 1000, OutputCostPer1K: 0.80 / 1000}
	case strings.Contains(modelLower, "gpt-4o"):
		return ModelPricing{InputCostPer1K: 0.005, OutputCostPer1K: 0.015}
	case strings.Contains(modelLower, "gpt-4-turbo"):
		return ModelPricing{InputCostPer1K: 0.01, OutputCostPer1K: 0.03}
	case strings.Contains(modelLower, "gpt-4"):
		return ModelPricing{InputCostPer1K: 0.03, OutputCostPer1K: 0.06}
	case strings.Contains(modelLower, "gpt-3.5-turbo"):
		return ModelPricing{InputCostPer1K: 0.0005, OutputCostPer1K: 0.0015}
	case strings.Contains(modelLower, "gemini"):
		return ModelPricing{InputCostPer1K: 0.00025, OutputCostPer1K: 0.0005}
	case strings.Contains(modelLower, "ollama"):
		return ModelPricing{InputCostPer1K: 0.0, OutputCostPer1K: 0.0}
	default:
		return ModelPricing{InputCostPer1K: 0.002, OutputCostPer1K: 0.002}
	}
}

// SyncDeepInfraPricing attempts to fetch a JSON mapping from a DeepInfra-provided URL.
// If no URL is provided, this is a no-op. This supports user-specified endpoints since
// DeepInfra does not publicly document a JSON pricing API.
func SyncDeepInfraPricing(url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch pricing from %s: status %d", url, resp.StatusCode)
	}
	var incoming PricingTable
	if err := json.NewDecoder(resp.Body).Decode(&incoming); err != nil {
		return fmt.Errorf("failed to decode pricing JSON: %w", err)
	}
	if pricingTable.Models == nil {
		pricingTable.Models = map[string]ModelPricing{}
	}
	for k, v := range incoming.Models {
		pricingTable.Models[strings.ToLower(strings.TrimSpace(k))] = v
	}
	return SavePricingTable()
}
