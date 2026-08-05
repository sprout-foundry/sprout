package configuration

import (
	"encoding/json"
	"strings"
)

// Layer merging needs to distinguish "this layer set the field to false" from
// "this layer didn't mention the field". Go's zero value collapses both to
// false, so the merge historically used truthiness as a proxy for
// presence — `if override.EmbeddingIndex.Enabled { ... }`. That makes every
// boolean one-way: a narrower layer can turn a flag ON but never OFF.
//
// The presence information exists in the raw JSON of each layer, so it's
// captured at decode time and carried on the Config as layer provenance. This
// keeps the fields plain `bool` for the ~135 places that read them, instead of
// forcing a `*bool` migration through all of them.

// maxExplicitKeyDepth bounds the recorded paths to "section.field". Nothing in
// the merge reasons about deeper paths, and unbounded recursion would enumerate
// every entry of the open-ended maps (mcp.servers, custom_providers,
// provider_models) for no gain.
const maxExplicitKeyDepth = 2

// unmarshalLayer decodes one config layer, recording which keys the layer
// actually contained. Use this instead of json.Unmarshal for anything that
// will be passed to MergeConfig as an override.
func unmarshalLayer(data []byte, cfg *Config) error {
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		// The struct decode already succeeded, so the layer is usable; it
		// just carries no presence information and falls back to truthiness.
		return nil
	}
	cfg.recordExplicitKeys(raw)
	return nil
}

// recordExplicitKeys captures the dotted paths present in a layer's raw JSON.
func (c *Config) recordExplicitKeys(raw map[string]interface{}) {
	if c.explicitKeys == nil {
		c.explicitKeys = make(map[string]bool)
	}
	flattenJSONKeys(raw, "", 1, c.explicitKeys)
}

func flattenJSONKeys(raw map[string]interface{}, prefix string, depth int, out map[string]bool) {
	for key, value := range raw {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		out[path] = true
		if depth >= maxExplicitKeyDepth {
			continue
		}
		if child, ok := value.(map[string]interface{}); ok {
			flattenJSONKeys(child, path, depth+1, out)
		}
	}
}

// IsExplicitlySet reports whether a dotted JSON path was present in the layer
// this config was decoded from. Configs built in memory (tests, defaults) carry
// no provenance and always report false.
func (c *Config) IsExplicitlySet(path string) bool {
	if c == nil || c.explicitKeys == nil {
		return false
	}
	return c.explicitKeys[path]
}

// overrides reports whether an override layer should win for a boolean field.
// A layer that named the path wins with whatever value it gave — including
// false. Otherwise it wins only when setting the flag to true, which is the
// legacy behavior and the only thing an in-memory Config (no provenance) can do.
func (c *Config) overrides(path string, value bool) bool {
	return value || c.IsExplicitlySet(path)
}

// mergeExplicitKeys unions the provenance of two layers into the merged result,
// so a three-layer merge (global → workspace → session) keeps the presence
// information from every layer it folded in.
func (c *Config) mergeExplicitKeys(from *Config) {
	if from == nil || len(from.explicitKeys) == 0 {
		return
	}
	if c.explicitKeys == nil {
		c.explicitKeys = make(map[string]bool, len(from.explicitKeys))
	}
	for path := range from.explicitKeys {
		c.explicitKeys[path] = true
	}
}

// copyExplicitKeys returns an independent copy, for cloneConfig — the JSON
// roundtrip there drops unexported fields.
func (c *Config) copyExplicitKeys() map[string]bool {
	if len(c.explicitKeys) == 0 {
		return nil
	}
	out := make(map[string]bool, len(c.explicitKeys))
	for path := range c.explicitKeys {
		out[path] = true
	}
	return out
}

// ExplicitKeyPaths lists the recorded paths, sorted-insensitive. Exposed for
// tests and provenance reporting.
func (c *Config) ExplicitKeyPaths() []string {
	if c == nil {
		return nil
	}
	paths := make([]string, 0, len(c.explicitKeys))
	for path := range c.explicitKeys {
		paths = append(paths, path)
	}
	return paths
}

// SectionExplicitlySet reports whether any field under a section was named by
// the layer, e.g. SectionExplicitlySet("embedding_index").
func (c *Config) SectionExplicitlySet(section string) bool {
	if c == nil {
		return false
	}
	prefix := section + "."
	for path := range c.explicitKeys {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// boolPtr returns a pointer to v, for the tri-state config flags.
func boolPtr(v bool) *bool { return &v }
