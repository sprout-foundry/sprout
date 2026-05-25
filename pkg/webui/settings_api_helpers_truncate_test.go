//go:build !js

package webui

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// truncateString
// ---------------------------------------------------------------------------

func TestTruncateString_NoTruncation(t *testing.T) {
	assert.Equal(t, "hello", truncateString("hello", 10))
	assert.Equal(t, "hello", truncateString("hello", 5))
	assert.Equal(t, "", truncateString("", 10))
}

func TestTruncateString_TruncatesLongString(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := truncateString(long, 100)
	assert.LessOrEqual(t, len(got), 100)
	assert.True(t, strings.HasSuffix(got, truncationEllipsis))
	// The content before the ellipsis should be 'a' characters
	prefix := strings.TrimSuffix(got, truncationEllipsis)
	assert.Equal(t, strings.Repeat("a", 100-len(truncationEllipsis)), prefix)
}

func TestTruncateString_MultiByteRunes(t *testing.T) {
	// Japanese characters — each rune is 3 bytes
	input := strings.Repeat("日", 100) // 300 bytes, 100 runes
	got := truncateString(input, 50)
	assert.LessOrEqual(t, len([]rune(got)), 50)
	assert.True(t, strings.HasSuffix(got, truncationEllipsis))
}

func TestTruncateString_EllipsisLength(t *testing.T) {
	// When maxLen is smaller than the ellipsis itself, just truncate
	got := truncateString("abcdef", 3)
	assert.Equal(t, "abc", got)
}

func TestTruncateString_ExactEllipsisFit(t *testing.T) {
	input := strings.Repeat("x", 20)
	got := truncateString(input, 20)
	// Equal length => no truncation
	assert.Equal(t, input, got)
}

// ---------------------------------------------------------------------------
// truncateConfigStrings
// ---------------------------------------------------------------------------

func TestTruncateConfigStrings_TruncatesSystemPrompt(t *testing.T) {
	cfg := &configuration.Config{}
	cfg.SystemPromptText = strings.Repeat("x", 200_000)
	truncateConfigStrings(cfg)
	assert.LessOrEqual(t, len(cfg.SystemPromptText), maxSettingPromptLength)
	assert.True(t, strings.HasSuffix(cfg.SystemPromptText, truncationEllipsis))
}

func TestTruncateConfigStrings_TruncatesReasoningEffort(t *testing.T) {
	cfg := &configuration.Config{}
	cfg.ReasoningEffort = strings.Repeat("a", 200)
	truncateConfigStrings(cfg)
	assert.LessOrEqual(t, len(cfg.ReasoningEffort), maxSettingEnumLength)
}

func TestTruncateConfigStrings_TruncatesResourceDirectory(t *testing.T) {
	cfg := &configuration.Config{}
	cfg.ResourceDirectory = strings.Repeat("/a", 5000)
	truncateConfigStrings(cfg)
	assert.LessOrEqual(t, len(cfg.ResourceDirectory), maxSettingPathLength)
}

func TestTruncateConfigStrings_TruncatesSubagentTypes(t *testing.T) {
	cfg := &configuration.Config{}
	cfg.SubagentTypes = map[string]configuration.SubagentType{
		"test": {Provider: strings.Repeat("p", 500), Model: strings.Repeat("m", 500)},
	}
	truncateConfigStrings(cfg)
	assert.LessOrEqual(t, len(cfg.SubagentTypes["test"].Provider), maxSettingNameLength)
	assert.LessOrEqual(t, len(cfg.SubagentTypes["test"].Model), maxSettingNameLength)
}
