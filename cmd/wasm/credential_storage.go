//go:build js && wasm

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"syscall/js"
)

// ─── Storage Key Constants ───────────────────────────────────────────────

const (
	credStoragePrefix  = "__sprout_cred_"
	cryptoKeyStorageID = "__sprout_crypto_key"
)

// ─── Crypto Key Management ──────────────────────────────────────────────

// cryptoKey holds the Web Crypto AES-GCM key used for all credential
// encryption/decryption. Initialized lazily via initCryptoKey().
var (
	cryptoKey    js.Value
	cryptoKeyErr error
	cryptoOnce   sync.Once
)

// initCryptoKey ensures an AES-256-GCM key exists. On first invocation it
// checks localStorage for a previously-stored key (stored as
// __sprout_crypto_key in base64). If found, it imports the raw bytes back
// via crypto.subtle.importKey. Otherwise it generates a fresh key, exports
// the raw bytes, base64-encodes them, and persists them to localStorage
// so the key survives page reloads.
//
// Subsequent calls are no-ops: the key is cached in the cryptoKey variable
// for the lifetime of the WASM process.
func initCryptoKey() error {
	cryptoOnce.Do(func() {
		localStorage := js.Global().Get("localStorage")

		// Try to restore a previously-persisted key.
		stored := localStorage.Call("getItem", cryptoKeyStorageID)
		if stored.IsUndefined() || stored.IsNull() || stored.String() == "" {
			// No stored key — generate a fresh AES-256-GCM key.
			subtle := js.Global().Get("crypto").Get("subtle")
			key, err := awaitPromise(subtle.Call("generateKey",
				js.Global().Get("JSON").Call("parse", `{"name":"AES-GCM","length":256}`),
				true,
				[]js.Value{js.ValueOf("encrypt"), js.ValueOf("decrypt")},
			))
			if err != nil {
				cryptoKeyErr = fmt.Errorf("generate crypto key: %w", err)
				return
			}
			cryptoKey = key

			// Export raw bytes and persist to localStorage for future reloads.
			rawPromise := subtle.Call("exportKey", "raw", cryptoKey)
			rawArrayBuf, err := awaitPromise(rawPromise)
			if err != nil {
				cryptoKeyErr = fmt.Errorf("export crypto key: %w", err)
				return
			}
			rawBytes := bytesFromUint8Array(js.Global().Get("Uint8Array").New(rawArrayBuf))
			encoded := base64.StdEncoding.EncodeToString(rawBytes)
			localStorage.Call("setItem", cryptoKeyStorageID, encoded)
			return
		}

		// Restore from stored base64 raw bytes.
		encoded := stored.String()
		rawBytes, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			cryptoKeyErr = fmt.Errorf("decode stored crypto key: %w", err)
			return
		}

		subtle := js.Global().Get("crypto").Get("subtle")
		keyPromise := subtle.Call("importKey",
			"raw",
			uint8ArrayFromBytes(rawBytes),
			js.ValueOf("AES-GCM"),
			true,
			[]js.Value{js.ValueOf("encrypt"), js.ValueOf("decrypt")},
		)
		k, err := awaitPromise(keyPromise)
		if err != nil {
			cryptoKeyErr = fmt.Errorf("import stored crypto key: %w", err)
			return
		}
		cryptoKey = k
	})
	return cryptoKeyErr
}

// ─── Promise Helper ─────────────────────────────────────────────────────

// awaitPromise blocks the current Go goroutine until a JavaScript Promise
// resolves or rejects. This bridges async Web Crypto APIs into synchronous
// Go code.
//
// The pattern uses a channel: two JS callbacks (onSuccess and onError) are
// attached via .then() and .catch(), each sending their result on the
// channel. The Go side blocks on <-ch until one fires.
//
// Because each call to asPromise() already spawns a goroutine, the
// synchronous block here does NOT deadlock the JS event loop.
func awaitPromise(promise js.Value) (js.Value, error) {
	ch := make(chan struct {
		val js.Value
		err error
	}, 1)

	promise.Call("then", js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		ch <- struct {
			val js.Value
			err error
		}{args[0], nil}
		return nil
	})).Call("catch", js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		msg := "unknown error"
		if args[0].Type() == js.TypeString {
			msg = args[0].String()
		}
		ch <- struct {
			val js.Value
			err error
		}{js.Undefined(), fmt.Errorf("promise rejected: %s", msg)}
		return nil
	}))

	res := <-ch
	return res.val, res.err
}

// ─── Encryption / Decryption ─────────────────────────────────────────────

// encryptValue encrypts plainText using the package-level AES-GCM key. It
// generates a fresh 12-byte IV, encrypts the plaintext, and returns the
// IV+ciphertext as a single base64-encoded string.
func encryptValue(plainText string) (string, error) {
	if err := initCryptoKey(); err != nil {
		return "", err
	}

	subtle := js.Global().Get("crypto").Get("subtle")

	// Generate a random 12-byte IV.
	ivJS := js.Global().Get("Uint8Array").New(12)
	js.Global().Get("crypto").Call("getRandomValues", ivJS)
	ivBytes := bytesFromUint8Array(ivJS)

	// Convert plaintext to Uint8Array.
	dataBytes := []byte(plainText)
	dataJS := uint8ArrayFromBytes(dataBytes)

	// Build algorithm object: {name: "AES-GCM", iv: <Uint8Array>}
	algo := js.Global().Get("JSON").Call("parse", `{"name":"AES-GCM"}`)
	algo.Set("iv", ivJS)

	// Encrypt.
	promise := subtle.Call("encrypt", algo, cryptoKey, dataJS)
	result, err := awaitPromise(promise)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	// Wrap ArrayBuffer in Uint8Array to extract bytes.
	cipherBytes := bytesFromUint8Array(js.Global().Get("Uint8Array").New(result))

	// Wire format: [12-byte IV][ciphertext + 16-byte GCM tag], base64-encoded
	combined := append(ivBytes, cipherBytes...)
	return base64.StdEncoding.EncodeToString(combined), nil
}

// decryptValue reverses encryptValue: base64-decode, split IV/ciphertext,
// decrypt with AES-GCM, and return the plaintext string.
func decryptValue(encoded string) (string, error) {
	if err := initCryptoKey(); err != nil {
		return "", err
	}

	subtle := js.Global().Get("crypto").Get("subtle")

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if len(raw) < 12 {
		return "", fmt.Errorf("encoded data too short for IV")
	}

	ivBytes := raw[:12]
	cipherBytes := raw[12:]

	ivJS := uint8ArrayFromBytes(ivBytes)
	cipherJS := uint8ArrayFromBytes(cipherBytes)

	algo := js.Global().Get("JSON").Call("parse", `{"name":"AES-GCM"}`)
	algo.Set("iv", ivJS)

	promise := subtle.Call("decrypt", algo, cryptoKey, cipherJS)
	result, err := awaitPromise(promise)
	if err != nil {
		return "", fmt.Errorf("credential data is invalid or corrupted")
	}

	plainBytes := bytesFromUint8Array(js.Global().Get("Uint8Array").New(result))
	return string(plainBytes), nil
}

// ─── Byte Conversion Helpers ────────────────────────────────────────────

// uint8ArrayFromBytes creates a new JS Uint8Array and copies Go bytes into it.
func uint8ArrayFromBytes(data []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(arr, data)
	return arr
}

// bytesFromUint8Array copies the contents of a JS Uint8Array into a Go []byte.
func bytesFromUint8Array(arr js.Value) []byte {
	length := arr.Get("length").Int()
	buf := make([]byte, length)
	js.CopyBytesToGo(buf, arr)
	return buf
}

// ─── Provider Validation ────────────────────────────────────────────────

// validateProviderName ensures a provider name is safe to use as a
// localStorage key component. Rejects empty names, path separators,
// backslashes, colons, null bytes, and excessively long names.
func validateProviderName(provider string) error {
	if provider == "" {
		return fmt.Errorf("provider name must not be empty")
	}
	if strings.ContainsAny(provider, "/\\:\x00") || len(provider) > 64 {
		return fmt.Errorf("invalid provider name %q", provider)
	}
	return nil
}

// ─── JS Function Registry ───────────────────────────────────────────────

// credentialJSFuncs returns the credential storage entries that main.go merges
// into the SproutWasm global.
func credentialJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"setCredential":    js.FuncOf(setCredentialFunc),
		"getCredential":    js.FuncOf(getCredentialFunc),
		"deleteCredential": js.FuncOf(deleteCredentialFunc),
		"listCredentials":  js.FuncOf(listCredentialsFunc),
		"hasCredential":    js.FuncOf(hasCredentialFunc),
		"injectHostKeys":   js.FuncOf(injectHostKeysFunc),
	}
}

// ─── Credential CRUD ────────────────────────────────────────────────────

// setCredentialFunc stores a credential for a given provider. The key is
// encrypted with AES-GCM and persisted in localStorage under
// __sprout_cred_<provider>.
//
// Args: provider (string), key (string)
func setCredentialFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	key := argString(args, 1, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if err := validateProviderName(provider); err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("key is required")
		}
		encrypted, err := encryptValue(key)
		if err != nil {
			return nil, fmt.Errorf("encrypt credential: %w", err)
		}
		js.Global().Get("localStorage").Call("setItem",
			fmt.Sprintf("%s%s", credStoragePrefix, provider), encrypted)
		return map[string]interface{}{"ok": true, "provider": provider}, nil
	})
}

// getCredentialFunc retrieves a credential for a given provider.
// Returns confirmation that the credential exists but never exposes
// the decrypted key value to arbitrary JavaScript callers.
//
// Args: provider (string)
func getCredentialFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if err := validateProviderName(provider); err != nil {
			return nil, err
		}
		encVal := js.Global().Get("localStorage").Call("getItem",
			fmt.Sprintf("%s%s", credStoragePrefix, provider))
		if encVal.IsUndefined() || encVal.IsNull() || encVal.String() == "" {
			return nil, fmt.Errorf("credential not found for provider %q", provider)
		}
		return map[string]interface{}{
			"ok":       true,
			"provider": provider,
		}, nil
	})
}

// deleteCredentialFunc removes a credential from localStorage.
//
// Args: provider (string)
func deleteCredentialFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if err := validateProviderName(provider); err != nil {
			return nil, err
		}
		js.Global().Get("localStorage").Call("removeItem",
			fmt.Sprintf("%s%s", credStoragePrefix, provider))
		return map[string]interface{}{"ok": true, "provider": provider}, nil
	})
}

// listCredentialsFunc returns the provider names that have credentials
// stored. Never returns actual credential values.
func listCredentialsFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(_ context.Context) (interface{}, error) {
		localStorage := js.Global().Get("localStorage")
		length := localStorage.Call("length").Int()
		var providers []string
		for i := 0; i < length; i++ {
			k := localStorage.Call("key", i).String()
			if len(k) > len(credStoragePrefix) && k[:len(credStoragePrefix)] == credStoragePrefix {
				providers = append(providers, k[len(credStoragePrefix):])
			}
		}
		return map[string]interface{}{"providers": providers}, nil
	})
}

// hasCredentialFunc checks whether a credential exists for a provider.
//
// Args: provider (string)
func hasCredentialFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	return asPromise(func(_ context.Context) (interface{}, error) {
		if err := validateProviderName(provider); err != nil {
			return nil, err
		}
		val := js.Global().Get("localStorage").Call("getItem",
			fmt.Sprintf("%s%s", credStoragePrefix, provider))
		exists := !val.IsUndefined() && !val.IsNull() && val.String() != ""
		return map[string]interface{}{
			"exists":   exists,
			"provider": provider,
		}, nil
	})
}

// injectHostKeysFunc reads credentials from window.__sproutKeys (an object
// map of provider -> key) that the host page can set before the WASM module
// is initialized. This allows the host page to inject API keys without
// calling setCredential individually for each one.
//
// After processing, window.__sproutKeys is cleared (set to undefined) to
// prevent accidental leakage.
//
// Returns {injected: count, providers: [...]}.
func injectHostKeysFunc(_ js.Value, _ []js.Value) interface{} {
	return asPromise(func(_ context.Context) (interface{}, error) {
		sproutKeys := js.Global().Get("__sproutKeys")
		if sproutKeys.IsUndefined() || sproutKeys.IsNull() ||
			sproutKeys.Type() != js.TypeObject {
			return map[string]interface{}{"injected": 0}, nil
		}

		keyNames := js.Global().Get("Object").Call("keys", sproutKeys)
		count := keyNames.Get("length").Int()
		var providers []string

		for i := 0; i < count; i++ {
			provider := keyNames.Index(i).String()
			keyVal := sproutKeys.Get(provider)
			keyStr := keyVal.String()
			if keyStr == "" {
				continue
			}

			encrypted, err := encryptValue(keyStr)
			if err != nil {
				return nil, fmt.Errorf("encrypt host key for %s: %w", provider, err)
			}
			js.Global().Get("localStorage").Call("setItem",
				fmt.Sprintf("%s%s", credStoragePrefix, provider), encrypted)
			providers = append(providers, provider)
		}

		// Clear the host keys for security — they shouldn't linger in JS memory.
		js.Global().Set("__sproutKeys", js.Undefined())

		return map[string]interface{}{
			"injected":  len(providers),
			"providers": providers,
		}, nil
	})
}
