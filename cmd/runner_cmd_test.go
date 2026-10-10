package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/runner"
)

// runnerFakeDocker is a docker CLI stand-in whose engine is up and whose
// registry denies every pull of an image that is not present locally.
func runnerFakeDocker(t *testing.T, imagePresent bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script docker stand-in")
	}
	inspect := "1"
	if imagePresent {
		inspect = "0"
	}
	docker := filepath.Join(t.TempDir(), "docker")
	script := `#!/bin/sh
case "$1" in
  info) echo 27.0.0; exit 0 ;;
  image) exit ` + inspect + ` ;;
  pull) echo 'denied' >&2; exit 1 ;;
esac
exit 0
`
	if err := os.WriteFile(docker, []byte(script), 0o700); err != nil { //nolint:gosec // G306: test executable
		t.Fatal(err)
	}
	return docker
}

func TestRunnerStartRefusesContainerModeWithoutItsImage(t *testing.T) {
	docker := runnerFakeDocker(t, false)
	st := &runner.State{Mode: runner.ModeContainer, Image: "ghcr.example/o/i:latest"}
	_, _, err := launcherFor(context.Background(), &bytes.Buffer{}, st, docker)
	var unavailable *runner.ImageUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("want ImageUnavailableError, got %v", err)
	}
	if !strings.Contains(err.Error(), "docker login ghcr.example") {
		t.Errorf("message should name the registry to log in to:\n%v", err)
	}
}

func TestRunnerStartUsesAPresentImage(t *testing.T) {
	docker := runnerFakeDocker(t, true)
	st := &runner.State{Mode: runner.ModeContainer, Image: "ghcr.example/o/i:latest"}
	launcher, capability, err := launcherFor(context.Background(), &bytes.Buffer{}, st, docker)
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := launcher.(*runner.ContainerLauncher); !ok || l.Image != st.Image || capability != "docker" {
		t.Errorf("unexpected launcher %#v (%s)", launcher, capability)
	}
}

func TestRunnerLinkPreflightRefusesAnUnpullableImage(t *testing.T) {
	docker := runnerFakeDocker(t, false)
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	st := &runner.State{Mode: runner.ModeContainer, Image: "ghcr.example/o/i:latest"}
	if err := preflightLinkImage(context.Background(), cmd, st, docker); err == nil {
		t.Fatal("link must refuse an image it cannot pull")
	}
	st.Mode = runner.ModeNative
	if err := preflightLinkImage(context.Background(), cmd, st, docker); err != nil {
		t.Fatalf("native mode needs no image: %v", err)
	}
}

func TestRunnerImageFlagIsValidatedAndSaved(t *testing.T) {
	t.Cleanup(func() { runnerImage = "" })
	st := &runner.State{Image: runner.DefaultImage}
	runnerImage = "--privileged"
	if err := applyImageFlag(st); err == nil {
		t.Error("a flag-like image must be rejected")
	}
	runnerImage = "registry.example/team/sprout:1"
	if err := applyImageFlag(st); err != nil || st.Image != runnerImage {
		t.Errorf("image = %q, err = %v", st.Image, err)
	}
	runnerImage = ""
	if err := applyImageFlag(st); err != nil || st.Image != "registry.example/team/sprout:1" {
		t.Errorf("no flag must keep the saved image; got %q", st.Image)
	}
}

func TestRunnerUnlinkMessageDescribesBackendRevoke(t *testing.T) {
	for _, want := range []string{"Runners page", "self-hosted backend", "no runner-side revoke"} {
		if !strings.Contains(unlinkMessage, want) {
			t.Errorf("unlink message lacks %q", want)
		}
	}
}
