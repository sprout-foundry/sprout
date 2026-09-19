package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validTokensDocument is a complete, well-formed DTCG document exercising
// all nine accepted $type values, nested groups, string and non-string
// $value scalars, $description metadata, and a resolving alias.
const validTokensDocument = `{
  "color": {
    "$description": "all colors for this tier",
    "brand": {
      "primary":   { "$type": "color", "$value": "#0055ff" },
      "secondary": { "$type": "color", "$value": "#00ff88" },
      "text":      { "$type": "color", "$value": "{color.neutral.900}" }
    },
    "neutral": {
      "50":  { "$type": "color", "$value": "#fafafa" },
      "900": { "$type": "color", "$value": "#0a0a0a" }
    },
    "swatch": { "$type": "color", "$value": { "r": 0, "g": 0.2, "b": 1 } },
    "links":  { "$type": "color", "$value": ["#0055ff", "#00ff88"] }
  },
  "dimension": {
    "space": {
      "small":  { "$type": "dimension", "$value": "4px" },
      "medium": { "$type": "dimension", "$value": "8px" }
    },
    "wide": { "$type": "dimension", "$value": 160 }
  },
  "fontFamily": { "body": { "$type": "fontFamily", "$value": "Inter, system-ui" } },
  "fontWeight": { "bold": { "$type": "fontWeight", "$value": 700 } },
  "number":     { "scale": { "$type": "number", "$value": 1.25 } },
  "duration":   { "slow":  { "$type": "duration", "$value": "300ms" } },
  "cubicBezier": { "ease": { "$type": "cubicBezier", "$value": [0.3, 0, 0, 1] } },
  "strokeStyle": { "hairline": { "$type": "strokeStyle", "$value": "solid" } },
  "border": {
    "card": {
      "$type": "border",
      "$value": { "color": "{color.brand.primary}", "width": "1px", "style": "solid" }
    }
  }
}`

// requireNoFindings asserts a clean validation run: no findings, but a
// non-nil empty slice (never nil), with every finding — were one to
// exist — stamped as a hard violation on a real file.
func requireNoFindings(t *testing.T, findings []Finding) {
	t.Helper()
	require.NotNil(t, findings, "a completed validation run must return a non-nil slice")
	assert.Empty(t, findings)
	for _, f := range findings {
		assert.Equal(t, SeverityError, f.Severity, "every token finding is a hard violation")
		assert.NotEmpty(t, f.File)
	}
}

// requireSingleFinding asserts exactly one finding with the given rule,
// severity, and 1-based line, and returns it for message checks.
func requireSingleFinding(t *testing.T, findings []Finding, rule string, severity Severity, line int) Finding {
	t.Helper()
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, rule, f.Rule)
	assert.Equal(t, severity, f.Severity)
	assert.Equal(t, line, f.Line, "finding must anchor at 1-based source line %d", line)
	assert.NotEmpty(t, f.Message)
	return f
}

// validFileBody wraps name around a minimal self-contained clean token.
// It deliberately avoids validTokensDocument: that document's alias is
// root-relative, and per-file resolution would leave it dangling under
// a wrapper group.
func validFileBody(name string) string {
	return `{"` + name + `": {"primary": {"$type": "color", "$value": "#0055ff"}}}`
}

// -----------------------------------------------------------------------------
// Item 1 — valid DTCG trees produce zero findings
// -----------------------------------------------------------------------------

func TestValidateTokensValid(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "all nine $type values, nested groups, $description, resolving alias",
			content: validTokensDocument,
		},
		{
			name: "alias-shaped strings inside non-alias values are inert",
			content: `{
  "color": {
    "primary": { "$type": "color", "$value": "#0055ff" }
  },
  "border": {
    "card": {
      "$type": "border",
      "$value": { "color": "{color.primary}", "label": "set {missing} anywhere" },
      "$description": "prose mentioning {dangling.like} references"
    }
  },
  "multiPart": { "$type": "color", "$value": "{color.primary} / {color.missing}" }
}`,
		},
		{
			name: "$extensions subtree never validates and never resolves aliases",
			content: `{
  "color": {
    "primary": { "$type": "color", "$value": "#0055ff" },
    "$extensions": {
      "org.tool": {
        "$value": "{does.not.exist}",
        "deep": { "nest": { "$value": "{still.missing}" } },
        "$type": 42
      }
    }
  },
  "extensionRef": {
    "$type": "color",
    "$extensions": { "org.tool": { "fallback": "{color.primary}" } },
    "$value": "#0055ff"
  }
}`,
		},
		{
			name:    "minimal single token",
			content: `{"color": {"primary": {"$type": "color", "$value": "#0055ff"}}}`,
		},
		{
			name:    "empty root group",
			content: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireNoFindings(t, ValidateTokens("design/tokens/color.tokens.json", []byte(tt.content)))
		})
	}
}

// -----------------------------------------------------------------------------
// Item 2 — JSON parse failures
// -----------------------------------------------------------------------------

func TestValidateTokensJSONParseErrors(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantLine        int
		wantMsgFragment string
	}{
		{
			name:            "truncated JSON",
			content:         "{\n  \"color\": {\n    \"primary\": {\n      \"$type\": \"color\"",
			wantLine:        4,
			wantMsgFragment: "invalid JSON",
		},
		{
			name:            "truncated mid-value",
			content:         "{\"color\": {\"primary\": {\"$type\": \"col",
			wantLine:        1,
			wantMsgFragment: "invalid JSON",
		},
		{
			// The trailing-data finding anchors at the decoder offset
			// captured after the root object — i.e. the boundary where
			// the extra value begins — which maps to the root object's
			// own line when a newline separates the two.
			name:            "well-formed second value after the root object",
			content:         "{}\n42",
			wantLine:        1,
			wantMsgFragment: "trailing data",
		},
		{
			name:            "adjacent second object after the root object",
			content:         "{} {}",
			wantLine:        1,
			wantMsgFragment: "trailing data",
		},
		{
			name:            "trailing garbage after a valid tree",
			content:         "{\"color\": {\"primary\": {\"$type\": \"color\", \"$value\": \"#fff\"}}}\nnot json at all",
			wantLine:        2,
			wantMsgFragment: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := requireSingleFinding(t, ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content)),
				ruleTokenJSONParse, SeverityError, tt.wantLine)
			assert.Contains(t, f.Message, tt.wantMsgFragment)
		})
	}
}

func TestValidateTokensNonObjectRoot(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "array root", content: "[]"},
		{name: "scalar root", content: "42"},
		{name: "string root", content: `"tokens"`},
		{name: "null root", content: "null"},
		{name: "true root", content: "true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireSingleFinding(t, ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content)),
				ruleTokenLeafStructure, SeverityError, 1)
		})
	}
}

// -----------------------------------------------------------------------------
// Item 13 — top-level must be a root group: a leaf root collapses to a
// single finding so nested violations cannot hide behind a well-typed leaf
// -----------------------------------------------------------------------------

func TestValidateTokensLeafRoot(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "string $value at top level",
			content: `{"$type": "color", "$value": "#0055ff"}`,
		},
		{
			name:    "$value only at top level",
			content: `{"$value": "#0055ff"}`,
		},
		{
			name:    "leaf root with unknown $type",
			content: `{"$type": "shadow", "$value": "blur"}`,
		},
		{
			name: "leaf root wrapping nested violations",
			content: `{
  "$type": "color",
  "$value": "#0055ff",
  "nested": { "unknownType": { "$type": "shadow", "$value": "x" }, "noType": { "$value": 1 } }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireSingleFinding(t, ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content)),
				ruleTokenLeafStructure, SeverityError, 1)
		})
	}
}

// -----------------------------------------------------------------------------
// Item 3 — structural values that are not objects
// -----------------------------------------------------------------------------

func TestValidateTokensNonObjectValue(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "string group value",
			content: `{"color": "blue"}`,
		},
		{
			name:    "number group value",
			content: `{"spacing": 42}`,
		},
		{
			name:    "array group value",
			content: `{"color": ["#fff", "#000"]}`,
		},
		{
			name:    "boolean group value",
			content: `{"dark": true}`,
		},
		{
			name:    "null group value",
			content: `{"color": null}`,
		},
		{
			name: "scalar mixed with valid groups",
			content: `{
  "color": { "brand": { "$type": "color", "$value": "#0055ff" } },
  "spacing": 8
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content))
			require.NotEmpty(t, findings, "a non-object structural value must be reported")
			for _, f := range findings {
				assert.Equal(t, ruleTokenLeafStructure, f.Rule)
				assert.Contains(t, f.Message, "must be an object")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Item 4 — leaf missing $type
// -----------------------------------------------------------------------------

func TestValidateTokensMissingType(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "string $value without $type",
			content: `{"color": {"primary": {"$value": "#0055ff"}}}`,
		},
		{
			name:    "number $value without $type",
			content: `{"number": {"scale": {"$value": 1.25}}}`,
		},
		{
			name:    "object $value without $type",
			content: `{"color": {"swatch": {"$value": {"r": 0, "g": 0, "b": 0}}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := requireSingleFinding(t, ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content)),
				ruleTokenLeafStructure, SeverityError, 1)
			assert.Contains(t, f.Message, "missing $type")
		})
	}
}

// -----------------------------------------------------------------------------
// Item 5 — non-string $type
// -----------------------------------------------------------------------------

func TestValidateTokensNonStringType(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "numeric $type", content: `{"color": {"primary": {"$type": 7, "$value": "#fff"}}}`},
		{name: "boolean $type", content: `{"color": {"primary": {"$type": true, "$value": "#fff"}}}`},
		{name: "null $type", content: `{"color": {"primary": {"$type": null, "$value": "#fff"}}}`},
		{name: "object $type", content: `{"color": {"primary": {"$type": {"name": "color"}, "$value": "#fff"}}}`},
		{name: "array $type", content: `{"color": {"primary": {"$type": ["color"], "$value": "#fff"}}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := requireSingleFinding(t, ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content)),
				ruleTokenTypeMembership, SeverityError, 1)
			assert.Contains(t, f.Message, "$type")
		})
	}
}

// -----------------------------------------------------------------------------
// Item 6 — unknown $type strings are errors, never a silent pass
// -----------------------------------------------------------------------------

func TestValidateTokensUnknownType(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantFrag string
	}{
		{name: "near-miss colour", content: `{"color": {"primary": {"$type": "colour", "$value": "#fff"}}}`, wantFrag: `"colour"`},
		{name: "unspecified shadow", content: `{"shadow": {"card": {"$type": "shadow", "$value": "4px"}}}`, wantFrag: `"shadow"`},
		{name: "typography", content: `{"type": {"body": {"$type": "typography", "$value": {}}}}`, wantFrag: `"typography"`},
		{name: "empty string type", content: `{"color": {"primary": {"$type": "", "$value": "#fff"}}}`, wantFrag: `""`},
		{name: "case-mismatched Color", content: `{"color": {"primary": {"$type": "Color", "$value": "#fff"}}}`, wantFrag: `"Color"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := requireSingleFinding(t, ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content)),
				ruleTokenTypeMembership, SeverityError, 1)
			assert.Contains(t, f.Message, "unknown $type")
			assert.Contains(t, f.Message, tt.wantFrag)
		})
	}
}

// -----------------------------------------------------------------------------
// Item 7 — $type present but no $value: the group walks, but the violation
// is reported
// -----------------------------------------------------------------------------

func TestValidateTokensTypeWithoutValue(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "known type without $value",
			content: `{"color": {"brand": {"$type": "color"}}}`,
		},
		{
			name:    "type with children and no $value also reports the children's own violations",
			content: `{"color": {"brand": {"$type": "color", "primary": {"$type": "shadow", "$value": "x"}}}}`,
		},
		{
			name:    "unknown type without $value",
			content: `{"shadow": {"card": {"$type": "shadow"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content))
			require.NotEmpty(t, findings, "$type without $value must be reported")
			structural := 0
			for _, f := range findings {
				if f.Rule == ruleTokenLeafStructure && strings.Contains(f.Message, "has $type but no $value") {
					structural++
				}
			}
			assert.Equal(t, 1, structural,
				"exactly one has-$type-but-no-$value finding expected, got %v", findings)
		})
	}
}

// -----------------------------------------------------------------------------
// Item 8 — leaf carrying both $value and child keys
// -----------------------------------------------------------------------------

func TestValidateTokensLeafWithChildren(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "valid type, string value, one child",
			content: `{"color": {"primary": {"$type": "color", "$value": "#fff", "note": {"$type": "color", "$value": "#000"}}}}`,
		},
		{
			name:    "children with metadata-only shapes",
			content: `{"color": {"primary": {"$type": "color", "$value": "#fff", "note": {"$description": "why"}}}}`,
		},
		{
			name:    "leaf with children also missing $type",
			content: `{"color": {"primary": {"$value": "#fff", "note": {"deep": true}}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := ValidateTokens("design/tokens/bad.tokens.json", []byte(tt.content))
			require.NotEmpty(t, findings, "a leaf with children must be reported")
			leafFindings := 0
			for _, f := range findings {
				if strings.Contains(f.Message, "child nodes but also $value") {
					leafFindings++
				}
			}
			assert.Equal(t, 1, leafFindings,
				"exactly one leaf-with-children finding expected, got %v", findings)
		})
	}
}

// -----------------------------------------------------------------------------
// Item 9 — resolving aliases
// -----------------------------------------------------------------------------

func TestValidateTokensResolvingAliases(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "alias to a leaf token",
			content: `{
  "color": {
    "neutral": { "900": { "$type": "color", "$value": "#0a0a0a" } },
    "text": { "$type": "color", "$value": "{color.neutral.900}" }
  }
}`,
		},
		{
			name: "alias to a group node",
			content: `{
  "color": {
    "brand": { "$type": "color", "$value": "#0055ff" },
    "aliasedGroup": { "$type": "color", "$value": "{color.brand}" }
  }
}`,
		},
		{
			name: "single-segment alias to a top-level group",
			content: `{
  "color": {
    "primary": { "$type": "color", "$value": "{color}" }
  }
}`,
		},
		{
			name: "chain of two aliases terminates at a real token",
			content: `{
  "color": {
    "neutral": { "900": { "$type": "color", "$value": "#0a0a0a" } },
    "text": { "$type": "color", "$value": "{color.brand.text}" },
    "brand": { "text": { "$type": "color", "$value": "{color.neutral.900}" } }
  }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireNoFindings(t, ValidateTokens("design/tokens/color.tokens.json", []byte(tt.content)))
		})
	}
}

// -----------------------------------------------------------------------------
// Item 10 — dangling alias references
// -----------------------------------------------------------------------------

func TestValidateTokensDanglingAlias(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantLine      int
		wantAliasText string
	}{
		{
			name: "missing deepest segment",
			content: `{
  "color": { "primary": { "$type": "color", "$value": "{color.brand.primary}" } }
}`,
			wantLine:      2,
			wantAliasText: "{color.brand.primary}",
		},
		{
			name: "missing top-level group",
			content: `{
  "color": { "primary": { "$type": "color", "$value": "{spacing.base}" } }
}`,
			wantLine:      2,
			wantAliasText: "{spacing.base}",
		},
		{
			name: "alias pointing at an empty file",
			content: `{
  "color": { "primary": { "$type": "color", "$value": "{missing}" } }
}`,
			wantLine:      2,
			wantAliasText: "{missing}",
		},
		{
			name: "alias path traveling through $extensions does not resolve",
			content: `{
  "color": {
    "primary": { "$type": "color", "$value": "{color.$extensions.org.tool.fallback}" },
    "$extensions": { "org": { "tool": { "fallback": "#0055ff" } } }
  }
}`,
			wantLine:      3,
			wantAliasText: "{color.$extensions.org.tool.fallback}",
		},
		{
			name: "spaces inside the braces make the path unresolvable",
			content: `{
  "color": {
    "neutral": { "900": { "$type": "color", "$value": "#0a0a0a" } },
    "spaced": { "$type": "color", "$value": "{ color.neutral.900 }" }
  }
}`,
			wantLine:      4,
			wantAliasText: "{ color.neutral.900 }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := requireSingleFinding(t, ValidateTokens("design/tokens/color.tokens.json", []byte(tt.content)),
				ruleTokenAliasDangling, SeverityError, tt.wantLine)
			assert.Contains(t, f.Message, tt.wantAliasText)
			assert.Contains(t, f.Message, "does not resolve")
		})
	}
}

// -----------------------------------------------------------------------------
// Item 11 — cycles report exactly once per distinct cycle
// -----------------------------------------------------------------------------

func TestValidateTokensAliasCycle(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantLine int
	}{
		{
			name: "two-token cycle",
			content: `{
  "a": { "$type": "color", "$value": "{b}" },
  "b": { "$type": "color", "$value": "{a}" }
}`,
			wantLine: 2,
		},
		{
			name: "three-token cycle",
			content: `{
  "a": { "$type": "color", "$value": "{b}" },
  "b": { "$type": "color", "$value": "{c}" },
  "c": { "$type": "color", "$value": "{a}" }
}`,
			wantLine: 2,
		},
		{
			name: "cycle of nested paths",
			content: `{
  "color": {
    "a": { "$type": "color", "$value": "{color.b}" },
    "b": { "$type": "color", "$value": "{color.a}" }
  }
}`,
			wantLine: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := ValidateTokens("design/tokens/cycle.tokens.json", []byte(tt.content))
			require.Len(t, findings, 1,
				"a cycle must report exactly once regardless of how many tokens participate: %v", findings)
			f := findings[0]
			assert.Equal(t, ruleTokenAliasCycle, f.Rule)
			assert.Equal(t, SeverityError, f.Severity)
			assert.Equal(t, tt.wantLine, f.Line)
			assert.Contains(t, f.Message, "alias cycle")
			assert.Contains(t, f.Message, "->", "cycle members should be listed as a chain")
		})
	}
}

func TestValidateTokensMultipleDistinctCycles(t *testing.T) {
	// Two disjoint 2-cycles: exactly one finding per cycle, and cycle
	// findings sort before the dangling finding (rule name order) at the
	// same line.
	content := `{
  "a": { "$type": "color", "$value": "{b}" },
  "b": { "$type": "color", "$value": "{a}" },
  "c": { "$type": "color", "$value": "{d}" },
  "d": { "$type": "color", "$value": "{c}" },
  "lonely": { "$type": "color", "$value": "{nowhere}" }
}`
	findings := ValidateTokens("design/tokens/cycles.tokens.json", []byte(content))
	require.Len(t, findings, 3, "two disjoint cycles plus one dangling reference")

	cycles := 0
	for _, f := range findings {
		if f.Rule == ruleTokenAliasCycle {
			cycles++
		}
	}
	assert.Equal(t, 2, cycles, "each distinct cycle reports once")
	assert.Equal(t, ruleTokenAliasDangling, findings[2].Rule,
		"token_alias_cycle sorts before token_alias_dangling at equal lines")
	assert.Contains(t, findings[2].Message, "{nowhere}")
}

func TestValidateTokensAliasCycleNULDedupKeys(t *testing.T) {
	// JSON keys may legally contain a NUL byte via \u0000. The two
	// cycles below have member sets {"a", "b<NUL>c"} and {"a<NUL>b",
	// "c"}, which a naive delimiter join collapses to one dedup key —
	// the dedup key quotes members instead, so both cycles report.
	content := `{
  "a": { "$type": "color", "$value": "{b\u0000c}" },
  "b\u0000c": { "$type": "color", "$value": "{a}" },
  "a\u0000b": { "$type": "color", "$value": "{c}" },
  "c": { "$type": "color", "$value": "{a\u0000b}" }
}`
	findings := ValidateTokens("design/tokens/cycles.tokens.json", []byte(content))
	require.Len(t, findings, 2,
		"NUL-bearing member paths must not collapse two distinct cycles: %v", findings)
	for _, f := range findings {
		assert.Equal(t, ruleTokenAliasCycle, f.Rule)
		assert.Equal(t, SeverityError, f.Severity)
		assert.Contains(t, f.Message, "alias cycle")
	}
	assert.Equal(t, 2, findings[0].Line, "first cycle anchors at its discovering leaf")
	assert.Equal(t, 4, findings[1].Line, "second cycle anchors at its discovering leaf")
}

// -----------------------------------------------------------------------------
// Items 12 + alias-target exclusions — $extensions is invisible to every
// rule: never validated, never a dangling source, never a resolution target
// -----------------------------------------------------------------------------

func TestValidateTokensExtensionsInvisible(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "$extensions dangling aliases are never reported",
			content: `{
  "color": {
    "primary": { "$type": "color", "$value": "#0055ff" },
    "$extensions": { "org.tool": { "$value": "{does.not.exist}" } }
  }
}`,
		},
		{
			name: "$extensions invalid subtrees are never reported",
			content: `{
  "color": {
    "primary": { "$type": "color", "$value": "#0055ff" },
    "$extensions": {
      "org.tool": { "nested": { "deep": { "$value": "no $type here" } }, "$type": "nope" }
    }
  }
}`,
		},
		{
			name: "cycle hidden entirely inside $extensions is never reported",
			content: `{
  "color": {
    "primary": { "$type": "color", "$value": "#0055ff" },
    "$extensions": {
      "org.tool": {
        "a": { "$value": "{b}" },
        "b": { "$value": "{a}" }
      }
    }
  }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireNoFindings(t, ValidateTokens("design/tokens/color.tokens.json", []byte(tt.content)))
		})
	}
}

// -----------------------------------------------------------------------------
// Item 14 — ValidateTokensDir
// -----------------------------------------------------------------------------

func writeTokensFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestValidateTokensDirMissingDirs(t *testing.T) {
	tests := []struct {
		name   string
		layout func(t *testing.T, root string)
	}{
		{
			name:   "missing design directory entirely",
			layout: func(t *testing.T, root string) {},
		},
		{
			name: "design exists but has no tokens directory",
			layout: func(t *testing.T, root string) {
				require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "wireframes"), 0o755))
			},
		},
		{
			name: "tokens directory exists but is empty",
			layout: func(t *testing.T, root string) {
				require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "tokens"), 0o755))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.layout(t, root)
			findings, err := ValidateTokensDir(root)
			require.NoError(t, err)
			require.NotNil(t, findings, "a completed run must return a non-nil slice")
			assert.Empty(t, findings)
		})
	}
}

func TestValidateTokensDirCleanFiles(t *testing.T) {
	root := t.TempDir()
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "color.tokens.json"), validTokensDocument)
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "spacing.tokens.json"), validFileBody("spacing"))

	findings, err := ValidateTokensDir(root)
	require.NoError(t, err)
	require.NotNil(t, findings)
	assert.Empty(t, findings)
}

func TestValidateTokensDirNonTokenFilesIgnored(t *testing.T) {
	root := t.TempDir()
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "index.json"), "not even json")
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "notes.md"), "# notes\n")
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "color.tokens.txt"), "wrong extension")
	// One genuinely clean tokens file so the run has something to validate.
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "color.tokens.json"), validTokensDocument)

	findings, err := ValidateTokensDir(root)
	require.NoError(t, err, "non-matching files must be ignored, not parsed")
	require.NotNil(t, findings)
	assert.Empty(t, findings)
}

func TestValidateTokensDirPerFileFindings(t *testing.T) {
	root := t.TempDir()
	// color.tokens.json: dangling alias on line 2.
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "color.tokens.json"), `{
  "color": { "primary": { "$type": "color", "$value": "{missing.token}" } }
}`)
	// spacing.tokens.json: unknown type on line 2 — a different rule so
	// per-file attribution and sorting are both observable.
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "spacing.tokens.json"), `{
  "spacing": { "base": { "$type": "em", "$value": "4px" } }
}`)

	findings, err := ValidateTokensDir(root)
	require.NoError(t, err, "per-file rule violations are findings, not errors")
	require.Len(t, findings, 2)

	// Sorted by file: color.tokens.json before spacing.tokens.json.
	first := findings[0]
	assert.Equal(t, ruleTokenAliasDangling, first.Rule)
	assert.Equal(t, filepath.ToSlash(filepath.Join(DirName, "tokens", "color.tokens.json")), first.File)
	assert.Equal(t, 2, first.Line)
	assert.Equal(t, SeverityError, first.Severity)

	second := findings[1]
	assert.Equal(t, ruleTokenTypeMembership, second.Rule)
	assert.Equal(t, filepath.ToSlash(filepath.Join(DirName, "tokens", "spacing.tokens.json")), second.File)
	assert.Equal(t, 2, second.Line)
	assert.Equal(t, SeverityError, second.Severity)
}

func TestValidateTokensDirParseErrorStampsFile(t *testing.T) {
	root := t.TempDir()
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "broken.tokens.json"), "{\nbroken")

	findings, err := ValidateTokensDir(root)
	require.NoError(t, err, "a malformed tokens file is a finding, not an I/O error")
	f := requireSingleFinding(t, findings, ruleTokenJSONParse, SeverityError, 2)
	assert.Equal(t, filepath.ToSlash(filepath.Join(DirName, "tokens", "broken.tokens.json")), f.File)
}

func TestValidateTokensDirAliasScopeIsPerFile(t *testing.T) {
	root := t.TempDir()
	// color.tokens.json defines the shared token.
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "color.tokens.json"),
		`{"color": {"brand": {"primary": {"$type": "color", "$value": "#0055ff"}}}}`)
	// typography.tokens.json references it across the file boundary:
	// per-file resolution makes that dangling even though the target
	// exists elsewhere in the tier set.
	writeTokensFile(t, root, filepath.Join(DirName, "tokens", "typography.tokens.json"), `{
  "font": {
    "body": { "$type": "fontFamily", "$value": "Inter" },
    "size": { "$type": "dimension", "$value": "{color.brand.primary}" }
  }
}`)

	findings, err := ValidateTokensDir(root)
	require.NoError(t, err)
	require.Len(t, findings, 1, "cross-file aliases must not resolve")
	assert.Equal(t, ruleTokenAliasDangling, findings[0].Rule)
	assert.Equal(t, filepath.ToSlash(filepath.Join(DirName, "tokens", "typography.tokens.json")), findings[0].File)
	assert.Equal(t, 4, findings[0].Line)
	assert.Contains(t, findings[0].Message, "{color.brand.primary}")
}

func TestValidateTokensDirReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0o000 does not block reads")
	}
	root := t.TempDir()
	locked := filepath.Join(root, DirName, "tokens", "locked.tokens.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(locked), 0o755))
	require.NoError(t, os.WriteFile(locked, []byte(validTokensDocument), 0o644))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	findings, err := ValidateTokensDir(root)
	require.Error(t, err, "an unreadable matched file must surface as an error")
	assert.Nil(t, findings)
	assert.Contains(t, err.Error(), "locked.tokens.json")
}

// -----------------------------------------------------------------------------
// Item 15 — ValidateTokens stamps File and Severity on every finding
// -----------------------------------------------------------------------------

func TestValidateTokensStampsFileAndSeverity(t *testing.T) {
	content := `{
  "color": { "primary": { "$type": "color", "$value": "{nope}" } },
  "size": { "big": { "$type": "nope", "$value": 1 } }
}`
	findings := ValidateTokens("design/tokens/typography.tokens.json", []byte(content))
	require.Len(t, findings, 2)
	for _, f := range findings {
		assert.Equal(t, "design/tokens/typography.tokens.json", f.File)
		assert.Equal(t, SeverityError, f.Severity)
	}
}

// -----------------------------------------------------------------------------
// Item 16 — 1-based line anchors on multi-line documents
// -----------------------------------------------------------------------------

func TestValidateTokensLineNumbers(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantRule string
		wantLine int
	}{
		{
			name: "unknown type on line 3",
			content: `{
  "color": {
    "primary": { "$type": "shadow", "$value": "x" }
  }
}`,
			wantRule: ruleTokenTypeMembership,
			wantLine: 3,
		},
		{
			name: "missing type on line 4",
			content: `{
  "color": {
    "nested": {
      "group": { "primary": { "$value": "#fff" } }
    }
  }
}`,
			wantRule: ruleTokenLeafStructure,
			wantLine: 4,
		},
		{
			name: "dangling alias on line 4",
			content: `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "{nowhere}" }
    }
  }
}`,
			wantRule: ruleTokenAliasDangling,
			wantLine: 4,
		},
		{
			name: "leaf whose object opens on the next line anchors at the key line",
			content: `{
  "color": {
    "primary":
      { "$type": "color", "$value": "{nope}" }
  }
}`,
			wantRule: ruleTokenAliasDangling,
			wantLine: 3,
		},
		{
			name: "leaf split across lines anchors at the $value line",
			content: `{
  "color": {
    "primary": { "$type": "color",
                 "$value": "{nope}" }
  }
}`,
			wantRule: ruleTokenAliasDangling,
			wantLine: 3,
		},
		{
			name:     "CRLF documents keep 1-based lines",
			content:  "{\r\n  \"color\": {\r\n    \"primary\": { \"$type\": \"ghost\", \"$value\": \"x\" }\r\n  }\r\n}",
			wantRule: ruleTokenTypeMembership,
			wantLine: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireSingleFinding(t, ValidateTokens("design/tokens/lines.tokens.json", []byte(tt.content)),
				tt.wantRule, SeverityError, tt.wantLine)
		})
	}
}

func TestValidateTokensLineNumbersMultipleFindings(t *testing.T) {
	// Two violations on distinct lines come back sorted by line.
	content := `{
  "color": {
    "noType": { "$value": "#fff" },
    "unknown": { "$type": "ghost", "$value": "x" }
  }
}`
	findings := ValidateTokens("design/tokens/lines.tokens.json", []byte(content))
	require.Len(t, findings, 2)
	assert.Equal(t, 3, findings[0].Line, "findings sort by line within one file")
	assert.Equal(t, ruleTokenLeafStructure, findings[0].Rule)
	assert.Contains(t, findings[0].Message, "missing $type")
	assert.Equal(t, 4, findings[1].Line)
	assert.Equal(t, ruleTokenTypeMembership, findings[1].Rule)
}

// -----------------------------------------------------------------------------
// Determinism — same input, same findings, every run
// -----------------------------------------------------------------------------

func TestValidateTokensDeterministicOrder(t *testing.T) {
	content := `{
  "z": { "leaf": { "$value": "#fff" } },
  "a": { "leaf": { "$type": "ghost", "$value": 1 } }
}`
	want := ValidateTokens("design/tokens/order.tokens.json", []byte(content))
	require.Len(t, want, 2)
	for range 10 {
		got := ValidateTokens("design/tokens/order.tokens.json", []byte(content))
		assert.Equal(t, want, got, "repeated runs must produce byte-identical findings")
	}
	// Within one run, ordering is by line: the z-group leaf sits on line 2,
	// the a-group leaf on line 3 — file paths cannot mask line order.
	assert.Equal(t, 2, want[0].Line)
	assert.Equal(t, 3, want[1].Line)
}

// -----------------------------------------------------------------------------
// Sanity — the shared clean fixture stays clean and every fixture helper
// wraps without breaking it
// -----------------------------------------------------------------------------

func TestTokensFixturesValid(t *testing.T) {
	requireNoFindings(t, ValidateTokens("design/tokens/color.tokens.json", []byte(validTokensDocument)))
	requireNoFindings(t, ValidateTokens("design/tokens/spacing.tokens.json", []byte(validFileBody("spacing"))))
}
