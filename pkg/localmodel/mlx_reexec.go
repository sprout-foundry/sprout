//go:build darwin && arm64 && cgo

package localmodel

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/sprout-foundry/sinter/mlx"
)

// reexecGuardEnv marks a process already restarted onto the bundled runtime,
// so a runtime that fails to load cannot cause a restart loop.
const reexecGuardEnv = "SPROUT_MLX_REEXEC"

// RuntimeSupported reports whether this build can run local models once the
// MLX runtime is present.
func RuntimeSupported() bool { return true }

// MaybeReexecWithBundledRuntime restarts sprout with MLX_C_LIB pointing at
// its own runtime copy when MLX did not load at startup. MLX resolves its
// library once per process, so a runtime installed after startup can only be
// picked up by a fresh process. It returns only when no restart is needed or
// the restart failed.
func MaybeReexecWithBundledRuntime() {
	if !shouldReexec(mlx.Available(), RuntimeInstalled(), os.Getenv(reexecGuardEnv), os.Getenv("MLX_C_LIB")) {
		return
	}
	_ = restartWithRuntime()
}

// RestartWithRuntime replaces the current process with a fresh sprout that
// loads the bundled runtime, keeping the same arguments. It returns only on
// failure.
func RestartWithRuntime() error {
	if !RuntimeInstalled() {
		return fmt.Errorf("MLX runtime is not installed in %s", RuntimeDir())
	}
	return restartWithRuntime()
}

func shouldReexec(mlxLoaded, runtimeInstalled bool, guard, mlxCLib string) bool {
	return !mlxLoaded && runtimeInstalled && guard == "" && mlxCLib != RuntimeLibPath()
}

func restartWithRuntime() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate sprout executable: %w", err)
	}
	env := []string{"MLX_C_LIB=" + RuntimeLibPath(), reexecGuardEnv + "=1"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "MLX_C_LIB=") && !strings.HasPrefix(kv, reexecGuardEnv+"=") {
			env = append(env, kv)
		}
	}
	if err := syscall.Exec(exe, os.Args, env); err != nil { //nolint:gosec // G204: restarts this same executable
		return fmt.Errorf("restart sprout: %w", err)
	}
	return nil
}
