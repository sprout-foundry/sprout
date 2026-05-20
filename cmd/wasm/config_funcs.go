//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// Cached pointer to the currently-loaded config. We keep one in memory so
// successive get/set calls don't churn IndexedDB; the StoreWriter persists
// on every Save() anyway.
var (
	configMu  sync.Mutex
	cachedCfg *configuration.Config
)

func configJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"getConfig":     js.FuncOf(getConfigFunc),
		"setConfig":     js.FuncOf(setConfigFunc),
		"getConfigPath": js.FuncOf(getConfigPathFunc),
		"resetConfig":   js.FuncOf(resetConfigFunc),
		"getAPIKeys":    js.FuncOf(getAPIKeysFunc),
		"setAPIKey":     js.FuncOf(setAPIKeyFunc),
		"removeAPIKey":  js.FuncOf(removeAPIKeyFunc),
	}
}

// loadCachedConfig returns the singleton Config, loading from disk on first
// access. Mutation through setConfigFunc invalidates the cache so the next
// getConfig sees the fresh struct.
func loadCachedConfig() (*configuration.Config, error) {
	configMu.Lock()
	defer configMu.Unlock()
	if cachedCfg != nil {
		return cachedCfg, nil
	}
	cfg, err := configuration.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	cachedCfg = cfg
	return cachedCfg, nil
}

func invalidateConfigCache() {
	configMu.Lock()
	cachedCfg = nil
	configMu.Unlock()
}

// ─── Config (top-level) ──────────────────────────────────────────

func getConfigFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(_ context.Context) (interface{}, error) {
		cfg, err := loadCachedConfig()
		if err != nil {
			return nil, err
		}
		// Marshal/unmarshal round-trip flattens the struct to a plain JSON
		// object that marshalJS can hand the browser without surprises.
		data, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		var out map[string]interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	})
}

// setConfigFunc accepts a JSON-stringified Config from JS, applies it, and
// persists. We deliberately take a string rather than navigating a js.Value
// object tree because the Config struct is large and JSON round-trip gives
// us deterministic field validation for free.
func setConfigFunc(_ js.Value, args []js.Value) interface{} {
	raw := argString(args, 0, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if raw == "" {
			return nil, fmt.Errorf("config JSON is required (first arg)")
		}
		var cfg configuration.Config
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("save config: %w", err)
		}
		invalidateConfigCache()
		return map[string]interface{}{"ok": true}, nil
	})
}

func getConfigPathFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(_ context.Context) (interface{}, error) {
		path, err := configuration.GetConfigPath()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"path": path}, nil
	})
}

// resetConfigFunc replaces the on-disk config with NewConfig() defaults.
// Useful when the user wants to start fresh from the UI without manually
// deleting the file.
func resetConfigFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(_ context.Context) (interface{}, error) {
		fresh := configuration.NewConfig()
		if err := fresh.Save(); err != nil {
			return nil, err
		}
		invalidateConfigCache()
		return map[string]interface{}{"ok": true}, nil
	})
}

// ─── API Keys ────────────────────────────────────────────────────
// Note on security: in the WASM build, API keys live in IndexedDB via the
// MEMFS store. This is no better or worse than localStorage from a browser
// threat model, but is significantly weaker than the native CLI's keyring
// integration. SP-045-4a will revisit this with a Web Crypto envelope
// design before agent/LLM commands ship in WASM.

func getAPIKeysFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(_ context.Context) (interface{}, error) {
		keys, err := configuration.LoadAPIKeys()
		if err != nil {
			return nil, err
		}
		// Don't leak full keys back to JS — return only which providers
		// have a key set. The host page can call setAPIKey to update;
		// reading back a plaintext key in arbitrary scripts is rarely
		// what UIs actually need.
		out := make(map[string]bool, len(*keys))
		for provider, value := range *keys {
			out[provider] = value != ""
		}
		return out, nil
	})
}

func setAPIKeyFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	key := argString(args, 1, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required")
		}
		if key == "" {
			return nil, fmt.Errorf("key is required; use removeAPIKey to clear")
		}
		keys, err := configuration.LoadAPIKeys()
		if err != nil {
			return nil, err
		}
		keys.Set(provider, key)
		if err := configuration.SaveAPIKeys(keys); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "provider": provider}, nil
	})
}

func removeAPIKeyFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required")
		}
		keys, err := configuration.LoadAPIKeys()
		if err != nil {
			return nil, err
		}
		delete(*keys, provider)
		if err := configuration.SaveAPIKeys(keys); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "provider": provider}, nil
	})
}
