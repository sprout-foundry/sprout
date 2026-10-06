//go:build !js

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigSetRejectsUnknownKeyAsUsageError(t *testing.T) {
	err := configSetCmd.RunE(configSetCmd, []string{"no_such_setting", "x"})
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCodeFor(err))
	assert.Contains(t, err.Error(), `"no_such_setting"`)
}

func TestConfigSetRequiresKeyAndValue(t *testing.T) {
	require.Error(t, configSetCmd.Args(configSetCmd, []string{"output_verbosity"}))
	require.NoError(t, configSetCmd.Args(configSetCmd, []string{"output_verbosity", "compact"}))
}
