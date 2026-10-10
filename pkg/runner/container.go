package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ContainerLauncher runs each workspace in the sprout workspace image with
// the platform's hardening profile: non-root, read-only rootfs, every
// capability dropped, no-new-privileges, CPU and memory limits, the daemon
// published on loopback only. It drives the docker CLI, so Docker Desktop,
// OrbStack, Colima and Podman's docker shim all work.
type ContainerLauncher struct {
	Image string
	// Docker is the CLI to run; "docker" when empty.
	Docker string
}

func (l *ContainerLauncher) docker() string {
	if l.Docker != "" {
		return l.Docker
	}
	return "docker"
}

func containerName(workspaceID string) string { return "sprout-ws-" + workspaceID }

// DockerAvailable reports whether container mode can run here.
func DockerAvailable(ctx context.Context, docker string) error {
	if docker == "" {
		docker = "docker"
	}
	out, err := exec.CommandContext(ctx, docker, "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("container mode needs a running Docker engine (Docker Desktop, OrbStack, Colima): %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ImageUnavailableError reports that the workspace image is neither present
// locally nor pullable. The runner never holds registry credentials: the
// docker CLI pulls with Docker's own credential store, so the fix is
// `docker login` or a different image or mode.
type ImageUnavailableError struct {
	Image  string
	Output string
}

func (e *ImageUnavailableError) Error() string {
	return fmt.Sprintf(`container mode cannot pull the workspace image %s:
  %s

Fix one of:
  - authenticate Docker to the registry: docker login %s
    (the runner pulls with Docker's own credential store; for ghcr.io a
    GitHub token with the read:packages scope works as the password)
  - choose an image you can pull: sprout runner start --image <image>
  - run without containers: sprout runner mode native`, e.Image, e.Output, imageRegistry(e.Image))
}

// ValidateImage rejects references that are empty, contain whitespace or
// start with "-": the reference is passed to the docker CLI as an argument
// and must never be read as a flag.
func ValidateImage(image string) error {
	if image == "" || strings.HasPrefix(image, "-") || strings.ContainsAny(image, " \t\r\n") {
		return fmt.Errorf("invalid image reference %q", image)
	}
	return nil
}

// imageRegistry is the registry host an image reference pulls from: the first
// path component when it looks like a host (has a dot or port, or is
// localhost), otherwise Docker Hub.
func imageRegistry(image string) string {
	first, _, found := strings.Cut(image, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}
	return "docker.io"
}

// EnsureImage makes sure the workspace image is present, pulling it when it
// is not, so a missing image or registry credential fails before the runner
// goes online rather than at its first task.
func (l *ContainerLauncher) EnsureImage(ctx context.Context, progress io.Writer) error {
	if strings.TrimSpace(l.Image) == "" {
		return errors.New("container mode has no workspace image configured; pass --image <image>")
	}
	if err := ValidateImage(l.Image); err != nil {
		return err
	}
	if exec.CommandContext(ctx, l.docker(), "image", "inspect", l.Image).Run() == nil { //nolint:gosec // G204: docker CLI with runner-built args
		return nil
	}
	if progress != nil {
		_, _ = fmt.Fprintf(progress, "Pulling workspace image %s…\n", l.Image)
	}
	out, err := exec.CommandContext(ctx, l.docker(), "pull", l.Image).CombinedOutput() //nolint:gosec // G204: docker CLI with runner-built args
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &ImageUnavailableError{Image: l.Image, Output: strings.TrimSpace(string(out))}
	}
	return nil
}

func (l *ContainerLauncher) Start(ctx context.Context, task WorkspaceTask) (*Workspace, error) {
	dir, err := workspaceDir(task.WorkspaceID)
	if err != nil {
		return nil, err
	}
	// A local-directory workspace bind-mounts the user's real directory at
	// /workspace (the allowlist was checked before the task started). A
	// clone workspace keeps the runner-managed volume it owns.
	workspaceMount := ""
	if task.WorkspaceDir != "" {
		if task.RepoURL != "" {
			// A local directory is served in place; a repo URL beside
			// it would ask the image to clone into the user's real
			// files, which it must never do.
			return nil, fmt.Errorf("start task names both a repo and a local directory")
		}
		workspaceMount = task.WorkspaceDir + ":/workspace"
	} else {
		volume := filepath.Join(dir, "volume")
		if err := os.MkdirAll(volume, 0o700); err != nil {
			return nil, fmt.Errorf("creating workspace volume: %w", err)
		}
		// The container runs as uid 1000, which need not match the host user.
		if err := os.Chmod(volume, 0o777); err != nil { //nolint:gosec // G302: the container's uid 1000 must write it; parent dir is 0700
			return nil, err
		}
		workspaceMount = volume + ":/workspace"
	}
	env := workspaceEnv(task)
	if task.WorkspaceDir != "" {
		// Inside the container the directory is the /workspace mount;
		// the host path means nothing there and would only leak it.
		env["WORKSPACE_DIR"] = "/workspace"
	}
	envFile, err := writeEnvFile(dir, env)
	if err != nil {
		return nil, fmt.Errorf("writing workspace env: %w", err)
	}
	defer func() { _ = os.Remove(envFile) }()

	name := containerName(task.WorkspaceID)
	_ = exec.CommandContext(ctx, l.docker(), "rm", "-f", name).Run() //nolint:gosec // G204: docker CLI with runner-built args
	args := []string{
		"run", "-d", "--name", name,
		"--label", "sprout.runner.workspace=" + task.WorkspaceID,
		"--env-file", envFile,
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--cpus", "2", "--memory", "4g",
		"--tmpfs", "/tmp:size=128m",
		"--tmpfs", "/run:size=16m",
		"--tmpfs", "/home/sprout:size=32m,uid=1000,gid=1000,mode=0700",
		"-v", workspaceMount,
		"-p", "127.0.0.1::" + strconv.Itoa(daemonPort),
		l.Image,
	}
	if out, err := exec.CommandContext(ctx, l.docker(), args...).CombinedOutput(); err != nil { //nolint:gosec // G204: docker CLI with runner-built args
		return nil, fmt.Errorf("docker run: %s", strings.TrimSpace(string(out)))
	}
	port, err := l.hostPort(ctx, name)
	if err != nil {
		_ = exec.CommandContext(ctx, l.docker(), "rm", "-f", name).Run() //nolint:gosec // G204: docker CLI with runner-built args
		return nil, err
	}
	ws := &Workspace{ID: task.WorkspaceID, Port: port, Handle: name}
	if err := waitReady(ctx, port); err != nil {
		logs, _ := exec.CommandContext(ctx, l.docker(), "logs", "--tail", "20", name).CombinedOutput() //nolint:gosec // G204: docker CLI with runner-built args
		_ = exec.CommandContext(ctx, l.docker(), "rm", "-f", name).Run()                               //nolint:gosec // G204: docker CLI with runner-built args
		return nil, fmt.Errorf("%w\n%s", err, logs)
	}
	return ws, nil
}

func (l *ContainerLauncher) hostPort(ctx context.Context, name string) (int, error) {
	out, err := exec.CommandContext(ctx, l.docker(), "port", name, strconv.Itoa(daemonPort)+"/tcp").Output() //nolint:gosec // G204: docker CLI with runner-built args
	if err != nil {
		return 0, fmt.Errorf("reading published port: %w", err)
	}
	// "127.0.0.1:49153" (one line per published address).
	line := strings.TrimSpace(string(bytes.SplitN(out, []byte("\n"), 2)[0]))
	port, err := strconv.Atoi(line[strings.LastIndex(line, ":")+1:])
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("unexpected docker port output %q", line)
	}
	return port, nil
}

func (l *ContainerLauncher) Stop(ctx context.Context, ws *Workspace) error {
	if ws == nil {
		return nil
	}
	if out, err := exec.CommandContext(ctx, l.docker(), "stop", ws.Handle).CombinedOutput(); err != nil { //nolint:gosec // G204: docker CLI with runner-built args
		return fmt.Errorf("docker stop: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (l *ContainerLauncher) Destroy(ctx context.Context, workspaceID string, _ *Workspace) error {
	_ = exec.CommandContext(ctx, l.docker(), "rm", "-f", containerName(workspaceID)).Run() //nolint:gosec // G204: docker CLI with runner-built args
	dir, err := workspaceDir(workspaceID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
