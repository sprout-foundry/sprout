package credentials

import (
	"fmt"
	"log"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	// keyringServiceName is the service name used for go-keyring
	keyringServiceName = "ledit"
	// keyringProbeProvider is a special provider name used to probe keyring availability
	keyringProbeProvider = "__ledit_probe__"
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

	value, err := keyring.Get(b.service, provider)
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

	err := keyring.Set(b.service, provider, value)
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

	err := keyring.Delete(b.service, provider)
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

	store, err := Load()
	if err != nil {
		return fmt.Errorf("failed to load credentials from file store: %w", err)
	}

	store[provider] = value

	if err := Save(store); err != nil {
		return fmt.Errorf("failed to save credentials to file store: %w", err)
	}

	return nil
}

// Delete removes a credential from the encrypted file store.
func (b *FileBackend) Delete(provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}

	store, err := Load()
	if err != nil {
		return fmt.Errorf("failed to load credentials from file store: %w", err)
	}

	if _, exists := store[provider]; !exists {
		// Key doesn't exist - this is not an error
		return nil
	}

	delete(store, provider)

	if err := Save(store); err != nil {
		return fmt.Errorf("failed to save credentials to file store: %w", err)
	}

	return nil
}

// IsKeyringAvailable checks if the OS keyring is available for use.
// This is useful for auto-detection of the preferred backend.
func IsKeyringAvailable() bool {
	backend := NewOSKeyringBackend()
	_, err := backend.Get(keyringProbeProvider)
	// If we get ErrNotFound or no error, keyring is available
	// If we get any other error, keyring is not available
	if err == keyring.ErrNotFound {
		return true
	}
	if err != nil {
		log.Printf("[credentials] OS keyring not available: %v", err)
		return false
	}
	// Probe key exists - keyring is available
	return true
}
