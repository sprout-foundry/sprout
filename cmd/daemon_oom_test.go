//go:build !js

package cmd

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// withPreferOOMVictimFn replaces the seam for one test and restores it.
func withPreferOOMVictimFn(fn func() error) func() {
	orig := preferOOMVictimFn
	preferOOMVictimFn = fn
	return func() { preferOOMVictimFn = orig }
}

func TestMaybePreferOOMVictim(t *testing.T) {
	t.Run("raises for autostarted daemon", func(t *testing.T) {
		called := false
		defer withPreferOOMVictimFn(func() error { called = true; return nil })()

		t.Setenv("SPROUT_DAEMON_AUTOSTARTED", "1")
		maybePreferOOMVictim(true)
		require.True(t, called)
	})

	t.Run("skips explicit daemon without marker", func(t *testing.T) {
		called := false
		defer withPreferOOMVictimFn(func() error { called = true; return nil })()

		t.Setenv("SPROUT_DAEMON_AUTOSTARTED", "")
		maybePreferOOMVictim(true)
		require.False(t, called)
	})

	t.Run("skips non-daemon process", func(t *testing.T) {
		called := false
		defer withPreferOOMVictimFn(func() error { called = true; return nil })()

		t.Setenv("SPROUT_DAEMON_AUTOSTARTED", "1")
		maybePreferOOMVictim(false)
		require.False(t, called)
	})

	t.Run("raise failure is non-fatal", func(t *testing.T) {
		called := false
		defer withPreferOOMVictimFn(func() error { called = true; return errors.New("boom") })()

		t.Setenv("SPROUT_DAEMON_AUTOSTARTED", "1")
		require.NotPanics(t, func() { maybePreferOOMVictim(true) })
		require.True(t, called)
	})
}
