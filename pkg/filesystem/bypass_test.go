package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeResolvePathWithBypass(t *testing.T) {
	// Save current working directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a test directory in the user's home directory (not /tmp to avoid the /tmp exception)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}
	testDir := filepath.Join(homeDir, ".ledit-test-security")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	// Change to test directory
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to chdir to test dir: %v", err)
	}

	// Create a test file in a sibling directory (outside working directory)
	siblingDir := filepath.Join(homeDir, ".ledit-test-sibling")
	if err := os.MkdirAll(siblingDir, 0755); err != nil {
		t.Fatalf("Failed to create sibling dir: %v", err)
	}
	defer os.RemoveAll(siblingDir)
	siblingFile := filepath.Join(siblingDir, "sibling-file.txt")
	if err := os.WriteFile(siblingFile, []byte("sibling content"), 0644); err != nil {
		t.Fatalf("Failed to create sibling file: %v", err)
	}

	tests := []struct {
		name          string
		ctx           context.Context
		path          string
		wantErr       bool
		errorContains string
	}{
		{
			name:          "normal ctx - path outside working dir blocked",
			ctx:           context.Background(),
			path:          siblingFile,
			wantErr:       true,
			errorContains: "file access outside working directory",
		},
		{
			name:    "bypass ctx - path outside working dir allowed",
			ctx:     WithSecurityBypass(context.Background()),
			path:    siblingFile,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeResolvePathWithBypass(tt.ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePathWithBypass() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errorContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("SafeResolvePathWithBypass() error = %v, expected to contain %q", err, tt.errorContains)
				}
			}
			if !tt.wantErr && got == "" {
				t.Errorf("SafeResolvePathWithBypass() returned empty path when no error expected")
			}
		})
	}
}

func TestSafeResolvePathForWriteWithBypass(t *testing.T) {
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Create a test directory in the user's home directory (not /tmp to avoid the /tmp exception)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home dir: %v", err)
	}
	testDir := filepath.Join(homeDir, ".ledit-test-write")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to chdir to test dir: %v", err)
	}

	siblingDir := filepath.Join(homeDir, ".ledit-test-write-sibling")
	if err := os.MkdirAll(siblingDir, 0755); err != nil {
		t.Fatalf("Failed to create sibling write dir: %v", err)
	}
	defer os.RemoveAll(siblingDir)

	tests := []struct {
		name          string
		ctx           context.Context
		path          string
		wantErr       bool
		errorContains string
	}{
		{
			name:          "normal ctx - write outside working dir blocked",
			ctx:           context.Background(),
			path:          filepath.Join(siblingDir, "newfile.txt"),
			wantErr:       true,
			errorContains: "file write outside working directory",
		},
		{
			name:    "bypass ctx - write outside working dir allowed",
			ctx:     WithSecurityBypass(context.Background()),
			path:    filepath.Join(siblingDir, "newfile.txt"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeResolvePathForWriteWithBypass(tt.ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeResolvePathForWriteWithBypass() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errorContains != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("SafeResolvePathForWriteWithBypass() error = %v, expected to contain %q", err, tt.errorContains)
				}
			}
			if !tt.wantErr && got == "" {
				t.Errorf("SafeResolvePathForWriteWithBypass() returned empty path when no error expected")
			}
		})
	}
}

func TestSecurityBypassEnabled(t *testing.T) {
	ctx1 := context.Background()
	if SecurityBypassEnabled(ctx1) {
		t.Error("SecurityBypassEnabled returned true for normal context")
	}

	ctx2 := WithSecurityBypass(context.Background())
	if !SecurityBypassEnabled(ctx2) {
		t.Error("SecurityBypassEnabled returned false for bypass context")
	}
}

func TestWithSecurityBypass(t *testing.T) {
	ctx := context.Background()
	bypassCtx := WithSecurityBypass(ctx)

	if SecurityBypassEnabled(ctx) {
		t.Error("Original context should not have bypass enabled")
	}

	if !SecurityBypassEnabled(bypassCtx) {
		t.Error("Bypass context should have bypass enabled")
	}
}
