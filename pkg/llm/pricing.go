package llm

import (
	"encoding/json"
	"fmt"
	"io"
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

// Minimal structs to parse DeepInfra's Next.js pricing JSON
// Prefer pageProps.pricing.sections, but also attempt i18n store fallback
type deepInfraEntry struct {
	ModelName string `json:"model_name"`
	Pricing   struct {
		Type                string  `json:"type"`
		CentsPerInputToken  float64 `json:"cents_per_input_token"`
		CentsPerOutputToken float64 `json:"cents_per_output_token"`
	} `json:"pricing"`
}

type deepInfraSection struct {
	PType   string           `json:"ptype"`
	Entries []deepInfraEntry `json:"entries"`
}

type deepInfraPricingRoot struct {
	PageProps struct {
		Pricing struct {
			Sections []deepInfraSection `json:"sections"`
		} `json:"pricing"`
		NextI18Next struct {
			InitialI18nStore struct {
				En struct {
					Pages struct {
						Pricing struct {
							Sections []deepInfraSection `json:"sections"`
						} `json:"pricing"`
					} `json:"pages"`
				} `json:"en"`
			} `json:"initialI18nStore"`
		} `json:"_nextI18Next"`
		PricingPageData []struct {
			SectionID string           `json:"section_id"`
			PType     string           `json:"ptype"`
			Entries   []deepInfraEntry `json:"entries"`
		} `json:"_pricingPageData"`
	} `json:"pageProps"`
}

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
	// Normalize model key: strip provider prefixes and lowercase
	key := strings.ToLower(strings.TrimSpace(model))
	// Common provider prefixes to strip
	prefixes := []string{"deepinfra:", "openai:", "groq:", "gemini:", "cerebras:", "lambda-ai:", "ollama:"}
	for _, p := range prefixes {
		if strings.HasPrefix(key, p) {
			key = strings.TrimPrefix(key, p)
			break
		}
	}
	// Family normalization (rough mapping)
	if strings.Contains(key, "gpt-4o") || strings.Contains(key, "gpt-4-turbo") || strings.Contains(key, "gpt-4") {
		key = "gpt-4"
	} else if strings.Contains(key, "gpt-3.5") {
		key = "gpt-3.5-turbo"
	} else if strings.Contains(key, "gemini") {
		key = "gemini"
	} else if strings.Contains(key, "llama") {
		key = "llama"
	} else if strings.Contains(key, "deepseek") {
		key = "deepseek"
	} else if strings.Contains(key, "qwen") {
		key = "qwen"
	}

	// If present in table, return
	if pricingTable.Models != nil {
		if p, ok := pricingTable.Models[key]; ok {
			return p
		}
	}
	// Attempt a one-time DeepInfra sync if empty
	_ = InitPricingTable()
	if pricingTable.Models != nil {
		if _, ok := pricingTable.Models[key]; !ok {
			// Best-effort sync; ignore error and fall back to heuristics
			_ = SyncDeepInfraPricing("")
			// Normalize keys after sync and persist
			normalizePricingKeys()
			_ = SavePricingTable()
			if p, ok := pricingTable.Models[key]; ok {
				return p
			}
		}
	}

	// Fallback heuristics for common families
	modelLower := key
	switch {
	case strings.Contains(modelLower, "deepseek"):
		return ModelPricing{InputCostPer1K: 0.27 / 1000, OutputCostPer1K: 1.1 / 1000} // $0.27/$1.10 per 1M → per 1K
	case strings.Contains(modelLower, "llama"):
		return ModelPricing{InputCostPer1K: 0.30 / 1000, OutputCostPer1K: 0.60 / 1000}
	case strings.Contains(modelLower, "mixtral"):
		return ModelPricing{InputCostPer1K: 0.24 / 1000, OutputCostPer1K: 0.24 / 1000}
	case strings.Contains(modelLower, "qwen"):
		return ModelPricing{InputCostPer1K: 0.40 / 1000, OutputCostPer1K: 0.80 / 1000}
	case strings.Contains(modelLower, "gpt-4o") || strings.Contains(modelLower, "gpt-4-turbo") || strings.Contains(modelLower, "gpt-4"):
		return ModelPricing{InputCostPer1K: 0.03, OutputCostPer1K: 0.06}
	case strings.Contains(modelLower, "gpt-3.5"):
		return ModelPricing{InputCostPer1K: 0.002, OutputCostPer1K: 0.002}
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
	// If URL omitted, auto-discover current pricing JSON by scraping the pricing page
	if url == "" {
		return syncDeepInfraPricingAuto()
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		// Fallback to auto if direct fetch fails
		if resp != nil {
			resp.Body.Close()
		}
		return syncDeepInfraPricingAuto()
	}
	defer resp.Body.Close()

	// First, try to decode as our native PricingTable in case the URL already matches
	var incoming PricingTable
	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &incoming); err == nil && len(incoming.Models) > 0 {
		if pricingTable.Models == nil {
			pricingTable.Models = map[string]ModelPricing{}
		}
		for k, v := range incoming.Models {
			pricingTable.Models[strings.ToLower(strings.TrimSpace(k))] = v
		}
		return SavePricingTable()
	}

	// Try DeepInfra Next.js structure
	var di deepInfraPricingRoot
	if err := json.Unmarshal(bodyBytes, &di); err == nil {
		if err := applyDeepInfraPricing(di); err == nil {
			return nil
		}
	}

	// Try generic recursive extraction of sections
	if err := applyDeepInfraPricingGeneric(bodyBytes); err == nil {
		return nil
	}

	// If none worked, fallback to auto
	return syncDeepInfraPricingAuto()
}

// syncDeepInfraPricingAuto fetches the DeepInfra pricing page, extracts buildId from __NEXT_DATA__,
// fetches the corresponding pricing JSON, and applies it.
func syncDeepInfraPricingAuto() error {
	client := &http.Client{Timeout: 15 * time.Second}
	pricingPage := "https://deepinfra.com/pricing"
	resp, err := client.Get(pricingPage)
	if err != nil {
		return fmt.Errorf("failed to fetch pricing page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch pricing page: status %d", resp.StatusCode)
	}
	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read pricing page: %w", err)
	}
	html := string(htmlBytes)
	// Extract __NEXT_DATA__ JSON
	const marker = "id=\"__NEXT_DATA__\""
	idx := strings.Index(html, marker)
	if idx == -1 {
		return fmt.Errorf("__NEXT_DATA__ not found in pricing page")
	}
	// Find start of JSON
	start := strings.Index(html[idx:], ">")
	if start == -1 {
		return fmt.Errorf("failed to locate __NEXT_DATA__ start")
	}
	start = idx + start + 1
	end := strings.Index(html[start:], "</script>")
	if end == -1 {
		return fmt.Errorf("failed to locate __NEXT_DATA__ end")
	}
	jsonStr := html[start : start+end]
	var nextData struct {
		BuildID string `json:"buildId"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &nextData); err != nil {
		return fmt.Errorf("failed to parse __NEXT_DATA__: %w", err)
	}
	if strings.TrimSpace(nextData.BuildID) == "" {
		return fmt.Errorf("buildId not found in __NEXT_DATA__")
	}
	// Fetch pricing JSON using buildId
	dataURL := fmt.Sprintf("https://deepinfra.com/_next/data/%s/pricing.json", nextData.BuildID)
	resp2, err := client.Get(dataURL)
	if err != nil {
		return fmt.Errorf("failed to fetch pricing JSON: %w", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch pricing from %s: status %d", dataURL, resp2.StatusCode)
	}
	var di deepInfraPricingRoot
	if err := json.NewDecoder(resp2.Body).Decode(&di); err != nil {
		return fmt.Errorf("failed to decode DeepInfra pricing JSON: %w", err)
	}
	return applyDeepInfraPricing(di)
}

func applyDeepInfraPricing(di deepInfraPricingRoot) error {
	sections := di.PageProps.Pricing.Sections
	if len(sections) == 0 {
		sections = di.PageProps.NextI18Next.InitialI18nStore.En.Pages.Pricing.Sections
	}
	// Fallback: build sections from _pricingPageData
	if len(sections) == 0 {
		if ppd := di.PageProps.PricingPageData; len(ppd) > 0 {
			for _, s := range ppd {
				sections = append(sections, deepInfraSection{PType: s.PType, Entries: s.Entries})
			}
		}
	}
	if len(sections) == 0 {
		return fmt.Errorf("no pricing sections found in DeepInfra JSON")
	}
	if pricingTable.Models == nil {
		pricingTable.Models = map[string]ModelPricing{}
	}
	for _, section := range sections {
		for _, entry := range section.Entries {
			modelName := strings.TrimSpace(entry.ModelName)
			if modelName == "" {
				continue
			}
			key := "deepinfra:" + strings.ToLower(modelName)
			inPer1K := entry.Pricing.CentsPerInputToken / 100.0 * 1000.0
			outPer1K := entry.Pricing.CentsPerOutputToken / 100.0 * 1000.0
			if strings.EqualFold(entry.Pricing.Type, "input_tokens") {
				outPer1K = 0
			}
			pricingTable.Models[key] = ModelPricing{InputCostPer1K: inPer1K, OutputCostPer1K: outPer1K}
		}
	}
	return SavePricingTable()
}

// applyDeepInfraPricingGeneric attempts to find any nested "sections" arrays with entries
// that contain model_name/pricing fields, and applies them.
func applyDeepInfraPricingGeneric(body []byte) error {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return err
	}
	sections := collectSections(root)
	if len(sections) == 0 {
		return fmt.Errorf("no pricing sections found generically")
	}
	if pricingTable.Models == nil {
		pricingTable.Models = map[string]ModelPricing{}
	}
	for _, section := range sections {
		for _, entry := range section.Entries {
			modelName := strings.TrimSpace(entry.ModelName)
			if modelName == "" {
				continue
			}
			key := "deepinfra:" + strings.ToLower(modelName)
			inPer1K := entry.Pricing.CentsPerInputToken / 100.0 * 1000.0
			outPer1K := entry.Pricing.CentsPerOutputToken / 100.0 * 1000.0
			if strings.EqualFold(entry.Pricing.Type, "input_tokens") {
				outPer1K = 0
			}
			pricingTable.Models[key] = ModelPricing{InputCostPer1K: inPer1K, OutputCostPer1K: outPer1K}
		}
	}
	return SavePricingTable()
}

func collectSections(node any) []deepInfraSection {
	var results []deepInfraSection
	switch v := node.(type) {
	case map[string]any:
		if sec, ok := v["sections"]; ok {
			if arr, ok := sec.([]any); ok && len(arr) > 0 {
				// Try to coerce to []deepInfraSection
				if b, err := json.Marshal(arr); err == nil {
					var parsed []deepInfraSection
					if err := json.Unmarshal(b, &parsed); err == nil && len(parsed) > 0 {
						results = append(results, parsed...)
					}
				}
			}
		}
		for _, child := range v {
			results = append(results, collectSections(child)...)
		}
	case []any:
		for _, child := range v {
			results = append(results, collectSections(child)...)
		}
	}
	return results
}
