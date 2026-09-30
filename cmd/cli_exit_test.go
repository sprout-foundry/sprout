//go:build !js

package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newExitTestTree() *cobra.Command {
	root := &cobra.Command{Use: "tool", SilenceErrors: true, SilenceUsage: true}
	leaf := &cobra.Command{
		Use:  "leaf",
		Args: cobra.ExactArgs(1),
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	leaf.Flags().Bool("a", false, "")
	leaf.Flags().Bool("b", false, "")
	leaf.Flags().String("need", "", "")
	leaf.MarkFlagsMutuallyExclusive("a", "b")
	req := &cobra.Command{Use: "req", RunE: func(*cobra.Command, []string) error { return nil }}
	req.Flags().String("must", "", "")
	_ = req.MarkFlagRequired("must")
	one := &cobra.Command{Use: "one", RunE: func(*cobra.Command, []string) error { return nil }}
	one.Flags().Bool("x", false, "")
	one.Flags().Bool("y", false, "")
	one.MarkFlagsOneRequired("x", "y")
	root.AddCommand(leaf, req, one)
	installUsageErrorHooks(root)
	return root
}

func TestExitCodeForCobraErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"lef"}},
		{"unknown flag", []string{"leaf", "x", "--nope"}},
		{"unknown shorthand", []string{"leaf", "x", "-z"}},
		{"arg count", []string{"leaf"}},
		{"bad flag value", []string{"leaf", "x", "--a=maybe"}},
		{"mutually exclusive", []string{"leaf", "x", "--a", "--b"}},
		{"required flag", []string{"req"}},
		{"one required", []string{"one"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newExitTestTree()
			root.SetArgs(tc.args)
			err := root.Execute()
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCodeFor(err), "error: %v", err)
		})
	}
}

func TestExitCodeForRuntimeErrors(t *testing.T) {
	assert.Equal(t, exitOK, exitCodeFor(nil))
	assert.Equal(t, exitFailure, exitCodeFor(errors.New("provider returned HTTP 400")))
	assert.Equal(t, exitInterrupted, exitCodeFor(fmt.Errorf("chat failed: %w", context.Canceled)))
	assert.Equal(t, exitInterrupted, exitCodeFor(errInterrupted))
	assert.Equal(t, exitFailure, exitCodeFor(markReported(errors.New("boom"))))
	assert.Equal(t, exitUsage, exitCodeFor(fmt.Errorf("wrapped: %w", usageErrorAt("sprout x", "bad"))))
}

func TestNewUsageErrorKeepsInnermostPath(t *testing.T) {
	inner := usageErrorAt("sprout a b", "bad")
	outer := newUsageError(rootCmd, inner)
	ue, ok := errors.AsType[*usageError](outer)
	require.True(t, ok)
	assert.Equal(t, "sprout a b", ue.cmdPath)
}

func TestSplitCobraSuggestions(t *testing.T) {
	headline, sugg := splitCobraSuggestions("unknown command \"agnet\" for \"sprout\"\n\nDid you mean this?\n\tagent\n\tagents\n")
	assert.Equal(t, `unknown command "agnet" for "sprout"`, headline)
	assert.Equal(t, []string{"agent", "agents"}, sugg)

	headline, sugg = splitCobraSuggestions("unknown flag: --bogus")
	assert.Equal(t, "unknown flag: --bogus", headline)
	assert.Empty(t, sugg)
}

func TestHumanizeFlagGroupError(t *testing.T) {
	root := newExitTestTree()
	root.SetArgs([]string{"leaf", "x", "--a", "--b"})
	err := root.Execute()
	require.Error(t, err)
	assert.Equal(t, "--a and --b can't be used together", humanizeFlagGroupError(err.Error()))

	root = newExitTestTree()
	root.SetArgs([]string{"one"})
	err = root.Execute()
	require.Error(t, err)
	assert.Equal(t, "one of --x, --y is required", humanizeFlagGroupError(err.Error()))

	assert.Equal(t, "something else", humanizeFlagGroupError("something else"))
}

func TestInvokedCommandPath(t *testing.T) {
	assert.Equal(t, "sprout upgrade", invokedCommandPath([]string{"upgrade", "--check", "--rollback"}))
	assert.Equal(t, "sprout", invokedCommandPath([]string{"--bogus"}))
}
