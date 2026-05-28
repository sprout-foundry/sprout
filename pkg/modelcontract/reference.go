package modelcontract

import "strings"

// ReferenceCatalog indexes canonical models by a normalized `org/model` key so
// adapters whose native API exposes little metadata (e.g. OpenAI) can borrow
// capabilities/context/modality from a richer source that lists the same model
// (e.g. OpenRouter, whose IDs are already `org/model`).
type ReferenceCatalog struct {
	byKey map[string]CanonicalModel
}

// NewReferenceCatalog builds a catalog from already-canonical models (typically
// the OpenRouter adapter's output).
func NewReferenceCatalog(models []CanonicalModel) *ReferenceCatalog {
	c := &ReferenceCatalog{byKey: make(map[string]CanonicalModel, len(models))}
	for _, m := range models {
		c.byKey[normalizeRefKey(m.ID)] = m
	}
	return c
}

// Lookup finds a reference model for a provider org + model ID — e.g.
// org="openai", id="gpt-4o" resolves "openai/gpt-4o". Returns false if absent.
func (c *ReferenceCatalog) Lookup(org, id string) (CanonicalModel, bool) {
	if c == nil {
		return CanonicalModel{}, false
	}
	m, ok := c.byKey[normalizeRefKey(org+"/"+id)]
	return m, ok
}

// normalizeRefKey lowercases and strips any OpenRouter-style variant suffix
// (":free", ":extended", …) so "OpenAI/GPT-4o:extended" and "openai/gpt-4o"
// collide intentionally.
func normalizeRefKey(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return s
}

// EnrichFromReference fills unknown fields on dst from a reference model that
// represents the same underlying model on another provider. Capabilities,
// context, and modality are copied with confidence (same model). Pricing is
// copied only when dst has none, and is marked Estimated because the
// originating provider's price may be account-specific.
func EnrichFromReference(dst, ref CanonicalModel) CanonicalModel {
	if dst.ContextWindow == 0 {
		dst.ContextWindow = ref.ContextWindow
	}
	if dst.MaxOutputTokens == 0 {
		dst.MaxOutputTokens = ref.MaxOutputTokens
	}
	if len(dst.InputModalities) == 0 {
		dst.InputModalities = ref.InputModalities
	}
	if len(dst.OutputModalities) == 0 {
		dst.OutputModalities = ref.OutputModalities
	}
	dst.Capabilities = mergeCapabilities(dst.Capabilities, ref.Capabilities)
	if dst.Pricing == nil && ref.Pricing != nil {
		p := *ref.Pricing
		p.Estimated = true
		p.Source = ref.Provider + "-reference"
		dst.Pricing = &p
	}
	return dst
}

// mergeCapabilities fills nil (unknown) capabilities on dst from ref, leaving
// any value dst already knows untouched.
func mergeCapabilities(dst, ref Capabilities) Capabilities {
	if dst.Tools == nil {
		dst.Tools = ref.Tools
	}
	if dst.Vision == nil {
		dst.Vision = ref.Vision
	}
	if dst.Reasoning == nil {
		dst.Reasoning = ref.Reasoning
	}
	if dst.StructuredOutput == nil {
		dst.StructuredOutput = ref.StructuredOutput
	}
	if dst.Streaming == nil {
		dst.Streaming = ref.Streaming
	}
	return dst
}
