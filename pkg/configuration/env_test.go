package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnv_SproutPreferred(t *testing.T) {
	t.Setenv("SPROUT_ENVTEST_A", "sprout")
	result := GetEnv("SPROUT_ENVTEST_A", "LEDIT_ENVTEST_A")
	assert.Equal(t, "sprout", result)
}

func TestGetEnvSimple_LeditOnly(t *testing.T) {
	t.Setenv("LEDIT_ENVSIMPLE_B", "ledit-val")

	result := GetEnvSimple("ENVSIMPLE_B")
	assert.Equal(t, "ledit-val", result)
}

func TestSetEnv(t *testing.T) {
	suffix := "ENVTEST_SETVAR"
	err := SetEnv(suffix, "testvalue")
	require.NoError(t, err)

	assert.Equal(t, "testvalue", GetEnvSimple(suffix))
}

func TestUnsetEnv(t *testing.T) {
	suffix := "ENVTEST_UNSETVAR"
	t.Setenv("SPROUT_"+suffix, "val")
	t.Setenv("LEDIT_"+suffix, "val")

	UnsetEnv(suffix)

	_, ok := LookupEnv(suffix)
	assert.False(t, ok)
}
