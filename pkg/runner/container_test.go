package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeDocker writes a docker CLI stand-in that logs its arguments and answers
// `image inspect` and `pull` with the given exit codes.
func fakeDocker(t *testing.T, inspectRC, pullRC string) (docker, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script docker stand-in")
	}
	dir := t.TempDir()
	log = filepath.Join(dir, "calls")
	docker = filepath.Join(dir, "docker")
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
case "$1" in
  info) echo 27.0.0; exit 0 ;;
  image) exit ` + inspectRC + ` ;;
  pull)
    if [ "` + pullRC + `" != 0 ]; then
      echo 'Error response from daemon: Head "https://ghcr.example/v2/o/i/manifests/latest": denied' >&2
    fi
    exit ` + pullRC + ` ;;
esac
exit 0
`
	if err := os.WriteFile(docker, []byte(script), 0o700); err != nil { //nolint:gosec // G306: test executable
		t.Fatal(err)
	}
	return docker, log
}

func calls(t *testing.T, log string) string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func TestEnsureImagePresentLocallyDoesNotPull(t *testing.T) {
	docker, log := fakeDocker(t, "0", "1")
	l := &ContainerLauncher{Image: "ghcr.example/o/i:latest", Docker: docker}
	if err := l.EnsureImage(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := calls(t, log); strings.Contains(got, "pull") {
		t.Errorf("a present image must not be pulled; calls:\n%s", got)
	}
}

func TestEnsureImagePullsWhenMissing(t *testing.T) {
	docker, log := fakeDocker(t, "1", "0")
	var progress bytes.Buffer
	l := &ContainerLauncher{Image: "ghcr.example/o/i:latest", Docker: docker}
	if err := l.EnsureImage(context.Background(), &progress); err != nil {
		t.Fatal(err)
	}
	if got := calls(t, log); !strings.Contains(got, "pull ghcr.example/o/i:latest") {
		t.Errorf("missing image must be pulled; calls:\n%s", got)
	}
	if !strings.Contains(progress.String(), "ghcr.example/o/i:latest") {
		t.Errorf("progress should name the image: %q", progress.String())
	}
}

func TestEnsureImageUnpullableNamesTheImageAndTheFixes(t *testing.T) {
	docker, _ := fakeDocker(t, "1", "1")
	l := &ContainerLauncher{Image: "ghcr.example/o/i:latest", Docker: docker}
	err := l.EnsureImage(context.Background(), nil)
	var unavailable *ImageUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("want ImageUnavailableError, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{
		"ghcr.example/o/i:latest",
		"denied",
		"docker login ghcr.example",
		"read:packages",
		"--image",
		"sprout runner mode native",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
}

func TestEnsureImageRejectsFlagLikeReferences(t *testing.T) {
	docker, log := fakeDocker(t, "0", "0")
	for _, bad := range []string{"", "--help", "a b"} {
		l := &ContainerLauncher{Image: bad, Docker: docker}
		if err := l.EnsureImage(context.Background(), nil); err == nil {
			t.Errorf("image %q must be rejected", bad)
		}
	}
	if got := calls(t, log); got != "" {
		t.Errorf("rejected references must never reach docker; calls:\n%s", got)
	}
}

func TestImageRegistry(t *testing.T) {
	for image, want := range map[string]string{
		"ghcr.io/sprout-foundry/sprout:latest": "ghcr.io",
		"localhost:5000/img":                   "localhost:5000",
		"localhost/img":                        "localhost",
		"library/ubuntu":                       "docker.io",
		"ubuntu:24.04":                         "docker.io",
	} {
		if got := imageRegistry(image); got != want {
			t.Errorf("imageRegistry(%q) = %q, want %q", image, got, want)
		}
	}
}
