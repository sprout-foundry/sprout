package configuration

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// NOTE: Tests in this file modify os.Stdin globally and must NOT be run with t.Parallel()

func TestSelectProviderNonInteractive(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() (func(), error)
		wantErr bool
		errMsg  string
	}{
		{
			name: "non-interactive stdin returns error",
			setup: func() (func(), error) {
				// Create a pipe and replace stdin with the read end
				r, w, err := os.Pipe()
				if err != nil {
					return nil, fmt.Errorf("failed to create pipe: %w", err)
				}
				oldStdin := os.Stdin
				os.Stdin = r

				// Close the write end immediately to simulate non-interactive
				if err := w.Close(); err != nil {
					return nil, fmt.Errorf("failed to close write end: %w", err)
				}

				cleanup := func() {
					os.Stdin = oldStdin
					r.Close()
				}
				return cleanup, nil
			},
			wantErr: true,
			errMsg:  "no provider configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup, err := tt.setup()
			if err != nil {
				t.Fatalf("setup failed: %v", err)
			}
			defer cleanup()

			// Create a minimal APIKeys object for the test
			apiKeys := &APIKeys{}

			_, err = SelectProvider("", apiKeys)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errMsg != "" {
				errStr := err.Error()
				if !strings.Contains(errStr, tt.errMsg) {
					t.Errorf("SelectProvider() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestEnsureProviderAPIKeyNonInteractive(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() (func(), error)
		wantErr bool
		errMsg  string
	}{
		{
			name: "non-interactive stdin returns error for provider requiring API key",
			setup: func() (func(), error) {
				// Create a pipe and replace stdin with the read end
				r, w, err := os.Pipe()
				if err != nil {
					return nil, fmt.Errorf("failed to create pipe: %w", err)
				}
				oldStdin := os.Stdin
				os.Stdin = r

				// Close the write end immediately to simulate non-interactive
				if err := w.Close(); err != nil {
					return nil, fmt.Errorf("failed to close write end: %w", err)
				}

				cleanup := func() {
					os.Stdin = oldStdin
					r.Close()
				}
				return cleanup, nil
			},
			wantErr: true,
			errMsg:  "non-interactive mode",
		},
	}

	// Ensure no host-side credential satisfies HasProviderAuth("openai") —
	// the previous version of this test was tolerant of "credential
	// exists", which made it pass locally on dev machines and fail in
	// CI where no env var is set. Clearing the env unconditionally makes
	// the assertion meaningful in both environments.
	t.Setenv("OPENAI_API_KEY", "")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup, err := tt.setup()
			if err != nil {
				t.Fatalf("setup failed: %v", err)
			}
			defer cleanup()

			// Skip when a non-env credential is present (e.g. ~/.config
			// store on a dev workstation). The behavior is still
			// correct — just untested here — and forcing it to fail
			// would block local runs.
			if HasProviderAuth("openai") {
				t.Skip("openai credential is configured locally; non-interactive failure path not reachable")
			}

			// Create a minimal APIKeys object for the test
			apiKeys := &APIKeys{}

			err = EnsureProviderAPIKey("openai", apiKeys)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureProviderAPIKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errMsg != "" {
				errStr := err.Error()
				if !strings.Contains(errStr, tt.errMsg) {
					t.Errorf("EnsureProviderAPIKey() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestSelectInitialProviderNonInteractive(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() (func(), error)
		wantErr bool
		errMsg  string
	}{
		{
			name: "non-interactive stdin returns error",
			setup: func() (func(), error) {
				// Create a pipe and replace stdin with the read end
				r, w, err := os.Pipe()
				if err != nil {
					return nil, fmt.Errorf("failed to create pipe: %w", err)
				}
				oldStdin := os.Stdin
				os.Stdin = r

				// Close the write end immediately to simulate non-interactive
				if err := w.Close(); err != nil {
					return nil, fmt.Errorf("failed to close write end: %w", err)
				}

				cleanup := func() {
					os.Stdin = oldStdin
					r.Close()
				}
				return cleanup, nil
			},
			wantErr: true,
			errMsg:  "no provider configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup, err := tt.setup()
			if err != nil {
				t.Fatalf("setup failed: %v", err)
			}
			defer cleanup()

			// Create a minimal APIKeys object for the test
			apiKeys := &APIKeys{}

			_, err = selectInitialProvider(apiKeys, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("selectInitialProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errMsg != "" {
				errStr := err.Error()
				if !strings.Contains(errStr, tt.errMsg) {
					t.Errorf("selectInitialProvider() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}
