package scripts_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filename))
}

func readRepoFile(t *testing.T, path ...string) string {
	t.Helper()
	parts := append([]string{repoRoot(t)}, path...)
	contents, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(path...), err)
	}
	return string(contents)
}

func TestBuildWasmUsesCheckedInBrowserRuntime(t *testing.T) {
	script := readRepoFile(t, "scripts", "build-wasm.sh")

	forbidden := []string{
		"$GOROOT/lib/wasm/wasm_exec.js",
		"$(go env GOROOT)/lib/wasm/wasm_exec.js",
		"lib/wasm/wasm_exec.js",
	}
	for _, fragment := range forbidden {
		if strings.Contains(script, fragment) {
			t.Fatalf("build-wasm.sh must not source wasm_exec.js from the Go toolchain; found %q", fragment)
		}
	}

	required := []string{
		"webui/public/wasm/wasm_exec.js",
		"checked-in browser-compatible wasm_exec.js",
		`[ "$wasm_exec_src" -ef "$wasm_exec_dst" ]`,
		`cp "$wasm_exec_src" "$wasm_exec_dst"`,
	}
	for _, fragment := range required {
		if !strings.Contains(script, fragment) {
			t.Fatalf("build-wasm.sh should preserve the checked-in runtime; missing %q", fragment)
		}
	}
}

func TestCheckedInWasmRuntimeKeepsInMemoryFilesystemShim(t *testing.T) {
	runtimeFiles := []string{
		"webui/public/wasm/wasm_exec.js",
		"pkg/webui/static/wasm/wasm_exec.js",
	}

	required := []string{
		"real in-memory filesystem",
		"const files = new Map();",
		"const dirs = new Set();",
		"open(pathStr, flags, mode, callback)",
		"read(fd, buffer, offset, length, position, callback)",
		"write(fd, buf, offset, length, position, callback)",
		"readdir(pathStr, callback)",
		"mkdir(pathStr, perm, callback)",
		"rename(from, to, callback)",
	}
	forbidden := []string{
		"const enosys = () => {",
		"callback(enosys())",
	}

	for _, path := range runtimeFiles {
		t.Run(path, func(t *testing.T) {
			contents := readRepoFile(t, filepath.FromSlash(path))
			for _, fragment := range required {
				if !strings.Contains(contents, fragment) {
					t.Fatalf("%s appears to have lost the browser in-memory filesystem shim; missing %q", path, fragment)
				}
			}
			for _, fragment := range forbidden {
				if strings.Contains(contents, fragment) {
					t.Fatalf("%s appears to be the upstream no-op fs shim; found %q", path, fragment)
				}
			}
		})
	}
}
