package webui

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// SSH binary preparation & download
// ---------------------------------------------------------------------------

func currentExecutableForSSH() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to determine current ledit executable: %w", err)
	}
	executablePath, err = filepath.EvalSymlinks(executablePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve current ledit executable: %w", err)
	}
	return executablePath, nil
}

func prepareLocalSSHBinary(remotePlatform, remoteArch string, logger *sshLaunchLogger) (string, error) {
	artifactPath, artifactErr := ensureLocalSSHBinaryArtifact(remotePlatform, remoteArch, logger)
	if artifactErr == nil && artifactPath != "" {
		return artifactPath, nil
	}

	if runtime.GOOS == remotePlatform && runtime.GOARCH == remoteArch {
		logger.Logf("release artifact unavailable for %s/%s, reusing current executable", remotePlatform, remoteArch)
		return currentExecutableForSSH()
	}

	logger.Logf("release artifact unavailable for %s/%s, falling back to local go build", remotePlatform, remoteArch)

	goBinary, err := exec.LookPath("go")
	if err != nil {
		if errors.Is(artifactErr, errNoReleaseTagForArtifact) {
			logger.Logf("no release tag for current build; attempting latest artifact as cross-arch fallback for %s/%s", remotePlatform, remoteArch)
			if latestPath, latestErr := ensureLocalSSHBinaryArtifactForTag("latest", remotePlatform, remoteArch, logger); latestErr == nil && latestPath != "" {
				return latestPath, nil
			} else if latestErr != nil {
				return "", fmt.Errorf("remote host requires %s/%s, but this machine is %s/%s and Go is not available to build a matching backend (latest artifact fallback failed: %w)", remotePlatform, remoteArch, runtime.GOOS, runtime.GOARCH, latestErr)
			}
		}
		return "", fmt.Errorf("remote host requires %s/%s, but this machine is %s/%s and Go is not available to build a matching backend", remotePlatform, remoteArch, runtime.GOOS, runtime.GOARCH)
	}

	executablePath, err := currentExecutableForSSH()
	if err != nil {
		return "", err
	}
	repoRoot := filepath.Dir(executablePath)
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		logger.Logf("source tree unavailable; attempting latest artifact as cross-arch fallback for %s/%s", remotePlatform, remoteArch)
		if latestPath, latestErr := ensureLocalSSHBinaryArtifactForTag("latest", remotePlatform, remoteArch, logger); latestErr == nil && latestPath != "" {
			return latestPath, nil
		} else if latestErr != nil {
			return "", fmt.Errorf("cannot build matching SSH backend for %s/%s because the ledit source tree is not available next to %s (latest artifact fallback failed: %w)", remotePlatform, remoteArch, executablePath, latestErr)
		}
		return "", fmt.Errorf("cannot build matching SSH backend for %s/%s because the ledit source tree is not available next to %s", remotePlatform, remoteArch, executablePath)
	}

	cacheDir := filepath.Join(localSSHCacheRoot(), "builds")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to prepare SSH build cache: %w", err)
	}

	outputPath := filepath.Join(cacheDir, fmt.Sprintf("ledit-%s-%s", remotePlatform, remoteArch))
	buildCmd := exec.Command(goBinary, "build", "-o", outputPath, ".")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+remotePlatform,
		"GOARCH="+remoteArch,
	)
	out, err := buildCmd.CombinedOutput()
	if output := trimSSHOutput(out); output != "" {
		logger.Logf("build-backend output:\n%s", output)
	}
	if err != nil {
		logger.Logf("build-backend error: %v", err)
		return "", newSSHLaunchFailure(
			"build-backend",
			fmt.Sprintf("failed to build SSH backend for %s/%s", remotePlatform, remoteArch),
			trimSSHOutput(out),
			logger,
		)
	}
	logger.Logf("build-backend completed for %s/%s", remotePlatform, remoteArch)

	return outputPath, nil
}

func ensureLocalSSHBinaryArtifact(remotePlatform, remoteArch string, logger *sshLaunchLogger) (string, error) {
	tag := resolvePreferredReleaseTag()
	if strings.TrimSpace(tag) == "" {
		return "", errNoReleaseTagForArtifact
	}
	return ensureLocalSSHBinaryArtifactForTag(tag, remotePlatform, remoteArch, logger)
}

func ensureLocalSSHBinaryArtifactForTag(tag, remotePlatform, remoteArch string, logger *sshLaunchLogger) (string, error) {
	assetName := fmt.Sprintf("ledit-%s-%s.tar.gz", remotePlatform, remoteArch)
	cacheTag := strings.TrimPrefix(tag, "v")
	if strings.TrimSpace(cacheTag) == "" {
		cacheTag = "latest"
	}
	cacheDir := filepath.Join(localSSHCacheRoot(), "artifacts", cacheTag, remotePlatform+"-"+remoteArch)
	binaryPath := filepath.Join(cacheDir, fmt.Sprintf("ledit-%s-%s", remotePlatform, remoteArch))

	hasCachedBinary := false
	if info, err := os.Stat(binaryPath); err == nil && info.Mode().IsRegular() {
		hasCachedBinary = true
		// Keep strict cache hits for fixed tags, but always refresh "latest" so
		// we do not pin stale artifacts after new releases are published.
		if strings.TrimSpace(tag) != "latest" {
			logger.Logf("using cached release artifact %s", binaryPath)
			return binaryPath, nil
		}
		logger.Logf("refreshing cached latest release artifact %s", binaryPath)
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to prepare SSH artifact cache: %w", err)
	}

	downloadURL, err := resolveGitHubReleaseAssetURL(tag, assetName, logger)
	if err != nil {
		if hasCachedBinary {
			logger.Logf("failed to resolve %q artifact URL; using stale cached artifact %s (error: %v)", tag, binaryPath, err)
			return binaryPath, nil
		}
		return "", err
	}
	logger.Logf("resolved release artifact %s for tag %s", assetName, tag)

	archivePath := filepath.Join(cacheDir, assetName)
	if err := downloadFile(downloadURL, archivePath, logger); err != nil {
		if hasCachedBinary {
			logger.Logf("failed to download %q artifact; using stale cached artifact %s (error: %v)", tag, binaryPath, err)
			return binaryPath, nil
		}
		return "", err
	}

	if err := extractTarGzSingleFile(archivePath, binaryPath); err != nil {
		if hasCachedBinary {
			logger.Logf("failed to extract %q artifact; using stale cached artifact %s (error: %v)", tag, binaryPath, err)
			return binaryPath, nil
		}
		return "", err
	}
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return "", err
	}
	return binaryPath, nil
}

func resolvePreferredReleaseTag() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		mainVersion := strings.TrimSpace(info.Main.Version)
		if tag := normalizeReleaseTagCandidate(mainVersion); tag != "" {
			return tag
		}
		for _, setting := range info.Settings {
			if (setting.Key == "vcs.tag" || setting.Key == "gitTag") && normalizeReleaseTagCandidate(setting.Value) != "" {
				return normalizeReleaseTagCandidate(setting.Value)
			}
		}
	}
	return ""
}

func normalizeReleaseTagCandidate(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "v") {
		return ""
	}
	if strings.Contains(value, "-0.") || strings.Contains(value, "+dirty") || strings.Contains(value, "(devel)") {
		return ""
	}
	return value
}

func resolveGitHubReleaseAssetURL(tag, assetName string, logger *sshLaunchLogger) (string, error) {
	if strings.TrimSpace(assetName) == "" {
		return "", errors.New("artifact name is required")
	}
	tag = strings.TrimSpace(tag)
	if tag == "" || tag == "latest" {
		url := fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/%s", githubReleaseRepoOwner, githubReleaseRepoName, assetName)
		logger.Logf("resolved latest release download URL: %s", url)
		return url, nil
	}
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", githubReleaseRepoOwner, githubReleaseRepoName, tag, assetName)
	logger.Logf("resolved tagged release download URL: %s", url)
	return url, nil
}

func downloadFile(url, destPath string, logger *sshLaunchLogger) error {
	logger.Logf("downloading artifact %s to %s", url, destPath)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download SSH artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("failed to download SSH artifact: %s", strings.TrimSpace(string(body)))
	}
	tmpPath := destPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, destPath)
}

func extractTarGzSingleFile(archivePath, destPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		outFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(outFile, tarReader); err != nil {
			outFile.Close()
			_ = os.Remove(destPath)
			return err
		}
		if err := outFile.Close(); err != nil {
			_ = os.Remove(destPath)
			return err
		}
		return nil
	}
	return fmt.Errorf("artifact archive %s did not contain a binary", archivePath)
}

func ensureRemoteSSHBinary(ctx context.Context, hostAlias, localBinary string, remoteInfo *remoteSSHInfo, logger *sshLaunchLogger) (string, error) {
	localFingerprint, err := fingerprintFile(localBinary)
	if err != nil {
		return "", fmt.Errorf("failed to fingerprint local executable: %w", err)
	}

	homeCmd := newSSHCommandContext(ctx, hostAlias, `printf "%s" "$HOME"`)
	homeOut, err := runSSHLoggedCommand(logger, "resolve-remote-home", fmt.Sprintf("ssh %s print $HOME", hostAlias), homeCmd)
	if err != nil {
		return "", err
	}
	remoteHome := strings.TrimSpace(string(homeOut))
	if remoteHome == "" {
		return "", newSSHLaunchFailure("resolve-remote-home", "failed to resolve remote home directory", "remote $HOME was empty", logger)
	}

	remoteDirSSH := fmt.Sprintf("%s/.cache/ledit-webui/backend/%s/%s-%s", remoteHome, localFingerprint, remoteInfo.Platform, remoteInfo.Arch)
	remoteBinarySSH := remoteDirSSH + "/ledit"
	remoteUploadSCP := fmt.Sprintf(".ledit-ssh-upload-%s.tmp", localFingerprint)

	// Fast path: if the fingerprinted backend already exists and executes,
	// skip upload/install and reuse it directly.
	checkExisting := newSSHCommandContext(ctx, hostAlias, fmt.Sprintf("[ -x %s ] && %s version", shellEscapeSSH(remoteBinarySSH), shellEscapeSSH(remoteBinarySSH)))
	if out, err := checkExisting.CombinedOutput(); err == nil {
		if output := trimSSHOutput(out); output != "" {
			logger.Logf("reuse-backend output:\n%s", output)
		}
		logger.Logf("reuse-backend found executable at %s", remoteBinarySSH)
		return remoteBinarySSH, nil
	} else {
		if output := trimSSHOutput(out); output != "" {
			logger.Logf("reuse-backend miss for %s:\n%s", remoteBinarySSH, output)
		}
		logger.Logf("reuse-backend check failed, proceeding with install")
	}

	mkdir := newSSHCommandContext(ctx, hostAlias, fmt.Sprintf("mkdir -p %s", shellEscapeSSH(remoteDirSSH)))
	if _, err := runSSHLoggedCommand(logger, "prepare-remote-dir", fmt.Sprintf("ssh %s mkdir -p %s", hostAlias, remoteDirSSH), mkdir); err != nil {
		return "", err
	}

	copyCmd := exec.CommandContext(ctx, "scp",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "ConnectionAttempts=1",
		"-q",
		localBinary,
		fmt.Sprintf("%s:%s", hostAlias, remoteUploadSCP),
	)
	if _, err := runSSHLoggedCommand(logger, "upload-backend", fmt.Sprintf("scp %s %s:%s", localBinary, hostAlias, remoteUploadSCP), copyCmd); err != nil {
		return "", err
	}

	install := newSSHCommandContext(ctx, hostAlias, fmt.Sprintf(
		"mv %s %s && chmod +x %s",
		`"$HOME/`+remoteUploadSCP+`"`,
		shellEscapeSSH(remoteBinarySSH),
		shellEscapeSSH(remoteBinarySSH),
	))
	if _, err := runSSHLoggedCommand(logger, "install-backend", fmt.Sprintf("ssh %s install backend into %s", hostAlias, remoteBinarySSH), install); err != nil {
		return "", err
	}

	// Verify the uploaded backend can execute on the remote host.
	verify := newSSHCommandContext(ctx, hostAlias, fmt.Sprintf("%s version", shellEscapeSSH(remoteBinarySSH)))
	if _, err := runSSHLoggedCommand(logger, "verify-backend", fmt.Sprintf("ssh %s verify backend executable %s", hostAlias, remoteBinarySSH), verify); err != nil {
		return "", newSSHLaunchFailure(
			"verify-backend",
			"uploaded SSH backend is not executable on remote host",
			err.Error(),
			logger,
		)
	}

	return remoteBinarySSH, nil
}

func fingerprintFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil))[:16], nil
}
