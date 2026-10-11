package commands

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
)

func TestAttributeCommitCommand(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v0.30.0"
	t.Cleanup(func() { buildinfo.Version = old })

	off := &configuration.Config{}
	on := &configuration.Config{CommitAttribution: configuration.CommitAttributionTrailer}

	// Off by default: nothing appended, generated or not.
	assert.Equal(t, "fix: x", attributeCommitCommand(off, nil, "deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash", true, "fix: x"))
	assert.Equal(t, "fix: x", attributeCommitCommand(nil, nil, "deepinfra", "m", true, "fix: x"))

	// On, generated message: the model that wrote it is named.
	got := attributeCommitCommand(on, nil, "deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash", true, "fix: x")
	assert.Equal(t, "fix: x\n\nAssisted-by: sprout/0.30.0 (deepinfra/deepseek-ai/DeepSeek-V4.1-Flash)\n", got)
	assert.NotContains(t, strings.ToLower(got), "co-authored-by")

	// On, hand-written message with no agent: no model to name, no trailer.
	assert.Equal(t, "fix: x", attributeCommitCommand(on, nil, "", "", false, "fix: x"))
}
