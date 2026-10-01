//go:build !js

package webui

// onboarding_detect.go — the webui onboarding detection helpers: the
// recommended-model probe / resolve (probeRecommendedModel,
// resolveRecommendedModel), the environment / WSL / git-bash detection
// (detectOnboardingEnvironment, detectWslDistros, hasGitBashShell). Split
// out of onboarding_api.go.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/modelregistry"
)

// probeRecommendedModel fetches the per-provider file from the published model
// registry and returns the strongest probe-backed model ID for onboarding, or
// "" if none. Prefers models whose RecommendedRoles contain "primary" (complex
// stage passed — the strongest signal); falls back to "subagent" (gates passed).
// A short context timeout keeps onboarding responsive if the registry is slow;
// any error or no-data yields "" so the caller falls back to existing logic.
func probeRecommendedModel(providerID string) string {
	if !modelregistry.IsEnabled() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := modelregistry.FetchModels(ctx, providerID)
	if err != nil || len(raw) == 0 {
		return ""
	}
	var primaryPick, subagentPick string
	for _, m := range raw {
		if modelcontract.RoleHas(m.RecommendedRoles, modelcontract.RolePrimary) {
			if primaryPick == "" {
				primaryPick = m.ID
			}
		} else if modelcontract.RoleHas(m.RecommendedRoles, modelcontract.RoleSubagent) {
			if subagentPick == "" {
				subagentPick = m.ID
			}
		}
	}
	if primaryPick != "" {
		return primaryPick
	}
	return subagentPick
}

// resolveRecommendedModel picks the first model whose ID starts with any of
// the curated prefixes (case-insensitive); falls back to the first available
// model. Used only as a last-resort default when neither the catalog nor the
// capability probe yielded a recommendation.

func resolveRecommendedModel(models []string, prefixes []string) string {
	for _, prefix := range prefixes {
		for _, model := range models {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), strings.ToLower(prefix)) {
				return model
			}
		}
	}
	if len(models) > 0 {
		return strings.TrimSpace(models[0])
	}
	return ""
}

func detectOnboardingEnvironment() onboardingEnvironment {
	hostPlatform := strings.TrimSpace(configuration.GetEnvSimple("HOST_PLATFORM"))
	if hostPlatform == "" {
		hostPlatform = runtime.GOOS
	}

	backendMode := strings.TrimSpace(configuration.GetEnvSimple("DESKTOP_BACKEND_MODE"))
	if backendMode == "" {
		backendMode = "native"
	}

	hasWSL := backendMode == "wsl"
	if !hasWSL && hostPlatform == "windows" {
		_, err := exec.LookPath("wsl.exe")
		hasWSL = err == nil
	}

	hasGitBash := hasGitBashShell()
	recommendedTerminal := "system"
	if hostPlatform == "windows" {
		if backendMode == "wsl" || hasWSL {
			recommendedTerminal = "wsl"
		} else if hasGitBash {
			recommendedTerminal = "git-bash"
		}
	}

	// Detect active WSL distro (set by the WSL runtime environment).
	activeDistro := strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME"))

	// Enumerate available WSL distros when on Windows native or when wsl.exe is reachable.
	wslDistros := detectWslDistros(hasWSL, backendMode, hostPlatform)

	return onboardingEnvironment{
		RuntimePlatform:     runtime.GOOS,
		HostPlatform:        hostPlatform,
		BackendMode:         backendMode,
		HasWSL:              hasWSL,
		HasGitBash:          hasGitBash,
		RecommendedTerminal: recommendedTerminal,
		ActiveDistro:        activeDistro,
		WslDistros:          wslDistros,
	}
}

func detectWslDistros(hasWSL bool, backendMode, hostPlatform string) []string {
	// Only attempt on Windows native (not from inside WSL) or when wsl.exe is known to be present.
	if !hasWSL {
		return nil
	}
	if hostPlatform != "windows" && backendMode != "wsl" {
		return nil
	}
	cmd := exec.Command("wsl.exe", "-l", "-q")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var distros []string
	for _, line := range strings.Split(string(out), "\n") {
		// wsl.exe -l -q uses UTF-16LE on Windows; when running inside WSL it emits UTF-8.
		// Strip BOM and null bytes that appear in the UTF-16 output.
		clean := strings.Map(func(r rune) rune {
			if r == 0 || r == '\r' || r == '\ufeff' {
				return -1
			}
			return r
		}, line)
		clean = strings.TrimSpace(clean)
		// Strip the leading '*' that wsl.exe uses to mark the default distro.
		clean = strings.TrimLeft(clean, "* ")
		clean = strings.TrimSpace(clean)
		if clean != "" {
			distros = append(distros, clean)
		}
	}
	return distros
}

func hasGitBashShell() bool {
	if _, err := exec.LookPath("bash"); err == nil {
		return true
	}

	programFiles := []string{
		strings.TrimSpace(os.Getenv("ProgramFiles")),
		strings.TrimSpace(os.Getenv("ProgramW6432")),
		strings.TrimSpace(os.Getenv("ProgramFiles(x86)")),
	}

	for _, root := range programFiles {
		if root == "" {
			continue
		}
		candidates := []string{
			filepath.Join(root, "Git", "bin", "bash.exe"),
			filepath.Join(root, "Git", "usr", "bin", "bash.exe"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return true
			}
		}
	}

	return false
}
