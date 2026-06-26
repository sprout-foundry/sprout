// Package modelcontract defines the canonical, provider-agnostic model schema
// and the adapter interface that normalizes each provider's native model API
// into it.
//
// The goal is a single contract the rest of the application consumes with
// confidence: every model has the same shape with normalized capabilities,
// pricing, context window, and lifecycle, regardless of how the originating
// provider happens to report them. Provider quirks live ONLY inside adapters.
// Values that genuinely can't be determined are left explicitly unknown
// (nil pointers / empty slices), never silently approximated.
package modelcontract

import "context"

// SchemaVersion is the version of the canonical published file format.
// Bumped only for breaking changes; consumers should accept known versions.
const SchemaVersion = 2

// Role names a model can be eligible/recommended for.
const (
	RolePrimary  = "primary"
	RoleSubagent = "subagent"
)

// CanonicalModel is the normalized, provider-agnostic representation of a model.
type CanonicalModel struct {
	// Identity
	ID          string `json:"id"`       // inference ID (provider-native)
	Provider    string `json:"provider"` // "deepinfra", "openrouter", …
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`

	// Limits — 0 means unknown (documented convention).
	ContextWindow   int `json:"context_window,omitempty"`
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`

	// Pricing — nil means unknown (NOT free).
	Pricing *Pricing `json:"pricing,omitempty"`

	// Capabilities — each is tri-state (see Capabilities).
	Capabilities Capabilities `json:"capabilities"`

	// Modality, e.g. ["text","image"].
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`

	// Lifecycle
	Status     string `json:"status,omitempty"` // StatusActive / StatusDeprecated / StatusPreview
	ReplacedBy string `json:"replaced_by,omitempty"`

	// Derived (not reported by the provider) — populated by sprout, not adapters.
	EligibleRoles    []string     `json:"eligible_roles,omitempty"`    // deterministic pre-filter
	Probe            *ProbeResult `json:"probe,omitempty"`             // capability probe (later phase)
	RecommendedRoles []string     `json:"recommended_roles,omitempty"` // post-probe (later phase)
	Warnings         []string     `json:"warnings,omitempty"`          // non-blocking caveats to surface (e.g. small context)

	// Provenance
	Source    string `json:"source,omitempty"`     // e.g. "deepinfra:/models/list"
	UpdatedAt string `json:"updated_at,omitempty"` // RFC3339
}

// Lifecycle status values.
const (
	StatusActive     = "active"
	StatusDeprecated = "deprecated"
	StatusPreview    = "preview"
)

// Pricing is USD per million tokens. Estimated marks values borrowed from a
// reference source (e.g. OpenRouter) rather than confirmed for the caller's
// account — relevant where provider pricing is account-specific.
type Pricing struct {
	InputPerMTok  float64 `json:"input_per_mtok"`
	OutputPerMTok float64 `json:"output_per_mtok"`
	CachedPerMTok float64 `json:"cached_input_per_mtok,omitempty"`
	Currency      string  `json:"currency,omitempty"` // "USD"
	Estimated     bool    `json:"estimated,omitempty"`
	Source        string  `json:"source,omitempty"` // e.g. "openrouter-reference"
}

// Capabilities is a tri-state capability set: a non-nil *bool is a known
// true/false; nil means the provider's metadata didn't let us determine it.
// Consumers must treat nil as "unknown", never as false.
type Capabilities struct {
	Tools            *bool `json:"tools,omitempty"`
	Vision           *bool `json:"vision,omitempty"`
	Reasoning        *bool `json:"reasoning,omitempty"`
	StructuredOutput *bool `json:"structured_output,omitempty"`
	Streaming        *bool `json:"streaming,omitempty"`
}

// ProbeResult records the outcome of a capability probe. Passed is the minimum
// gate (model is usable for agentic edits at all); Complex additionally reports
// that the model cleared the discovery+scoping tier, the signal for driving
// primary-grade complex flows.
type ProbeResult struct {
	Passed       bool    `json:"passed"`
	Complex      bool    `json:"complex,omitempty"`
	Score        float64 `json:"score,omitempty"`
	LastProbedAt string  `json:"last_probed_at,omitempty"`
	ProbeVersion string  `json:"probe_version,omitempty"`
}

// ProviderFile is the published per-provider canonical model file.
type ProviderFile struct {
	SchemaVersion int              `json:"schema_version"`
	Provider      string           `json:"provider"`
	GeneratedAt   string           `json:"generated_at"`
	Models        []CanonicalModel `json:"models"`
}

// ModelAdapter owns ALL provider-specific parsing and emits canonical models.
// It is the only place a provider's quirks are allowed to exist. Listing must
// not require an API key where the provider's model endpoint is public.
type ModelAdapter interface {
	Provider() string
	ListModels(ctx context.Context) ([]CanonicalModel, error)
}

// CapabilityTags renders known-true capabilities as legacy tag strings, for
// consumers/wire formats that carry capabilities as a flat tag list.
func CapabilityTags(c Capabilities) []string {
	var tags []string
	if IsTrue(c.Tools) {
		tags = append(tags, "tools")
	}
	if IsTrue(c.Vision) {
		tags = append(tags, "vision")
	}
	if IsTrue(c.Reasoning) {
		tags = append(tags, "reasoning")
	}
	if IsTrue(c.StructuredOutput) {
		tags = append(tags, "structured_output")
	}
	return tags
}

// CapabilitiesFromTags reconstructs capabilities from a flat tag list (the
// inverse of CapabilityTags, used when projecting a legacy flat record up to
// the canonical shape). A tag's presence is known-true; absence stays unknown
// (nil) since a flat list can't distinguish known-false from unknown.
func CapabilitiesFromTags(tags []string) Capabilities {
	has := func(t string) *bool {
		for _, x := range tags {
			if x == t {
				return Bool(true)
			}
		}
		return nil
	}
	return Capabilities{
		Tools:            has("tools"),
		Vision:           has("vision"),
		Reasoning:        has("reasoning"),
		StructuredOutput: has("structured_output"),
	}
}

// RoleHas reports whether roles contains role (exact match). It treats the
// modelcontract role names as opaque strings, so adding a new role never
// requires touching this helper — only the call sites that interpret the
// string need to know what to do with it.
func RoleHas(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// Bool returns a pointer to b, for setting tri-state capabilities.
func Bool(b bool) *bool { return &b }

// IsTrue reports whether a tri-state capability is known-true.
func IsTrue(b *bool) bool { return b != nil && *b }

// IsKnownFalse reports whether a tri-state capability is known-false
// (as opposed to unknown).
func IsKnownFalse(b *bool) bool { return b != nil && !*b }
