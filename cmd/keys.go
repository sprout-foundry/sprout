package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"filippo.io/age"
	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// keysCmd represents the keys command
var keysCmd = &cobra.Command{
	Use:   "keys",
	Short: "Manage API key encryption and credentials",
	Long: `Manage encryption and storage of API keys.

The ledit tool now encrypts API keys at rest using the age encryption library.
By default, a machine-specific key is used for transparent encryption.

Commands:
  status   - Show current encryption status
  encrypt  - Enable or change encryption mode
  decrypt  - Decrypt to plaintext (for migration/export)
  migrate  - Encrypt existing plaintext API keys
  rotate   - Generate a new machine key and re-encrypt all keys`,
}

// statusCmd shows encryption status
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current encryption status",
	Long:  `Display the current encryption status of your API keys file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		status, err := credentials.CheckEncryptionStatus()
		if err != nil {
			return fmt.Errorf("failed to check encryption status: %w", err)
		}

		if !status.Encrypted && !status.MachineKeyExists {
			fmt.Println("API keys are not encrypted (plaintext mode).")
			fmt.Println("Run 'ledit keys encrypt' to enable encryption.")
			return nil
		}

		if status.Encrypted {
			fmt.Printf("API keys are encrypted using %s mode.\n", status.Mode)
		} else {
			fmt.Println("API keys are not encrypted (plaintext mode).")
		}

		if status.MachineKeyExists {
			fmt.Println("Machine key exists: yes")
		} else {
			fmt.Println("Machine key exists: no")
		}

		return nil
	},
}

// encryptCmd enables encryption
var encryptCmd = &cobra.Command{
	Use:   "encrypt",
	Short: "Enable or change encryption mode",
	Long: `Enable encryption for your API keys using either a machine key or passphrase.

Machine key mode (default):
  - A unique X25519 key is generated and stored in ~/.ledit/key.age
  - No user interaction required after initial setup
  - Keys are encrypted automatically on save

Passphrase mode:
  - Encrypts using a user-provided passphrase
  - Enables portability of the encrypted store across machines
  - Requires passphrase entry each time keys are accessed

Examples:
  ledit keys encrypt                    # Use machine key mode
  ledit keys encrypt --passphrase       # Use passphrase mode`,
	RunE: func(cmd *cobra.Command, args []string) error {
		usePassphrase, _ := cmd.Flags().GetBool("passphrase")

		if usePassphrase {
			fmt.Println("Enter passphrase for encryption (will not be shown):")
			passphrase, err := readPassword()
			if err != nil {
				return fmt.Errorf("failed to read passphrase: %w", err)
			}

			// Validate passphrase strength
			if err := validatePassphrase(string(passphrase)); err != nil {
				return fmt.Errorf("invalid passphrase: %w", err)
			}

			fmt.Println("Re-enter passphrase to confirm:")
			confirmation, err := readPassword()
			if err != nil {
				// Zero out passphrase buffer before returning error
				for i := range passphrase {
					passphrase[i] = 0
				}
				return fmt.Errorf("failed to read confirmation: %w", err)
			}

			// Compare passphrases using constant-time comparison to prevent timing attacks
			if !bytes.Equal(passphrase, confirmation) {
				// Zero out buffers before returning error
				for i := range passphrase {
					passphrase[i] = 0
				}
				for i := range confirmation {
					confirmation[i] = 0
				}
				return fmt.Errorf("passphrases do not match")
			}

			// Zero out confirmation buffer after successful verification
			for i := range confirmation {
				confirmation[i] = 0
			}

			// Load existing keys
			store, err := credentials.Load()
			if err != nil {
				// Zero out passphrase before returning error
				for i := range passphrase {
					passphrase[i] = 0
				}
				return fmt.Errorf("failed to load API keys: %w", err)
			}

			// Serialize to JSON
			jsonData, err := json.MarshalIndent(store, "", "  ")
			if err != nil {
				for i := range passphrase {
					passphrase[i] = 0
				}
				return fmt.Errorf("failed to serialize API keys: %w", err)
			}

			// Encrypt with passphrase
			encrypted, err := credentials.EncryptWithPassphrase(jsonData, string(passphrase))
			// Zero out passphrase after use
			for i := range passphrase {
				passphrase[i] = 0
			}
			if err != nil {
				return fmt.Errorf("failed to encrypt with passphrase: %w", err)
			}

			// Write encrypted data atomically
			path, err := credentials.GetAPIKeysPath()
			if err != nil {
				return fmt.Errorf("failed to get API keys path: %w", err)
			}

			if err := atomicWriteFile(path, encrypted, 0600); err != nil {
				return err
			}

			fmt.Println("API keys encrypted with passphrase successfully.")
			fmt.Println("Note: You will need to enter the passphrase each time you use ledit.")
			fmt.Println("      Consider using machine key mode for convenience, or use LEDIT_KEY_PASSPHRASE env var.")
		} else {
			// Machine key mode - just ensure the key exists
			_, err := credentials.LoadOrCreateMachineKey()
			if err != nil {
				return fmt.Errorf("failed to setup machine key: %w", err)
			}

			// Re-save all existing keys to encrypt them
			store, err := credentials.Load()
			if err != nil {
				return fmt.Errorf("failed to load API keys: %w", err)
			}

			if err := credentials.Save(store); err != nil {
				return fmt.Errorf("failed to save encrypted API keys: %w", err)
			}

			fmt.Println("API keys encrypted with machine key successfully.")
		}

		return nil
	},
}

// decryptCmd decrypts to plaintext
var decryptCmd = &cobra.Command{
	Use:   "decrypt",
	Short: "Decrypt API keys to plaintext",
	Long: `Decrypt your API keys and save them as plaintext JSON.

WARNING: This will store your API keys in unencrypted format.
Only use this for migration or export purposes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := credentials.GetAPIKeysPath()
		if err != nil {
			return fmt.Errorf("failed to get API keys path: %w", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read API keys file: %w", err)
		}

		// Decrypt
		decrypted, err := credentials.DecryptStore(data)
		if err != nil {
			return fmt.Errorf("failed to decrypt API keys: %w", err)
		}

		// Write plaintext atomically
		if err := atomicWriteFile(path, decrypted, 0600); err != nil {
			return err
		}

		fmt.Println("API keys decrypted to plaintext successfully.")
		fmt.Println("WARNING: Your API keys are now stored in unencrypted format.")
		return nil
	},
}

// migrateCmd migrates plaintext to encrypted
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate plaintext API keys to encrypted format",
	Long: `Convert existing plaintext API keys to encrypted format.

This command will:
1. Generate a machine key if one doesn't exist
2. Encrypt your existing API keys
3. Replace the plaintext file with the encrypted version`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Ensure machine key exists
		_, err := credentials.LoadOrCreateMachineKey()
		if err != nil {
			return fmt.Errorf("failed to setup machine key: %w", err)
		}

		// Load existing keys (will auto-detect plaintext)
		store, err := credentials.Load()
		if err != nil {
			return fmt.Errorf("failed to load API keys: %w", err)
		}

		// Check if already encrypted
		status, err := credentials.CheckEncryptionStatus()
		if err != nil {
			return fmt.Errorf("failed to check status: %w", err)
		}

		if status.Encrypted {
			fmt.Println("API keys are already encrypted. No migration needed.")
			return nil
		}

		// Save will encrypt
		if err := credentials.Save(store); err != nil {
			return fmt.Errorf("failed to save encrypted API keys: %w", err)
		}

		fmt.Println("API keys migrated to encrypted format successfully.")
		return nil
	},
}

// rotateCmd generates a new machine key
var rotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Generate a new machine key and re-encrypt all keys",
	Long: `Generate a new machine-specific encryption key and re-encrypt all API keys.

WARNING: This will make your existing encrypted keys inaccessible unless
you have a backup of the old key or have exported your keys.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get old key path
		oldKeyPath, err := credentials.GetMachineKeyPath()
		if err != nil {
			return fmt.Errorf("failed to get machine key path: %w", err)
		}

		// Read existing key first — abort if unavailable (nothing to rotate)
		oldKeyData, err := os.ReadFile(oldKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read existing machine key (nothing to rotate): %w", err)
		}

		// Backup old key
		backupPath := fmt.Sprintf("%s.backup.%d", oldKeyPath, time.Now().Unix())
		if err := os.WriteFile(backupPath, oldKeyData, 0600); err != nil {
			return fmt.Errorf("failed to backup old key: %w", err)
		}
		fmt.Printf("Old machine key backed up to: %s\n", backupPath)

		// Read the encrypted API keys file BEFORE deleting the old key
		apiKeysPath, err := credentials.GetAPIKeysPath()
		if err != nil {
			return fmt.Errorf("failed to get API keys path: %w", err)
		}
		encryptedData, err := os.ReadFile(apiKeysPath)
		if err != nil {
			return fmt.Errorf("failed to read API keys file: %w", err)
		}

		// Check if the API keys file is plaintext (not yet encrypted)
		// Do this BEFORE deleting the old key to avoid unnecessary key deletion.
		if credentials.IsPlaintextJSON(encryptedData) {
			return fmt.Errorf("API keys file is not yet encrypted — run 'ledit keys migrate' to encrypt it first")
		}

		// Delete old key
		if err := os.Remove(oldKeyPath); err != nil {
			return fmt.Errorf("failed to remove old machine key: %w", err)
		}

		// Decrypt using the old key (load from memory)
		identity, err := age.ParseX25519Identity(string(oldKeyData))
		if err != nil {
			return fmt.Errorf("failed to parse old machine key: %w", err)
		}
		r, err := age.Decrypt(bytes.NewReader(encryptedData), identity)
		if err != nil {
			return fmt.Errorf("failed to decrypt with old key: %w", err)
		}
		decryptedData, err := io.ReadAll(io.LimitReader(r, credentials.MaxDecryptedSize))
		if err != nil {
			return fmt.Errorf("failed to read decrypted data: %w", err)
		}

		// Parse the decrypted JSON into a store
		var store credentials.Store
		if err := json.Unmarshal(decryptedData, &store); err != nil {
			return fmt.Errorf("failed to parse API keys: %w", err)
		}

		// Generate new key and re-encrypt
		if err := credentials.Save(store); err != nil {
			return fmt.Errorf("failed to save with new key: %w", err)
		}

		fmt.Println("Machine key rotated successfully.")
		fmt.Println("Your API keys have been re-encrypted with the new key.")
		fmt.Printf("Note: Delete the backup at %s when you no longer need the old key.\n", backupPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(keysCmd)

	// status subcommand
	keysCmd.AddCommand(statusCmd)

	// encrypt subcommand
	keysCmd.AddCommand(encryptCmd)
	encryptCmd.Flags().Bool("passphrase", false, "Use passphrase mode instead of machine key")

	// decrypt subcommand
	keysCmd.AddCommand(decryptCmd)

	// migrate subcommand
	keysCmd.AddCommand(migrateCmd)

	// rotate subcommand
	keysCmd.AddCommand(rotateCmd)
}

// readPassword reads a password from stdin without echoing
func readPassword() ([]byte, error) {
	fmt.Print("> ")
	passphrase, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Add newline after password input
	return passphrase, err
}

// validatePassphrase checks if the passphrase meets minimum requirements:
// - At least 8 characters
// - Contains at least one uppercase letter
// - Contains at least one lowercase letter
// - Contains at least one digit
func validatePassphrase(passphrase string) error {
	if len(passphrase) < 8 {
		return fmt.Errorf("passphrase must be at least 8 characters long")
	}

	// Check for at least one uppercase letter
	upperRegex := regexp.MustCompile(`[A-Z]`)
	if !upperRegex.MatchString(passphrase) {
		return fmt.Errorf("passphrase must contain at least one uppercase letter")
	}

	// Check for at least one lowercase letter
	lowerRegex := regexp.MustCompile(`[a-z]`)
	if !lowerRegex.MatchString(passphrase) {
		return fmt.Errorf("passphrase must contain at least one lowercase letter")
	}

	// Check for at least one digit
	digitRegex := regexp.MustCompile(`[0-9]`)
	if !digitRegex.MatchString(passphrase) {
		return fmt.Errorf("passphrase must contain at least one digit")
	}

	return nil
}

// atomicWriteFile writes data to a file atomically using a temp file + rename pattern.
// This prevents data corruption if the process crashes during the write.
// The file is created with the specified permissions.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".tmp-*.ledit")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if err := os.Chmod(tmpPath, perm); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set permissions on temp file: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to replace API keys file: %w", err)
	}
	return nil
}
