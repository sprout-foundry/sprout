package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnvSimple_SproutSet(t *testing.T) {
	t.Setenv("SPROUT_ENVSIMPLE_A", "sprout-val")
	result := GetEnvSimple("ENVSIMPLE_A")
	assert.Equal(t, "sprout-val", result)
}

func TestGetEnvSimple_NotSet(t *testing.T) {
	result := GetEnvSimple("ENVSIMPLE_NONEXISTENT")
	assert.Equal(t, "", result)
}

func TestSetEnv(t *testing.T) {
	suffix := "ENVTEST_SETVAR"
	err := SetEnv(suffix, "testvalue")
	require.NoError(t, err)
	assert.Equal(t, "testvalue", GetEnvSimple(suffix))
}

func TestLookupEnv_Found(t *testing.T) {
	t.Setenv("SPROUT_ENVTEST_LOOKUP", "val")
	val, ok := LookupEnv("ENVTEST_LOOKUP")
	assert.True(t, ok)
	assert.Equal(t, "val", val)
}

func TestUnsetEnv(t *testing.T) {
	suffix := "ENVTEST_UNSETVAR"
	t.Setenv("SPROUT_"+suffix, "val")
	UnsetEnv(suffix)
	_, ok := LookupEnv(suffix)
	assert.False(t, ok)
}
