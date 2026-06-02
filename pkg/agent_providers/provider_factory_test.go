package providers

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Minimal valid ProviderConfig JSON blobs used throughout these tests.
//
// Validate() requires: Name != "", Endpoint != "", Auth.Type != "",
// and either Models.DefaultContextLimit > 0 or Models.ContextLimit > 0.
// ---------------------------------------------------------------------------

var (
	// baseConfig is a minimal valid config for a fictitious provider "testprov"
	baseConfig = []byte(`{
		"name": "testprov",
		"endpoint": "https://test.example.com/v1/chat/completions",
		"auth": {"type": "bearer"},
		"defaults": {"model": "test-model-1"},
		"models": {"default_context_limit": 8192}
	}`)

	// baseConfigV2 is an updated version with a different default model
	baseConfigV2 = []byte(`{
		"name": "testprov",
		"endpoint": "https://test.example.com/v1/chat/completions",
		"auth": {"type": "bearer"},
		"defaults": {"model": "test-model-2-updated"},
		"models": {"default_context_limit": 16384}
	}`)

	// altConfig is a completely different provider for multi-provider tests
	altConfig = []byte(`{
		"name": "altprov",
		"endpoint": "https://alt.example.com/v1/chat/completions",
		"auth": {"type": "api_key"},
		"defaults": {"model": "alt-model"},
		"models": {"default_context_limit": 4096}
	}`)
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestFactory creates a ProviderFactory with the base config pre-loaded.
func newTestFactory(t *testing.T) *ProviderFactory {
	t.Helper()
	f := NewProviderFactory()
	if err := f.LoadConfigFromBytes(baseConfig); err != nil {
		t.Fatalf("LoadConfigFromBytes(baseConfig): %v", err)
	}
	return f
}

// newEmptyFactory creates a fresh ProviderFactory with no configs.
func newEmptyFactory(t *testing.T) *ProviderFactory {
	t.Helper()
	return NewProviderFactory()
}

// ---------------------------------------------------------------------------
// Correctness tests — sequential
// ---------------------------------------------------------------------------

func TestLoadConfigFromBytes_InvalidJSON(t *testing.T) {
	t.Parallel()
	f := NewProviderFactory()
	err := f.LoadConfigFromBytes([]byte(`{broken json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadConfigFromBytes_MissingRequiredFields(t *testing.T) {
	t.Parallel()
	f := NewProviderFactory()

	tests := []struct {
		name  string
		json  []byte
	}{
		{
			name: "empty name",
			json: []byte(`{"endpoint": "https://x.com", "auth": {"type": "bearer"}, "models": {"default_context_limit": 1024}}`),
		},
		{
			name: "empty endpoint",
			json: []byte(`{"name": "prov", "auth": {"type": "bearer"}, "models": {"default_context_limit": 1024}}`),
		},
		{
			name: "empty auth type",
			json: []byte(`{"name": "prov", "endpoint": "https://x.com", "auth": {}, "models": {"default_context_limit": 1024}}`),
		},
		{
			name: "missing context limit",
			json: []byte(`{"name": "prov", "endpoint": "https://x.com", "auth": {"type": "bearer"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.LoadConfigFromBytes(tt.json)
			if err == nil {
				t.Fatalf("expected error for invalid config, got nil")
			}
		})
	}
}

func TestGetAvailableProviders_AfterLoad(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	providers := f.GetAvailableProviders()
	if len(providers) != 1 || providers[0] != "testprov" {
		t.Fatalf("expected [\"testprov\"], got %v", providers)
	}
}

func TestGetProviderConfig_ExistingProvider(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	cfg, err := f.GetProviderConfig("testprov")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "testprov" {
		t.Fatalf("expected name testprov, got %s", cfg.Name)
	}
}

func TestGetProviderConfig_MissingProvider(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	_, err := f.GetProviderConfig("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestCreateProvider_ExistingProvider(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	provider, err := f.CreateProvider("testprov")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestCreateProvider_MissingProvider(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	_, err := f.CreateProvider("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestCreateProviderWithModel_Success(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	provider, err := f.CreateProviderWithModel("testprov", "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestCreateProviderWithModel_EmptyModel(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	// Empty model string should succeed and fall back to the config's default model.
	provider, err := f.CreateProviderWithModel("testprov", "")
	if err != nil {
		t.Fatalf("unexpected error with empty model: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider when model is empty")
	}
}

func TestCreateProviderWithModel_MissingProvider(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	_, err := f.CreateProviderWithModel("nonexistent", "any-model")
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestListProvidersWithModels_WithConfigs(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	models := f.ListProvidersWithModels()
	if len(models) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(models))
	}
	if _, ok := models["testprov"]; !ok {
		t.Fatalf("expected testprov in models map")
	}
}

func TestValidateProvider_Valid(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	// When AvailableModels is empty, any model is accepted
	err := f.ValidateProvider("testprov", "any-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateProvider_MissingProvider(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	err := f.ValidateProvider("nonexistent", "any-model")
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

func TestValidateProvider_ModelNotInList(t *testing.T) {
	t.Parallel()
	configWithModels := []byte(`{
		"name": "strictprov",
		"endpoint": "https://strict.example.com/v1/chat/completions",
		"auth": {"type": "bearer"},
		"defaults": {"model": "model-a"},
		"models": {
			"default_context_limit": 4096,
			"available_models": ["model-a", "model-b"]
		}
	}`)

	f := newEmptyFactory(t)
	if err := f.LoadConfigFromBytes(configWithModels); err != nil {
		t.Fatalf("LoadConfigFromBytes: %v", err)
	}

	err := f.ValidateProvider("strictprov", "model-c")
	if err == nil {
		t.Fatal("expected error for model not in available list")
	}
}

func TestGetDefaultProvider_WithLoadedConfig(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	name := f.GetDefaultProvider()
	if name != "testprov" {
		t.Fatalf("expected testprov, got %s", name)
	}
}

func TestGetDefaultProvider_EmptyFactory(t *testing.T) {
	t.Parallel()
	f := newEmptyFactory(t)

	name := f.GetDefaultProvider()
	if name != "" {
		t.Fatalf("expected empty string, got %s", name)
	}
}

func TestGetRegistry_ReturnsRegistry(t *testing.T) {
	t.Parallel()
	f := newTestFactory(t)

	reg := f.GetRegistry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
	if _, ok := reg.ProviderConfigs["testprov"]; !ok {
		t.Fatal("expected testprov in registry")
	}
}

// ---------------------------------------------------------------------------
// ReloadConfig tests
// ---------------------------------------------------------------------------

func TestReloadConfig_LoadAndVerifyUpdate(t *testing.T) {
	t.Parallel()
	// Write the base config to a temp file first
	tmpFile := t.TempDir() + "/testprov.json"
	if err := fWriteFile(tmpFile, string(baseConfig)); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	f := newEmptyFactory(t)
	if err := f.LoadConfigFromFile(tmpFile); err != nil {
		t.Fatalf("LoadConfigFromFile: %v", err)
	}

	// Verify initial config
	cfg, _ := f.GetProviderConfig("testprov")
	if cfg.Defaults.Model != "test-model-1" {
		t.Fatalf("expected initial model test-model-1, got %s", cfg.Defaults.Model)
	}

	// Write updated config to same file
	if err := fWriteFile(tmpFile, string(baseConfigV2)); err != nil {
		t.Fatalf("failed to write updated config: %v", err)
	}

	// Reload
	if err := f.ReloadConfig(tmpFile); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}

	// Verify updated config
	cfg, _ = f.GetProviderConfig("testprov")
	if cfg.Defaults.Model != "test-model-2-updated" {
		t.Fatalf("expected updated model test-model-2-updated, got %s", cfg.Defaults.Model)
	}
	if cfg.Models.DefaultContextLimit != 16384 {
		t.Fatalf("expected updated context limit 16384, got %d", cfg.Models.DefaultContextLimit)
	}
}

func TestReloadConfig_RemovesOldAndAddsNew(t *testing.T) {
	t.Parallel()
	tmpFile := t.TempDir() + "/testprov.json"
	if err := fWriteFile(tmpFile, string(baseConfig)); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	f := newEmptyFactory(t)
	if err := f.LoadConfigFromFile(tmpFile); err != nil {
		t.Fatalf("LoadConfigFromFile: %v", err)
	}

	// Load another provider separately (not via ReloadConfig)
	if err := f.LoadConfigFromBytes(altConfig); err != nil {
		t.Fatalf("LoadConfigFromBytes(altConfig): %v", err)
	}

	providers := f.GetAvailableProviders()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers before reload, got %d", len(providers))
	}

	// Reload testprov — should still have 2 providers, just the updated one
	if err := f.ReloadConfig(tmpFile); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}

	providers = f.GetAvailableProviders()
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers after reload, got %d", len(providers))
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests — parallel reads
// ---------------------------------------------------------------------------

func TestConcurrentReads_NoDeadlock(t *testing.T) {
	t.Parallel()

	f := newTestFactory(t)
	// Also load an alt config for variety
	if err := f.LoadConfigFromBytes(altConfig); err != nil {
		t.Fatalf("LoadConfigFromBytes(altConfig): %v", err)
	}

	const goroutines = 20
	const iterations = 50

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("goroutine %d iteration %d: %w", id, j, ctx.Err())
					return
				default:
				}

				switch j % 7 {
				case 0:
					_ = f.GetAvailableProviders()
				case 1:
					_, _ = f.GetProviderConfig("testprov")
				case 2:
					_, _ = f.CreateProvider("testprov")
				case 3:
					_ = f.ListProvidersWithModels()
				case 4:
					_ = f.ValidateProvider("testprov", "any-model")
				case 5:
					_ = f.GetDefaultProvider()
				case 6:
					_ = f.GetRegistry()
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed — success
	case <-ctx.Done():
		t.Fatal("concurrent reads test timed out (possible deadlock)")
	}

	close(errCh)
	for err := range errCh {
		t.Fatalf("goroutine error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests — interleaved reads and writes
// ---------------------------------------------------------------------------

func TestConcurrentReadWrite_NoRace(t *testing.T) {
	t.Parallel()

	f := newEmptyFactory(t)

	const readGoroutines = 10
	const writeGoroutines = 10
	const iterations = 30

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, readGoroutines+writeGoroutines)

	// Writer goroutines — each writes a unique config
	wg.Add(writeGoroutines)
	for i := 0; i < writeGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("writer %d iter %d: %w", id, j, ctx.Err())
					return
				default:
				}

				cfgJSON := []byte(fmt.Sprintf(`{
					"name": "w%dd_prov",
					"endpoint": "https://w%d.example.com/v1/chat/completions",
					"auth": {"type": "bearer"},
					"defaults": {"model": "model-%d"},
					"models": {"default_context_limit": %d}
				}`, id, id, j, 4096+j*1024))

				if err := f.LoadConfigFromBytes(cfgJSON); err != nil {
					errCh <- fmt.Errorf("writer %d iter %d LoadConfigFromBytes: %w", id, j, err)
					return
				}
			}
		}(i)
	}

	// Reader goroutines — read whatever configs exist
	wg.Add(readGoroutines)
	for i := 0; i < readGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("reader %d iter %d: %w", id, j, ctx.Err())
					return
				default:
				}

				switch j % 4 {
				case 0:
					_ = f.GetAvailableProviders()
				case 1:
					// Try to get any provider that exists; ignore missing errors
					names := f.GetAvailableProviders()
					if len(names) > 0 {
						_, _ = f.GetProviderConfig(names[0])
					}
				case 2:
					_ = f.ListProvidersWithModels()
				case 3:
					_ = f.GetDefaultProvider()
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed — success
	case <-ctx.Done():
		t.Fatal("concurrent read-write test timed out (possible deadlock)")
	}

	close(errCh)
	for err := range errCh {
		t.Fatalf("goroutine error: %v", err)
	}

	// After all writes, verify we can read the final state without panic
	providers := f.GetAvailableProviders()
	if len(providers) == 0 {
		t.Fatal("expected at least one provider after concurrent writes")
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests — concurrent writes
// ---------------------------------------------------------------------------

func TestConcurrentWrites_NoPanic(t *testing.T) {
	t.Parallel()

	f := newEmptyFactory(t)

	const goroutines = 20
	const iterations = 20

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("writer %d iter %d: %w", id, j, ctx.Err())
					return
				default:
				}

				cfgJSON := []byte(fmt.Sprintf(`{
					"name": "p%d_m%d",
					"endpoint": "https://p%d.example.com/v1/chat/completions",
					"auth": {"type": "bearer"},
					"defaults": {"model": "model-%d"},
					"models": {"default_context_limit": %d}
				}`, id, j, id, j, 4096+j*512))

				if err := f.LoadConfigFromBytes(cfgJSON); err != nil {
					errCh <- fmt.Errorf("writer %d iter %d LoadConfigFromBytes: %w", id, j, err)
					return
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed — success
	case <-ctx.Done():
		t.Fatal("concurrent writes test timed out (possible deadlock)")
	}

	close(errCh)
	for err := range errCh {
		t.Fatalf("goroutine error: %v", err)
	}

	// Verify the factory is in a consistent state
	providers := f.GetAvailableProviders()
	if len(providers) == 0 {
		t.Fatal("expected at least one provider after concurrent writes")
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests — concurrent ReloadConfig
// ---------------------------------------------------------------------------

func TestConcurrentReloadConfig_NoDeadlock(t *testing.T) {
	t.Parallel()

	tmpFile := t.TempDir() + "/testprov.json"
	if err := fWriteFile(tmpFile, string(baseConfig)); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	f := newEmptyFactory(t)
	if err := f.LoadConfigFromFile(tmpFile); err != nil {
		t.Fatalf("LoadConfigFromFile: %v", err)
	}

	const goroutines = 10
	const iterations = 10

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var reloadsCompleted atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Alternate between the two config versions
				var data []byte
				if j%2 == 0 {
					data = baseConfig
				} else {
					data = baseConfigV2
				}
				if err := fWriteFile(tmpFile, string(data)); err != nil {
					// File write errors are rare but possible; skip iteration
					continue
				}

				if err := f.ReloadConfig(tmpFile); err != nil {
					// Reload may fail if file was overwritten mid-read;
					// we're mostly checking for deadlocks, not perfect accuracy
					continue
				}
				reloadsCompleted.Add(1)

				// Verify we can still read after reload
				cfg, err := f.GetProviderConfig("testprov")
				if err != nil || cfg == nil {
					continue // non-fatal during concurrent reload
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed — success
	case <-ctx.Done():
		t.Fatal("concurrent reload test timed out (possible deadlock)")
	}

	if reloadsCompleted.Load() == 0 {
		t.Fatal("no reloads completed — something went wrong")
	}
}

// ---------------------------------------------------------------------------
// Concurrency tests — concurrent LoadConfigFromFile
// ---------------------------------------------------------------------------

func TestConcurrentLoadConfigFromFile_NoPanic(t *testing.T) {
	t.Parallel()

	f := newEmptyFactory(t)

	const goroutines = 10
	const iterations = 10

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create temp files with unique configs
	files := make([]string, goroutines)
	for i := 0; i < goroutines; i++ {
		cfgJSON := fmt.Sprintf(`{
			"name": "fileprov%d",
			"endpoint": "https://file%d.example.com/v1/chat/completions",
			"auth": {"type": "bearer"},
			"defaults": {"model": "model-%d"},
			"models": {"default_context_limit": 4096}
		}`, i, i, i)
		path := t.TempDir() + fmt.Sprintf("/fileprov%d.json", i)
		if err := fWriteFile(path, cfgJSON); err != nil {
			t.Fatalf("failed to write temp file %d: %v", i, err)
		}
		files[i] = path
	}

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("goroutine %d iter %d: %w", id, j, ctx.Err())
					return
				default:
				}

				if err := f.LoadConfigFromFile(files[id]); err != nil {
					errCh <- fmt.Errorf("goroutine %d iter %d LoadConfigFromFile: %w", id, j, err)
					return
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed — success
	case <-ctx.Done():
		t.Fatal("concurrent LoadConfigFromFile test timed out (possible deadlock)")
	}

	close(errCh)
	for err := range errCh {
		t.Fatalf("goroutine error: %v", err)
	}

	// Verify all 10 unique providers were loaded
	providers := f.GetAvailableProviders()
	if len(providers) < goroutines {
		t.Fatalf("expected at least %d providers, got %d", goroutines, len(providers))
	}
}

// ---------------------------------------------------------------------------
// Mixed read-write-write-reload stress test
// ---------------------------------------------------------------------------

func TestStress_MixedOperations(t *testing.T) {
	t.Parallel()

	f := newEmptyFactory(t)

	// Create temp files for reload tests
	tmpFiles := make([]string, 5)
	for i := 0; i < 5; i++ {
		cfgJSON := fmt.Sprintf(`{
			"name": "stress%d",
			"endpoint": "https://stress%d.example.com/v1/chat/completions",
			"auth": {"type": "bearer"},
			"defaults": {"model": "model-stress-%d"},
			"models": {"default_context_limit": 8192}
		}`, i, i, i)
		path := t.TempDir() + fmt.Sprintf("/stress%d.json", i)
		if err := fWriteFile(path, cfgJSON); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		tmpFiles[i] = path
	}

	const goroutines = 15
	const iterations = 20

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("goroutine %d iter %d: %w", id, j, ctx.Err())
					return
				default:
				}

				// Rotate through all operations deterministically by iteration
				op := (id+j)%9
				switch op {
				case 0, 1:
					// Read operations
					_ = f.GetAvailableProviders()
				case 2:
					_ = f.GetDefaultProvider()
				case 3:
					_ = f.ListProvidersWithModels()
				case 4:
					// Try to get config for any existing provider
					names := f.GetAvailableProviders()
					if len(names) > 0 {
						_, _ = f.GetProviderConfig(names[0])
					}
				case 5:
					// Try to create provider
					names := f.GetAvailableProviders()
					if len(names) > 0 {
						_, _ = f.CreateProvider(names[0])
					}
				case 6:
					// Write via LoadConfigFromBytes
					cfgJSON := []byte(fmt.Sprintf(`{
						"name": "bytes%d_%d",
						"endpoint": "https://bytes%d_%d.example.com/v1/chat/completions",
						"auth": {"type": "bearer"},
						"defaults": {"model": "bytes-model"},
						"models": {"default_context_limit": 4096}
					}`, id, j, id, j))
					if err := f.LoadConfigFromBytes(cfgJSON); err != nil {
						errCh <- fmt.Errorf("LoadConfigFromBytes(%d,%d): %w", id, j, err)
						return
					}
				case 7:
					// Write via LoadConfigFromFile
					fileIdx := (id + j) % len(tmpFiles)
					if err := f.LoadConfigFromFile(tmpFiles[fileIdx]); err != nil {
						errCh <- fmt.Errorf("LoadConfigFromFile(%d,%d): %w", id, j, err)
						return
					}
				case 8:
					// ReloadConfig
					fileIdx := (id + j) % len(tmpFiles)
					_ = f.ReloadConfig(tmpFiles[fileIdx])
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed — success
	case <-ctx.Done():
		t.Fatal("stress test timed out (possible deadlock)")
	}

	close(errCh)
	for err := range errCh {
		t.Fatalf("goroutine error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// UpsertConfig tests
// ---------------------------------------------------------------------------

func TestUpsertConfig_NewProvider(t *testing.T) {
	t.Parallel()

	f := newEmptyFactory(t)

	cfg := &ProviderConfig{
		Name:     "newprov",
		Endpoint: "https://new.example.com/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "new-model"},
		Models:   ModelConfig{DefaultContextLimit: 8192},
	}

	f.UpsertConfig("newprov", cfg)

	// Verify via GetProviderConfig (reads from f.configs)
	got, err := f.GetProviderConfig("newprov")
	if err != nil {
		t.Fatalf("GetProviderConfig after upsert: %v", err)
	}
	if got.Name != "newprov" {
		t.Errorf("expected name newprov, got %q", got.Name)
	}
	if got.Defaults.Model != "new-model" {
		t.Errorf("expected model new-model, got %q", got.Defaults.Model)
	}

	// Verify via GetRegistry (reads from f.registry.ProviderConfigs)
	reg := f.GetRegistry()
	if _, ok := reg.ProviderConfigs["newprov"]; !ok {
		t.Fatal("expected newprov in registry.ProviderConfigs after upsert")
	}
	if reg.ProviderConfigs["newprov"].Defaults.Model != "new-model" {
		t.Errorf("expected model new-model in registry, got %q", reg.ProviderConfigs["newprov"].Defaults.Model)
	}

	// Verify it appears in GetAvailableProviders
	providers := f.GetAvailableProviders()
	found := false
	for _, p := range providers {
		if p == "newprov" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected newprov in GetAvailableProviders")
	}
}

func TestUpsertConfig_OverwriteExisting(t *testing.T) {
	t.Parallel()

	f := newTestFactory(t)

	// Verify the initial config loaded from baseConfig
	initialCfg, _ := f.GetProviderConfig("testprov")
	if initialCfg.Defaults.Model != "test-model-1" {
		t.Fatalf("expected initial model test-model-1, got %q", initialCfg.Defaults.Model)
	}

	// Upsert an updated config
	updatedCfg := &ProviderConfig{
		Name:     "testprov",
		Endpoint: "https://updated.example.com/v1/chat/completions",
		Auth:     AuthConfig{Type: "api_key"},
		Defaults: RequestDefaults{Model: "updated-model"},
		Models:   ModelConfig{DefaultContextLimit: 65536},
	}
	f.UpsertConfig("testprov", updatedCfg)

	// Verify the config was replaced
	got, err := f.GetProviderConfig("testprov")
	if err != nil {
		t.Fatalf("GetProviderConfig after overwrite: %v", err)
	}
	if got.Defaults.Model != "updated-model" {
		t.Errorf("expected updated model updated-model, got %q", got.Defaults.Model)
	}
	if got.Endpoint != "https://updated.example.com/v1/chat/completions" {
		t.Errorf("expected updated endpoint, got %q", got.Endpoint)
	}
	if got.Auth.Type != "api_key" {
		t.Errorf("expected updated auth type api_key, got %q", got.Auth.Type)
	}
	if got.Models.DefaultContextLimit != 65536 {
		t.Errorf("expected updated context limit 65536, got %d", got.Models.DefaultContextLimit)
	}

	// Verify registry is also updated
	reg := f.GetRegistry()
	if reg.ProviderConfigs["testprov"].Defaults.Model != "updated-model" {
		t.Errorf("expected registry to be updated, got model %q", reg.ProviderConfigs["testprov"].Defaults.Model)
	}
}

func TestUpsertConfig_NilConfig(t *testing.T) {
	t.Parallel()

	f := newTestFactory(t)

	// Upsert with nil config should not panic and should not change anything
	f.UpsertConfig("testprov", nil)

	// Original config should still be there
	cfg, err := f.GetProviderConfig("testprov")
	if err != nil {
		t.Fatalf("GetProviderConfig after nil upsert: %v", err)
	}
	if cfg.Defaults.Model != "test-model-1" {
		t.Errorf("expected original model test-model-1, got %q", cfg.Defaults.Model)
	}

	// Upsert a new name with nil config should not create an entry
	f.UpsertConfig("nonexistent", nil)
	_, err = f.GetProviderConfig("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent provider after nil upsert")
	}
}

func TestUpsertConfig_EmptyName(t *testing.T) {
	t.Parallel()

	f := newTestFactory(t)

	cfg := &ProviderConfig{
		Name:     "someprov",
		Endpoint: "https://some.example.com/v1",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "some-model"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	// Upsert with empty name — should insert under "" key
	f.UpsertConfig("", cfg)

	// Verify it's stored under empty string key
	got, err := f.GetProviderConfig("")
	if err != nil {
		t.Fatalf("GetProviderConfig for empty name: %v", err)
	}
	if got.Name != "someprov" {
		t.Errorf("expected name someprov, got %q", got.Name)
	}

	// Verify it appears in GetAvailableProviders
	providers := f.GetAvailableProviders()
	found := false
	for _, p := range providers {
		if p == "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected empty-string key in GetAvailableProviders")
	}
}

func TestUpsertConfig_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	f := newEmptyFactory(t)

	cfg := &ProviderConfig{
		Name:     "copyprov",
		Endpoint: "https://copy.example.com/v1/chat/completions",
		Auth:     AuthConfig{Type: "bearer"},
		Defaults: RequestDefaults{Model: "original-model"},
		Models:   ModelConfig{DefaultContextLimit: 8192},
	}

	f.UpsertConfig("copyprov", cfg)

	// Mutate the input config AFTER upsert
	cfg.Name = "mutated-name"
	cfg.Defaults.Model = "mutated-model"
	cfg.Endpoint = "https://mutated.example.com/v1"

	// Verify the stored config was NOT affected
	got, err := f.GetProviderConfig("copyprov")
	if err != nil {
		t.Fatalf("GetProviderConfig: %v", err)
	}
	if got.Name != "copyprov" {
		t.Errorf("expected stored name copyprov, got %q (input was mutated after upsert)", got.Name)
	}
	if got.Defaults.Model != "original-model" {
		t.Errorf("expected stored model original-model, got %q (input was mutated after upsert)", got.Defaults.Model)
	}
	if got.Endpoint != "https://copy.example.com/v1/chat/completions" {
		t.Errorf("expected stored endpoint unchanged, got %q", got.Endpoint)
	}
}

func TestUpsertConfig_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	f := newEmptyFactory(t)

	const writerGoroutines = 10
	const readerGoroutines = 10
	const iterations = 30

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, writerGoroutines+readerGoroutines)

	// Writers — each upserts a unique provider name
	wg.Add(writerGoroutines)
	for i := 0; i < writerGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("writer %d iter %d: %w", id, j, ctx.Err())
					return
				default:
				}

				cfg := &ProviderConfig{
					Name:     fmt.Sprintf("upsert%d_%d", id, j),
					Endpoint: "https://upsert.example.com/v1/chat/completions",
					Auth:     AuthConfig{Type: "bearer"},
					Defaults: RequestDefaults{Model: fmt.Sprintf("model-%d-%d", id, j)},
					Models:   ModelConfig{DefaultContextLimit: 4096},
				}
				f.UpsertConfig(cfg.Name, cfg)
			}
		}(i)
	}

	// Readers — read whatever configs exist
	wg.Add(readerGoroutines)
	for i := 0; i < readerGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					errCh <- fmt.Errorf("reader %d iter %d: %w", id, j, ctx.Err())
					return
				default:
				}

				switch j % 3 {
				case 0:
					_ = f.GetAvailableProviders()
				case 1:
					// Try to get any existing provider
					names := f.GetAvailableProviders()
					if len(names) > 0 {
						_, _ = f.GetProviderConfig(names[0])
					}
				case 2:
					_ = f.GetRegistry()
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed — success
	case <-ctx.Done():
		t.Fatal("concurrent UpsertConfig test timed out (possible deadlock)")
	}

	close(errCh)
	for err := range errCh {
		t.Fatalf("goroutine error: %v", err)
	}

	// Verify the factory is in a consistent state
	providers := f.GetAvailableProviders()
	if len(providers) == 0 {
		t.Fatal("expected at least one provider after concurrent upserts")
	}
}

// ---------------------------------------------------------------------------
// Helper: thin wrapper around os.WriteFile to avoid import in test data
// ---------------------------------------------------------------------------

func fWriteFile(path, data string) error {
	return os.WriteFile(path, []byte(data), 0644)
}
