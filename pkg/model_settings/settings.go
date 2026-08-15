package modelsettings

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
)

//go:embed openrouter_model_settings.json
var openRouterModelSettingsJSON []byte

//go:embed creator_recommendations.json
var creatorRecommendationsJSON []byte

type openRouterCatalog struct {
	Models []openRouterModel `json:"models"`
}

type openRouterModel struct {
	ID                  string                 `json:"id"`
	Slug                string                 `json:"slug"`
	SupportedParameters []string               `json:"supported_parameters"`
	DefaultParameters   map[string]interface{} `json:"default_parameters"`
}

type creatorCatalog struct {
	Profiles []creatorProfile `json:"profiles"`
}

type creatorProfile struct {
	ID                    string                 `json:"id"`
	MatchPrefixes         []string               `json:"match_prefixes"`
	MatchExact            []string               `json:"match_exact,omitempty"`
	Parameters            map[string]interface{} `json:"parameters"`
	InstructParameters    map[string]interface{} `json:"instruct_parameters,omitempty"`
	UnsupportedParameters []string               `json:"unsupported_parameters,omitempty"`
	Source                string                 `json:"source"`
	SourceType            string                 `json:"source_type"`
}

// ModelSettings resolves model-specific parameters independent of serving provider.
type ModelSettings struct {
	Known       bool
	Parameters  map[string]interface{}
	Supported   map[string]bool
	Unsupported map[string]bool
	Source      string
	SourceType  string
}

type modelEntry struct {
	Supported map[string]bool
	Defaults  map[string]interface{}
}

var (
	loadOnce      sync.Once
	modelsByKey   map[string]modelEntry
	creatorRules  []creatorProfile
	quantSuffixRe = regexp.MustCompile(`(?i)([_-](q\d[\w.-]*|int\d+|fp\d+|gguf|awq|gptq|exl2[\w.-]*))+$`)
)

func ensureLoaded() {
	loadOnce.Do(loadCatalogs)
}

func loadCatalogs() {
	modelsByKey = make(map[string]modelEntry)

	var openrouter openRouterCatalog
	_ = json.Unmarshal(openRouterModelSettingsJSON, &openrouter)
	for _, m := range openrouter.Models {
		supported := make(map[string]bool, len(m.SupportedParameters))
		for _, p := range m.SupportedParameters {
			supported[strings.ToLower(strings.TrimSpace(p))] = true
		}
		entry := modelEntry{
			Supported: supported,
			Defaults:  m.DefaultParameters,
		}
		modelsByKey[normalizeModelKey(m.ID)] = entry
		modelsByKey[normalizeModelKey(m.Slug)] = entry
	}

	var creators creatorCatalog
	_ = json.Unmarshal(creatorRecommendationsJSON, &creators)
	creatorRules = creators.Profiles
}

// ResolveModelSettings applies precedence:
// model exact rule > model family rule > openrouter fallback defaults.
// It resolves the model's default (thinking) mode parameters.
func ResolveModelSettings(model string) ModelSettings {
	return ResolveModelSettingsForMode(model, false)
}

// ResolveModelSettingsForMode applies precedence and selects the creator
// recommendation for the given generation mode. When instruct is true, a
// profile's instruct_parameters override its parameters (used for models such
// as Qwen3.8 that recommend different sampling for non-thinking mode).
func ResolveModelSettingsForMode(model string, instruct bool) ModelSettings {
	ensureLoaded()
	key := normalizeModelKey(model)
	exact, family := matchCreatorProfile(key)

	entry, ok := modelsByKey[key]
	if !ok {
		// Allow creator-backed profiles to apply even when a model is not in
		// the OpenRouter snapshot (for example custom-provider model IDs).
		profile := exact
		if profile == nil {
			profile = family
		}
		if profile == nil {
			return ModelSettings{Known: false}
		}
		settings := ModelSettings{
			Known:       true,
			Parameters:  map[string]interface{}{},
			Supported:   map[string]bool{},
			Unsupported: map[string]bool{},
		}
		for param := range parametersForMode(profile, instruct) {
			settings.Supported[strings.ToLower(strings.TrimSpace(param))] = true
		}
		mergeCreatorProfileForMode(&settings, profile, instruct)
		return settings
	}

	settings := ModelSettings{
		Known:       true,
		Parameters:  cloneMap(entry.Defaults),
		Supported:   entry.Supported,
		Unsupported: map[string]bool{},
		Source:      "https://openrouter.ai/api/v1/models",
		SourceType:  "third_party",
	}

	if exact != nil {
		mergeCreatorProfileForMode(&settings, exact, instruct)
		return settings
	}
	if family != nil {
		mergeCreatorProfileForMode(&settings, family, instruct)
		return settings
	}

	return settings
}

// parametersForMode returns the parameter set to apply for a profile in the
// given mode: instruct_parameters when set and instruct is true, else
// parameters.
func parametersForMode(profile *creatorProfile, instruct bool) map[string]interface{} {
	if instruct && len(profile.InstructParameters) > 0 {
		return profile.InstructParameters
	}
	return profile.Parameters
}

func mergeCreatorProfileForMode(settings *ModelSettings, profile *creatorProfile, instruct bool) {
	if settings.Parameters == nil {
		settings.Parameters = map[string]interface{}{}
	}
	for k, v := range parametersForMode(profile, instruct) {
		settings.Parameters[strings.ToLower(strings.TrimSpace(k))] = v
	}
	for _, p := range profile.UnsupportedParameters {
		settings.Unsupported[strings.ToLower(strings.TrimSpace(p))] = true
	}
	settings.Source = profile.Source
	settings.SourceType = profile.SourceType
}

func matchCreatorProfile(modelKey string) (exact *creatorProfile, family *creatorProfile) {
	for i := range creatorRules {
		rule := &creatorRules[i]
		for _, exactKey := range rule.MatchExact {
			if normalizeModelKey(exactKey) == modelKey {
				exact = rule
				return exact, family
			}
		}
	}
	for i := range creatorRules {
		rule := &creatorRules[i]
		for _, prefix := range rule.MatchPrefixes {
			if strings.HasPrefix(modelKey, strings.ToLower(strings.TrimSpace(prefix))) {
				family = rule
				return exact, family
			}
		}
	}
	return exact, family
}

func normalizeModelKey(model string) string {
	v := strings.ToLower(strings.TrimSpace(model))
	if slash := strings.Index(v, "/"); slash >= 0 {
		v = v[slash+1:]
	}
	if colon := strings.Index(v, ":"); colon >= 0 {
		v = v[:colon]
	}
	v = quantSuffixRe.ReplaceAllString(v, "")
	return v
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[strings.ToLower(strings.TrimSpace(k))] = v
	}
	return out
}
