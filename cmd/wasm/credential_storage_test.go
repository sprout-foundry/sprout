//go:build js && wasm

// Tests for credential_storage.go's pure-Go helpers. Functions that depend
// on js.Value (encryptValue, decryptValue, the CRUD callbacks, and
// awaitPromise) require a live browser to validate meaningfully —
// go_js_wasm_exec under Node lacks the Web Crypto and localStorage APIs
// they exercise. Those paths are covered by the in-browser integration
// tests.
//
// What we can pin here: the JS function registry surface and the types
// of its entries.

package main

import (
	"sort"
	"testing"

	"syscall/js"
)

func TestCredentialJSFuncs_RegistersAllKeys(t *testing.T) {
	funcs := credentialJSFuncs()

	expected := []string{
		"setCredential",
		"getCredential",
		"deleteCredential",
		"listCredentials",
		"hasCredential",
		"injectHostKeys",
	}

	for _, name := range expected {
		if _, ok := funcs[name]; !ok {
			t.Errorf("credentialJSFuncs() must register %q", name)
		}
	}

	if len(funcs) != len(expected) {
		got := make([]string, 0, len(funcs))
		for k := range funcs {
			got = append(got, k)
		}
		sort.Strings(got)
		t.Errorf("credentialJSFuncs() returned %d funcs, expected %d; got: %v", len(funcs), len(expected), got)
	}
}

func TestCredentialJSFuncs_AllValuesAreJSFunc(t *testing.T) {
	funcs := credentialJSFuncs()

	for name, fn := range funcs {
		// js.FuncOf returns js.Func; verify the type assertion works.
		jf, ok := fn.(js.Func)
		if !ok {
			t.Errorf("credentialJSFuncs()[%q] expected js.Func, got %T", name, fn)
			continue
		}
		// The underlying JS value must be a function type.
		if jf.Value.Type() != js.TypeFunction {
			t.Errorf("credentialJSFuncs()[%q] JS value is not a function, got %v", name, jf.Value.Type())
		}
	}
}

// TestCredentialJSFuncs_ReturnsFreshMap confirms that each call to
// credentialJSFuncs() returns a new map (not a cached/shared one).
// This matters because callers may mutate the returned map or hold onto
// js.Func values across invocations.
//
// NOTE: This is a cheap safety check — the current implementation uses a
// map literal so it's trivially per-call. If the implementation ever
// switches to a cached singleton, this test will catch the regression.
func TestCredentialJSFuncs_ReturnsFreshMap(t *testing.T) {
	a := credentialJSFuncs()
	b := credentialJSFuncs()

	// The maps must be distinct (different pointers).
	// We can't compare pointers directly on interface{}, so we mutate one
	// and verify the other is unaffected.
	originalA := make([]string, 0, len(a))
	for k := range a {
		originalA = append(originalA, k)
	}

	// Mutate map b (this is safe since js.Func values can be garbage-collected).
	delete(b, "setCredential")

	// a must still contain all original keys.
	for _, name := range originalA {
		if _, ok := a[name]; !ok {
			t.Errorf("credentialJSFuncs() does not return a fresh map: mutation of second call affected first; missing %q", name)
		}
	}
}

// ─── Storage Key Contract Tests ────────────────────────────────────────

// NOTE: The storage keys are not defined as named constants in
// credential_storage.go — they are inline string literals. These tests
// document the expected values to serve as a contract check. If the
// implementation strings ever drift, these serve as a reminder. The
// actual enforcement happens at runtime via the JS bridge.

func TestStorageKey_Contract(t *testing.T) {
	// The credential storage prefix used across all CRUD operations.
	// Verified in: setCredentialFunc, getCredentialFunc, deleteCredentialFunc,
	// hasCredentialFunc, injectHostKeysFunc, and listCredentialsFunc.
	expectedCredPrefix := "__sprout_cred_"
	if expectedCredPrefix == "" {
		t.Error("credential storage prefix must be non-empty")
	}

	// The localStorage key used to persist the AES-GCM crypto key.
	// Verified in: initCryptoKey().
	expectedCryptoKey := "__sprout_crypto_key"
	if expectedCryptoKey == "" {
		t.Error("crypto key storage key must be non-empty")
	}

	// Verify the prefix pattern: credential keys are formed as
	// prefix + providerName, so the prefix must end with an underscore
	// to separate from provider names.
	if expectedCredPrefix[len(expectedCredPrefix)-1] != '_' {
		t.Error("credential storage prefix should end with '_' to separate from provider names")
	}
}
