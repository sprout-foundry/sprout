package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// captureStderr temporarily replaces os.Stderr with a pipe and returns the captured output.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	data, err := os.ReadFile(r.Name())
	// os.Pipe() on Linux returns a pipe fd whose .Name() is "/dev/fd/N";
	// os.ReadFile on that works. Fallback: read from r directly.
	if len(data) == 0 {
		// Read from the reader end directly.
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		data = buf[:n]
	}
	if err != nil && len(data) == 0 {
		t.Logf("captureStderr read error: %v", err)
	}
	return string(data)
}

// captureStdout temporarily replaces os.Stdout with a pipe and returns the captured output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	data, err := os.ReadFile(r.Name())
	if len(data) == 0 {
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		data = buf[:n]
	}
	if err != nil && len(data) == 0 {
		t.Logf("captureStdout read error: %v", err)
	}
	return string(data)
}
func setupTest(t *testing.T) (configsDir, registryDir string, cleanup func()) {
	t.Helper()
	configsDir = t.TempDir()
	registryDir = t.TempDir()
	// Create the models subdirectory inside registryDir.
	modelsDir := filepath.Join(registryDir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	cleanup = func() {
		// t.TempDir handles cleanup automatically.
	}
	return configsDir, registryDir, cleanup
}

// writeConfig writes a provider config JSON file into the configs directory.
func writeConfig(t *testing.T, dir, id string, cfg map[string]interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	data = append(data, '\n')
	path := filepath.Join(dir, id+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config %s: %v", id, err)
	}
}

// writeRegistry writes a canonical provider registry file into the registry directory.
func writeRegistry(t *testing.T, dir, id string, models []modelcontract.CanonicalModel) {
	t.Helper()
	pf := modelcontract.ProviderFile{
		SchemaVersion: modelcontract.SchemaVersion,
		Provider:      id,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Models:        models,
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	data = append(data, '\n')
	path := filepath.Join(dir, "models", id+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write registry %s: %v", id, err)
	}
}

// readConfig reads and returns the config map for the given provider ID.
func readConfig(t *testing.T, dir, id string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatalf("read config %s: %v", id, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config %s: %v", id, err)
	}
	return cfg
}

// getConfigMtime returns the modification time of the config file.
func getConfigMtime(t *testing.T, dir, id string) time.Time {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatalf("stat config %s: %v", id, err)
	}
	return info.ModTime()
}

// makeBaseConfig returns a minimal provider config with the given available_models.
func makeBaseConfig(id, endpoint string, models []string) map[string]interface{} {
	cfg := map[string]interface{}{
		"name":     id,
		"endpoint": endpoint,
		"auth":     map[string]interface{}{"type": "bearer", "env_var": id + "_API_KEY"},
		"models": map[string]interface{}{
			"available_models":    models,
			"default_context_limit": 128000,
		},
		"retry": map[string]interface{}{
			"max_attempts":    3,
			"base_delay_ms":   1000,
			"backoff_multiplier": 2,
		},
		"cost": map[string]interface{}{
			"input_token_cost":  0,
			"output_token_cost": 0,
			"currency":          "USD",
		},
	}
	return cfg
}

// TestSync_NoOpWhenListUnchanged verifies that when the embedded config's
// available_models matches the registry, no file write occurs.
func TestSync_NoOpWhenListUnchanged(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	models := []string{"a", "b", "c"}
	cfg := makeBaseConfig("test-provider", "https://api.example.com/v1/chat", models)
	writeConfig(t, configsDir, "test-provider", cfg)
	writeRegistry(t, registryDir, "test-provider", []modelcontract.CanonicalModel{
		{ID: "a", Provider: "test-provider"},
		{ID: "b", Provider: "test-provider"},
		{ID: "c", Provider: "test-provider"},
	})

	// Record mtime before.
	mtimeBefore := getConfigMtime(t, configsDir, "test-provider")

	// Small delay to ensure mtime would differ if written.
	time.Sleep(10 * time.Millisecond)

	if err := Run(configsDir, registryDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mtimeAfter := getConfigMtime(t, configsDir, "test-provider")
	if !mtimeBefore.Equal(mtimeAfter) {
		t.Error("config file was modified even though lists are identical")
	}
}

// TestSync_UpdatesListWhenChanged verifies that when the registry has more
// models than the embedded config, the config is updated and other fields
// are preserved.
func TestSync_UpdatesListWhenChanged(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	cfg := makeBaseConfig("test-provider", "https://api.example.com/v1/chat", []string{"a", "b", "c"})
	writeConfig(t, configsDir, "test-provider", cfg)
	writeRegistry(t, registryDir, "test-provider", []modelcontract.CanonicalModel{
		{ID: "a", Provider: "test-provider"},
		{ID: "b", Provider: "test-provider"},
		{ID: "c", Provider: "test-provider"},
		{ID: "d", Provider: "test-provider"},
		{ID: "e", Provider: "test-provider"},
	})

	if err := Run(configsDir, registryDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	updated := readConfig(t, configsDir, "test-provider")
	modelsMap, ok := updated["models"].(map[string]interface{})
	if !ok {
		t.Fatal("models field is not a map")
	}
	avail, ok := modelsMap["available_models"].([]interface{})
	if !ok {
		t.Fatal("available_models is not a slice")
	}
	if len(avail) != 5 {
		t.Errorf("expected 5 models, got %d", len(avail))
	}
	// Verify the model IDs.
	ids := make([]string, len(avail))
	for i, v := range avail {
		ids[i] = v.(string)
	}
	expected := []string{"a", "b", "c", "d", "e"}
	for i := range expected {
		if ids[i] != expected[i] {
			t.Errorf("model[%d] = %q, want %q", i, ids[i], expected[i])
		}
	}
}

// TestSync_SkipsEmptyListProvider verifies that providers with an empty
// available_models list (live-discovery pattern) are not touched.
func TestSync_SkipsEmptyListProvider(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	cfg := makeBaseConfig("openai", "https://api.openai.com/v1/chat", []string{})
	writeConfig(t, configsDir, "openai", cfg)
	writeRegistry(t, registryDir, "openai", []modelcontract.CanonicalModel{
		{ID: "gpt-4", Provider: "openai"},
		{ID: "gpt-5", Provider: "openai"},
	})

	mtimeBefore := getConfigMtime(t, configsDir, "openai")
	time.Sleep(10 * time.Millisecond)

	if err := Run(configsDir, registryDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mtimeAfter := getConfigMtime(t, configsDir, "openai")
	if !mtimeBefore.Equal(mtimeAfter) {
		t.Error("config with empty available_models was modified")
	}
}

// TestSync_SkipsLocalOnlyProvider verifies that providers with a non-HTTPS
// endpoint (e.g. lmstudio) are skipped.
func TestSync_SkipsLocalOnlyProvider(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	cfg := makeBaseConfig("lmstudio", "http://127.0.0.1:1234/v1/chat/completions", []string{"model-a", "model-b"})
	writeConfig(t, configsDir, "lmstudio", cfg)
	writeRegistry(t, registryDir, "lmstudio", []modelcontract.CanonicalModel{
		{ID: "model-a", Provider: "lmstudio"},
		{ID: "model-c", Provider: "lmstudio"},
	})

	mtimeBefore := getConfigMtime(t, configsDir, "lmstudio")
	time.Sleep(10 * time.Millisecond)

	if err := Run(configsDir, registryDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mtimeAfter := getConfigMtime(t, configsDir, "lmstudio")
	if !mtimeBefore.Equal(mtimeAfter) {
		t.Error("local-only provider config was modified")
	}
}

// TestSync_SkipsMissingRegistryFile verifies that when the registry file
// doesn't exist, the embedded config is left unchanged and a warning is printed.
func TestSync_SkipsMissingRegistryFile(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	cfg := makeBaseConfig("missing-reg", "https://api.example.com/v1/chat", []string{"a", "b"})
	writeConfig(t, configsDir, "missing-reg", cfg)
	// Don't write a registry file for "missing-reg".

	mtimeBefore := getConfigMtime(t, configsDir, "missing-reg")
	time.Sleep(10 * time.Millisecond)

	// Capture stderr to check for warning.
	stderrStr := captureStderr(t, func() {
		if err := Run(configsDir, registryDir, false); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})

	mtimeAfter := getConfigMtime(t, configsDir, "missing-reg")
	if !mtimeBefore.Equal(mtimeAfter) {
		t.Error("config was modified despite missing registry file")
	}

	if !strings.Contains(stderrStr, "warn:") || !strings.Contains(stderrStr, "no registry file") {
		t.Errorf("expected warning in stderr, got: %s", stderrStr)
	}
}

// TestSync_PreservesAllOtherFields verifies that custom headers, retry, and
// cost fields are preserved byte-for-byte after sync.
func TestSync_PreservesAllOtherFields(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	cfg := makeBaseConfig("preserve-test", "https://api.example.com/v1/chat", []string{"a", "b"})
	// Add custom fields.
	cfg["headers"] = map[string]interface{}{
		"X-Custom-Header": "custom-value",
	}
	cfg["retry"] = map[string]interface{}{
		"max_attempts":    5,
		"base_delay_ms":   2000,
		"backoff_multiplier": 3,
		"max_delay_ms":    60000,
	}
	cfg["cost"] = map[string]interface{}{
		"input_token_cost":  0.5,
		"output_token_cost": 1.5,
		"currency":          "USD",
	}
	cfg["display_name"] = "Preserve Test"
	writeConfig(t, configsDir, "preserve-test", cfg)

	writeRegistry(t, registryDir, "preserve-test", []modelcontract.CanonicalModel{
		{ID: "a", Provider: "preserve-test"},
		{ID: "b", Provider: "preserve-test"},
		{ID: "c", Provider: "preserve-test"},
	})

	if err := Run(configsDir, registryDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	updated := readConfig(t, configsDir, "preserve-test")

	// Check custom headers.
	headers, ok := updated["headers"].(map[string]interface{})
	if !ok {
		t.Fatal("headers field is missing or not a map")
	}
	if v, ok := headers["X-Custom-Header"].(string); !ok || v != "custom-value" {
		t.Errorf("custom header not preserved: %v", headers)
	}

	// Check retry settings.
	retry, ok := updated["retry"].(map[string]interface{})
	if !ok {
		t.Fatal("retry field is missing or not a map")
	}
	if v, ok := retry["max_attempts"].(float64); !ok || v != 5 {
		t.Errorf("retry.max_attempts not preserved: %v", retry)
	}

	// Check cost settings.
	cost, ok := updated["cost"].(map[string]interface{})
	if !ok {
		t.Fatal("cost field is missing or not a map")
	}
	if v, ok := cost["input_token_cost"].(float64); !ok || v != 0.5 {
		t.Errorf("cost.input_token_cost not preserved: %v", cost)
	}

	// Check display_name.
	if dn, ok := updated["display_name"].(string); !ok || dn != "Preserve Test" {
		t.Errorf("display_name not preserved: %v", updated["display_name"])
	}
}

// TestSync_DryRunDoesNotWrite verifies that --dry-run reports changes without
// modifying any files.
func TestSync_DryRunDoesNotWrite(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	cfg := makeBaseConfig("dryrun-test", "https://api.example.com/v1/chat", []string{"a"})
	writeConfig(t, configsDir, "dryrun-test", cfg)
	writeRegistry(t, registryDir, "dryrun-test", []modelcontract.CanonicalModel{
		{ID: "a", Provider: "dryrun-test"},
		{ID: "b", Provider: "dryrun-test"},
	})

	mtimeBefore := getConfigMtime(t, configsDir, "dryrun-test")
	time.Sleep(10 * time.Millisecond)

	// Capture stdout to verify dry-run message.
	stdoutStr := captureStdout(t, func() {
		if err := Run(configsDir, registryDir, true); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})

	mtimeAfter := getConfigMtime(t, configsDir, "dryrun-test")
	if !mtimeBefore.Equal(mtimeAfter) {
		t.Error("config was modified in dry-run mode")
	}

	if !strings.Contains(stdoutStr, "[dry-run]") {
		t.Errorf("expected dry-run message in stdout, got: %s", stdoutStr)
	}
}

// TestSync_HandlesAtomicWrite verifies that no .tmp file is left behind
// after a successful sync.
func TestSync_HandlesAtomicWrite(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	cfg := makeBaseConfig("atomic-test", "https://api.example.com/v1/chat", []string{"a"})
	writeConfig(t, configsDir, "atomic-test", cfg)
	writeRegistry(t, registryDir, "atomic-test", []modelcontract.CanonicalModel{
		{ID: "a", Provider: "atomic-test"},
		{ID: "b", Provider: "atomic-test"},
	})

	if err := Run(configsDir, registryDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Check that no .tmp file exists.
	tmpPath := filepath.Join(configsDir, "atomic-test.json.tmp")
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file was left behind after successful sync")
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected error checking for temp file: %v", err)
	}
}

// TestSync_PreservesOriginalCase verifies that model IDs from the canonical
// registry are preserved with their original case.
func TestSync_PreservesOriginalCase(t *testing.T) {
	configsDir, registryDir, _ := setupTest(t)
	defer func() {}()

	cfg := makeBaseConfig("case-test", "https://api.example.com/v1/chat", []string{"Model-A"})
	writeConfig(t, configsDir, "case-test", cfg)
	writeRegistry(t, registryDir, "case-test", []modelcontract.CanonicalModel{
		{ID: "Model-A", Provider: "case-test"},
		{ID: "Model-B", Provider: "case-test"},
		{ID: "model-c", Provider: "case-test"},
	})

	if err := Run(configsDir, registryDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	updated := readConfig(t, configsDir, "case-test")
	modelsMap := updated["models"].(map[string]interface{})
	avail := modelsMap["available_models"].([]interface{})

	expected := []string{"Model-A", "Model-B", "model-c"}
	for i, exp := range expected {
		if avail[i].(string) != exp {
			t.Errorf("model[%d] = %q, want %q (case mismatch)", i, avail[i], exp)
		}
	}
}
