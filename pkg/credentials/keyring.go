package credentials

import (
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	// keyringServiceName is the service name used for go-keyring
	keyringServiceName = "sprout"
	// keyringProbeProvider is a special provider name used to probe keyring availability
	keyringProbeProvider = "__sprout_probe__"
)

// Backend interface defines the contract for credential storage backends.
// Implementations can be OS keyring, encrypted file store, or any other backend.
type Backend interface {
	// Get retrieves a credential for the given provider.
	// Returns empty string and no error if the provider has no stored credential.
	// Returns an error only for backend-specific failures (e.g., keyring unavailable).
	Get(provider string) (string, error)

	// Set stores a credential for the given provider.
	Set(provider, value string) error

	// Delete removes a credential for the given provider.
	// Returns no error if the provider has no stored credential.
	Delete(provider string) error

	// Source returns the source identifier for this backend (e.g., "keyring", "stored").
	Source() string
}

// OSKeyringBackend wraps go-keyring for OS-native credential storage.
// It uses the system keyring (GNOME Keyring, macOS Keychain, Windows Credential Manager, etc.)
type OSKeyringBackend struct {
	service string
}

// safeKeyringGet wraps keyring.Get with panic recovery.
// The go-keyring library panics on unsupported platforms (Android/Termux, headless)
// instead of returning an error. This wrapper converts panics to errors.
func safeKeyringGet(service, key string) (val string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("keyring get panic: %v", r)
		}
	}()
	return keyring.Get(service, key)
}

// safeKeyringSet wraps keyring.Set with panic recovery.
func safeKeyringSet(service, key, value string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("keyring set panic: %v", r)
		}
	}()
	return keyring.Set(service, key, value)
}

// safeKeyringDelete wraps keyring.Delete with panic recovery.
func safeKeyringDelete(service, key string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("keyring delete panic: %v", r)
		}
	}()
	return keyring.Delete(service, key)
}

// NewOSKeyringBackend creates a new OS keyring backend.
func NewOSKeyringBackend() *OSKeyringBackend {
	return &OSKeyringBackend{
		service: keyringServiceName,
	}
}

// Get retrieves a credential from the OS keyring.
// Returns empty string and no error if the credential doesn't exist.
// Returns an error if the keyring is unavailable or another backend error occurs.
func (b *OSKeyringBackend) Get(provider string) (string, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "", fmt.Errorf("provider name cannot be empty")
	}

	value, err := safeKeyringGet(b.service, provider)
	if err == keyring.ErrNotFound {
		// Keyring doesn't have this credential - return empty, no error
		// This matches the behavior of the file store
		return "", nil
	}
	if err != nil {
		// Other errors (keyring unavailable, DBus not running, etc.)
		return "", fmt.Errorf("failed to get credential from keyring for provider %q: %w", provider, err)
	}

	// TrimSpace for consistency with FileBackend.Get, which also trims
	return strings.TrimSpace(value), nil
}

// Source returns the source identifier for OSKeyringBackend.
func (b *OSKeyringBackend) Source() string {
	return "keyring"
}

// Set stores a credential in the OS keyring.
func (b *OSKeyringBackend) Set(provider, value string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	if value == "" {
		return fmt.Errorf("credential value cannot be empty")
	}
	// Trim whitespace from value to store canonical form
	value = strings.TrimSpace(value)

	err := safeKeyringSet(b.service, provider, value)
	if err != nil {
		return fmt.Errorf("failed to store credential in keyring for provider %q: %w", provider, err)
	}

	return nil
}

// Delete removes a credential from the OS keyring.
// Returns no error if the credential doesn't exist.
func (b *OSKeyringBackend) Delete(provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}

	err := safeKeyringDelete(b.service, provider)
	if err == keyring.ErrNotFound {
		// Key doesn't exist - this is not an error
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to delete credential from keyring for provider %q: %w", provider, err)
	}

	return nil
}

// FileBackend wraps the existing encrypted file store as a Backend.
// This allows the Backend interface to be used uniformly for both keyring and file storage.
type FileBackend struct {
	// No additional state needed - uses global Load()/Save() functions
}

// NewFileBackend creates a new file backend.
func NewFileBackend() *FileBackend {
	return &FileBackend{}
}

// Get retrieves a credential from the encrypted file store.
func (b *FileBackend) Get(provider string) (string, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "", fmt.Errorf("provider name cannot be empty")
	}

	store, err := Load()
	if err != nil {
		return "", fmt.Errorf("failed to load credentials from file store: %w", err)
	}

	value := strings.TrimSpace(store[provider])
	if value == "" {
		// Not found - return empty, no error
		return "", nil
	}

	return value, nil
}

// Source returns the source identifier for FileBackend.
func (b *FileBackend) Source() string {
	return "stored"
}

// Set stores a credential in the encrypted file store.
// Uses AtomicModify to ensure the load-modify-save cycle is atomic,
// preventing TOCTOU races when multiple processes modify the store concurrently.
func (b *FileBackend) Set(provider, value string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	if value == "" {
		return fmt.Errorf("credential value cannot be empty")
	}
	// Trim whitespace from value to store canonical form
	value = strings.TrimSpace(value)

	return AtomicModify(func(store Store) error {
		store[provider] = value
		return nil
	})
}

// Delete removes a credential from the encrypted file store.
// Uses AtomicModify to ensure the load-modify-save cycle is atomic,
// preventing TOCTOU races when multiple processes modify the store concurrently.
func (b *FileBackend) Delete(provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}

	return AtomicModify(func(store Store) error {
		delete(store, provider)
		return nil
	})
}

// IsKeyringAvailable checks if the OS keyring is available for use.
// It is safe to call on any platform — panics from the underlying keyring
// library (e.g. on Android/Termux where no keyring service exists) are
// recovered and treated as "unavailable".
func IsKeyringAvailable() (available bool) {
	// Recover from panics in go-keyring on unsupported platforms (Android, headless, etc.)
	defer func() {
		if r := recover(); r != nil {
			debugLogf("[credentials] OS keyring not available (panic): %v", r)
			available = false
		}
	}()

	backend := NewOSKeyringBackend()
	_, err := backend.Get(keyringProbeProvider)
	if err != nil {
		debugLogf("[credentials] OS keyring not available: %v", err)
		return false
	}
	// No error means keyring responded successfully (entry absent or present).
	return true
}
