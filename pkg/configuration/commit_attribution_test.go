package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommitAttribution_DefaultOffAndValidated(t *testing.T) {
	cfg := NewConfig()
	assert.False(t, cfg.CommitAttributionEnabled(), "commit attribution must be off by default")
	assert.NoError(t, cfg.Validate())

	cfg.CommitAttribution = CommitAttributionOff
	assert.False(t, cfg.CommitAttributionEnabled())
	assert.NoError(t, cfg.Validate())

	cfg.CommitAttribution = CommitAttributionTrailer
	assert.True(t, cfg.CommitAttributionEnabled())
	assert.NoError(t, cfg.Validate())

	cfg.CommitAttribution = "co-author"
	assert.Error(t, cfg.Validate())

	var nilCfg *Config
	assert.False(t, nilCfg.CommitAttributionEnabled())
}

func TestCommitAttribution_Merge(t *testing.T) {
	base := &Config{CommitAttribution: CommitAttributionTrailer}
	assert.Equal(t, CommitAttributionTrailer, MergeConfig(base, &Config{}).CommitAttribution)
	assert.Equal(t, CommitAttributionOff, MergeConfig(base, &Config{CommitAttribution: CommitAttributionOff}).CommitAttribution)
}
