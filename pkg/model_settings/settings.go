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
func ResolveModelSettings(model string) ModelSettings {
	ensureLoaded()
	key := normalizeModelKey(model)

	entry, ok := modelsByKey[key]
	if !ok {
		return ModelSettings{Known: false}
	}

	settings := ModelSettings{
		Known:       true,
		Parameters:  cloneMap(entry.Defaults),
		Supported:   entry.Supported,
		Unsupported: map[string]bool{},
		Source:      "https://openrouter.ai/api/v1/models",
		SourceType:  "third_party",
	}

	exact, family := matchCreatorProfile(key)
	if exact != nil {
		mergeCreatorProfile(&settings, exact)
		return settings
	}
	if family != nil {
		mergeCreatorProfile(&settings, family)
		return settings
	}

	return settings
}

func mergeCreatorProfile(settings *ModelSettings, profile *creatorProfile) {
	if settings.Parameters == nil {
		settings.Parameters = map[string]interface{}{}
	}
	for k, v := range profile.Parameters {
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
