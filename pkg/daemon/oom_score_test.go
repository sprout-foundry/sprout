//go:build !js

package daemon

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// withOOMScoreHooks replaces the platform hooks for one test and returns a
// restore func so other tests keep the real platform behavior.
func withOOMScoreHooks(read func() (string, error), write func(string) error) func() {
	origRead, origWrite := readOOMScoreAdj, writeOOMScoreAdj
	readOOMScoreAdj, writeOOMScoreAdj = read, write
	return func() {
		readOOMScoreAdj, writeOOMScoreAdj = origRead, origWrite
	}
}

func TestRaiseOOMScoreAdj(t *testing.T) {
	t.Run("adds delta to current value", func(t *testing.T) {
		var written string
		defer withOOMScoreHooks(
			func() (string, error) { return "0\n", nil },
			func(v string) error { written = v; return nil },
		)()

		require.NoError(t, raiseOOMScoreAdj(200))
		require.Equal(t, "200", written)
	})

	t.Run("clamps at upper bound", func(t *testing.T) {
		var written string
		defer withOOMScoreHooks(
			func() (string, error) { return "900", nil },
			func(v string) error { written = v; return nil },
		)()

		require.NoError(t, raiseOOMScoreAdj(200))
		require.Equal(t, "1000", written)
	})

	t.Run("clamps at lower bound", func(t *testing.T) {
		var written string
		defer withOOMScoreHooks(
			func() (string, error) { return "-900", nil },
			func(v string) error { written = v; return nil },
		)()

		require.NoError(t, raiseOOMScoreAdj(-200))
		require.Equal(t, "-1000", written)
	})

	t.Run("propagates read error", func(t *testing.T) {
		defer withOOMScoreHooks(
			func() (string, error) { return "", errors.New("read failed") },
			func(string) error { t.Fatal("write must not run"); return nil },
		)()

		err := raiseOOMScoreAdj(200)
		require.ErrorContains(t, err, "read failed")
	})

	t.Run("propagates unparsable current value", func(t *testing.T) {
		defer withOOMScoreHooks(
			func() (string, error) { return "not-a-number", nil },
			func(string) error { t.Fatal("write must not run"); return nil },
		)()

		require.Error(t, raiseOOMScoreAdj(200))
	})

	t.Run("propagates write error", func(t *testing.T) {
		defer withOOMScoreHooks(
			func() (string, error) { return "0", nil },
			func(string) error { return errors.New("write failed") },
		)()

		err := raiseOOMScoreAdj(200)
		require.ErrorContains(t, err, "write failed")
	})
}

func TestPreferOOMVictim(t *testing.T) {
	var written string
	defer withOOMScoreHooks(
		func() (string, error) { return "0", nil },
		func(v string) error { written = v; return nil },
	)()

	require.NoError(t, PreferOOMVictim())
	require.Equal(t, "200", written)
}
