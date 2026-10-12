//go:build !windows

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDaemonSpec_UsesHomeAsWorkingDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	spec := DefaultDaemonSpec()
	assert.Equal(t, home, spec.Dir)
}

func TestStartDaemon_SpawnsInSpecDir(t *testing.T) {
	tmpDir := t.TempDir()
	daemonDir := t.TempDir()
	marker := filepath.Join(tmpDir, "cwd.txt")

	spec := makeTestSpec(t, tmpDir, freePort(t))
	spec.Dir = daemonDir
	spec.StartTimeout = time.Second
	spec.DaemonCommand = []string{"sh", "-c", `pwd -P > "$0"; exit 1`, marker}

	err := StartDaemon(context.Background(), spec)
	require.Error(t, err, "the inert command never becomes healthy")

	var got []byte
	require.Eventually(t, func() bool {
		got, err = os.ReadFile(marker)
		return err == nil && len(got) > 0
	}, 5*time.Second, 20*time.Millisecond)

	want, err := filepath.EvalSymlinks(daemonDir)
	require.NoError(t, err)
	assert.Equal(t, want, strings.TrimSpace(string(got)))
}
