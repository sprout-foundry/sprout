package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/spf13/cobra"
)

// backendCmd shows the current storage backend status.
var backendCmd = &cobra.Command{
	Use:   "backend",
	Short: "Show or manage the storage backend",
	Long: `Show, set, or migrate the credential storage backend.

The storage backend determines where encrypted API keys are stored:
  - keyring: OS-native keyring (GNOME Keyring, macOS Keychain, Windows Credential Manager)
  - file:    Encrypted JSON file in ~/.ledit/
  - auto:    Auto-detect on first use (default)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackendStatus()
	},
}

// backendSetCmd sets the storage backend mode.
var backendSetCmd = &cobra.Command{
	Use:   "set <mode>",
	Short: "Set the storage backend mode",
	Long: `Set the storage backend mode to 'keyring', 'file', or 'auto'.

Modes:
  keyring  Use OS-native keyring for credential storage
  file     Use encrypted JSON file for credential storage
  auto     Reset to auto-detection (deletes the persisted mode)

When switching away from keyring, a warning is shown if credentials exist in it.
When switching to keyring, you will be offered an option to migrate file credentials.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackendSet(args[0])
	},
}

// backendMigrateToKeyringCmd migrates file credentials to keyring.
var backendMigrateToKeyringCmd = &cobra.Command{
	Use:   "migrate-to-keyring",
	Short: "Migrate all credentials from file to OS keyring",
	Long: `Migrate all credentials from the encrypted file store to the OS keyring.
After successful migration, the file store is cleared.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		migrated, err := credentials.MigrateFileToKeyring(true)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		if len(migrated) == 0 {
			fmt.Println("No credentials found in file store. Nothing to migrate.")
			return nil
		}

		fmt.Printf("Successfully migrated %d credential(s) to OS keyring:\n", len(migrated))
		for _, p := range migrated {
			fmt.Printf("  - %s\n", p)
		}
		return nil
	},
}

// backendMigrateToFileCmd migrates keyring credentials to file.
var backendMigrateToFileCmd = &cobra.Command{
	Use:   "migrate-to-file",
	Short: "Migrate all credentials from OS keyring to file",
	Long: `Migrate all credentials from the OS keyring to the encrypted file store.
After successful migration, the keyring entries are cleared.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		migrated, err := credentials.MigrateKeyringToFile(true)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		if len(migrated) == 0 {
			fmt.Println("No credentials found in keyring. Nothing to migrate.")
			return nil
		}

		fmt.Printf("Successfully migrated %d credential(s) to file store:\n", len(migrated))
		for _, p := range migrated {
			fmt.Printf("  - %s\n", p)
		}
		return nil
	},
}

func init() {
	keysCmd.AddCommand(backendCmd)
	backendCmd.AddCommand(backendSetCmd)
	backendCmd.AddCommand(backendMigrateToKeyringCmd)
	backendCmd.AddCommand(backendMigrateToFileCmd)
}

// runBackendStatus displays the current storage backend status.
func runBackendStatus() error {
	mode, err := credentials.GetStorageMode()
	if err != nil {
		return fmt.Errorf("failed to get storage mode: %w", err)
	}

	keyringAvail := credentials.IsKeyringAvailable()
	providers, err := credentials.ListKeyringProviders()
	if err != nil {
		return fmt.Errorf("failed to list keyring providers: %w", err)
	}

	// Build display label: empty mode means auto
	if mode == "" {
		mode = "auto"
	}
	fmt.Printf("Storage backend: %s", describeMode(mode, keyringAvail, len(providers)))

	return nil
}

// describeMode builds a human-readable description of the current backend state.
func describeMode(mode string, keyringAvail bool, providerCount int) string {
	var parts []string

	if mode == "auto" {
		parts = append(parts, "auto-detected")
		if keyringAvail {
			parts = append(parts, "OS keyring available")
		} else {
			parts = append(parts, "OS keyring not available (will use file)")
		}
	} else {
		parts = append(parts, mode)
		if mode == "keyring" {
			parts = append(parts, fmt.Sprintf("OS keyring available=%v", keyringAvail))
		}
	}

	if providerCount > 0 {
		parts = append(parts, fmt.Sprintf("%d providers in keyring", providerCount))
	}

	return strings.Join(parts, ", ") + "\n"
}

// runBackendSet handles the `ledit keys backend set <mode>` command.
func runBackendSet(arg string) error {
	mode := strings.ToLower(arg)

	if mode != "keyring" && mode != "file" && mode != "auto" {
		return fmt.Errorf("invalid mode %q (must be 'keyring', 'file', or 'auto')", mode)
	}

	// Warn if switching away from keyring with credentials still in it
	if mode == "file" || mode == "auto" {
		providers, err := credentials.ListKeyringProviders()
		if err != nil {
			return fmt.Errorf("failed to list keyring providers: %w", err)
		}
		if len(providers) > 0 {
			fmt.Printf("Warning: %d provider(s) still have credentials in the OS keyring.\n", len(providers))
			fmt.Println("Run 'ledit keys backend migrate-to-file' to migrate them first.")
			if mode == "auto" {
				fmt.Println("After migration, credentials will be picked up from the file store.")
			}
		}
	}

	switch mode {
	case "auto":
		credentials.ResetStorageBackend()
		modePath, err := credentials.GetBackendModePath()
		if err != nil {
			return fmt.Errorf("failed to get backend mode path: %w", err)
		}
		if err := os.Remove(modePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove backend mode file: %w", err)
		}
		fmt.Println("Storage mode reset to auto-detection.")
		fmt.Println("The backend will be auto-detected on next use.")

	case "keyring":
		if !credentials.IsKeyringAvailable() {
			return fmt.Errorf("OS keyring is not available on this system")
		}
		if err := credentials.SetStorageMode("keyring"); err != nil {
			return fmt.Errorf("failed to set storage mode: %w", err)
		}
		credentials.ResetStorageBackend()
		fmt.Println("Storage mode set to: keyring")

		// Offer to migrate file credentials
		store, err := credentials.Load()
		if err != nil {
			return fmt.Errorf("failed to load file credentials: %w", err)
		}
		if len(store) > 0 {
			fmt.Printf("Found %d credential(s) in file store. Run 'ledit keys backend migrate-to-keyring' to migrate them.\n", len(store))
		}

	case "file":
		if err := credentials.SetStorageMode("file"); err != nil {
			return fmt.Errorf("failed to set storage mode: %w", err)
		}
		credentials.ResetStorageBackend()
		fmt.Println("Storage mode set to: file")
	}

	return nil
}
