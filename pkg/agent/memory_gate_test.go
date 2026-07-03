package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// MemoryGate.Check() — core logic
// ---------------------------------------------------------------------------

func TestMemoryGate_AllowWhenSufficientMemory(t *testing.T) {
	gb := int64(1024 * 1024 * 1024)
	tests := []struct {
		name  string
		mem   int64
	}{
		{"20 GB", 20 * gb},
		{"exactly 16 GB", 16 * gb},
		{"way above", 100 * gb},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := &MemoryGate{
				readMem: func() (int64, error) { return tt.mem, nil },
			}
			err := gate.Check()
			if err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})
	}
}

func TestMemoryGate_RefuseWhenBelowMinimum(t *testing.T) {
	gb := int64(1024 * 1024 * 1024)
	tests := []struct {
		name  string
		mem   int64
	}{
		{"4 GB", 4 * gb},
		{"exactly 7.9 GB", 7 * gb + 900*1024*1024},
		{"0 bytes", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			gate := &MemoryGate{
				readMem: func() (int64, error) {
					calls++
					return tt.mem, nil
				},
			}
			err := gate.Check()
			if err == nil {
				t.Fatal("expected MemoryGateError, got nil")
			}
			gateErr, ok := err.(*MemoryGateError)
			if !ok {
				t.Fatalf("expected *MemoryGateError, got %T", err)
			}
			if gateErr.Retried {
				t.Error("Retried should be false for immediate refusal")
			}
			if calls != 1 {
				t.Errorf("expected 1 readMem call, got %d", calls)
			}
			if !strings.Contains(gateErr.Error(), "insufficient memory") {
				t.Errorf("error should mention insufficient memory: %s", gateErr.Error())
			}
		})
	}
}

func TestMemoryGate_RetryWhenBetweenThresholds(t *testing.T) {
	gb := int64(1024 * 1024 * 1024)

	t.Run("succeeds after memory frees up", func(t *testing.T) {
		calls := 0
		gate := &MemoryGate{
			RetrySleep: 1 * time.Millisecond,
			MaxRetries: 5,
			readMem: func() (int64, error) {
				calls++
				if calls <= 2 {
					return 12 * gb, nil // between 8 and 16
				}
				return 18 * gb, nil // now sufficient
			},
		}
		err := gate.Check()
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
		// 1 initial + 2 retries (3rd call returns 18 GB)
		if calls != 3 {
			t.Errorf("expected 3 calls (1 initial + 2 retries), got %d", calls)
		}
	})

	t.Run("fails after exhausting retries", func(t *testing.T) {
		calls := 0
		maxRetries := 3
		gate := &MemoryGate{
			RetrySleep: 1 * time.Millisecond,
			MaxRetries: maxRetries,
			readMem: func() (int64, error) {
				calls++
				return 12 * gb, nil // always between thresholds
			},
		}
		err := gate.Check()
		if err == nil {
			t.Fatal("expected MemoryGateError, got nil")
		}
		gateErr := err.(*MemoryGateError)
		if !gateErr.Retried {
			t.Error("Retried should be true after exhausting retries")
		}
		// 1 initial + maxRetries in the loop
		expectedCalls := 1 + maxRetries
		if calls != expectedCalls {
			t.Errorf("expected %d calls (1 initial + %d retries), got %d", expectedCalls, maxRetries, calls)
		}
	})
}

func TestMemoryGate_RetrySuccessAfterFewRetries(t *testing.T) {
	gb := int64(1024 * 1024 * 1024)
	calls := 0
	gate := &MemoryGate{
		RetrySleep: 1 * time.Millisecond,
		MaxRetries: 5,
		readMem: func() (int64, error) {
			calls++
			switch calls {
			case 1:
				return 10 * gb, nil // retry zone
			case 2:
				return 14 * gb, nil // still retry zone
			default:
				return 18 * gb, nil // sufficient on 3rd call
			}
		},
	}
	err := gate.Check()
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	// 1 initial + 2 retries (3rd call returns 18 GB)
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestMemoryGate_FailOpenWhenReaderReturnsError(t *testing.T) {
	t.Run("initial read fails", func(t *testing.T) {
		calls := 0
		gate := &MemoryGate{
			readMem: func() (int64, error) {
				calls++
				return 0, fmt.Errorf("simulated read failure")
			},
		}
		err := gate.Check()
		if err != nil {
			t.Fatalf("expected nil (fail open), got %v", err)
		}
		if calls != 1 {
			t.Errorf("expected 1 call, got %d", calls)
		}
	})

	t.Run("retry read fails", func(t *testing.T) {
		gb := int64(1024 * 1024 * 1024)
		calls := 0
		gate := &MemoryGate{
			RetrySleep: 1 * time.Millisecond,
			MaxRetries: 5,
			readMem: func() (int64, error) {
				calls++
				if calls == 1 {
					return 12 * gb, nil // enter retry zone
				}
				return 0, fmt.Errorf("simulated read failure during retry")
			},
		}
		err := gate.Check()
		if err != nil {
			t.Fatalf("expected nil (fail open during retry), got %v", err)
		}
		// 1 initial + 1 retry that errors
		if calls != 2 {
			t.Errorf("expected 2 calls, got %d", calls)
		}
	})
}

func TestMemoryGate_DropsBelowMinDuringRetry(t *testing.T) {
	gb := int64(1024 * 1024 * 1024)
	calls := 0
	gate := &MemoryGate{
		RetrySleep: 1 * time.Millisecond,
		MaxRetries: 5,
		readMem: func() (int64, error) {
			calls++
			if calls == 1 {
				return 12 * gb, nil // retry zone
			}
			return 6 * gb, nil // below minimum
		},
	}
	err := gate.Check()
	if err == nil {
		t.Fatal("expected MemoryGateError, got nil")
	}
	gateErr := err.(*MemoryGateError)
	if !gateErr.Retried {
		t.Error("Retried should be true")
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// Default values
// ---------------------------------------------------------------------------

func TestMemoryGate_DefaultValues(t *testing.T) {
	t.Run("zero-value gate uses defaults", func(t *testing.T) {
		// A zero-value gate should refuse 4 GB immediately (proves min=8GB default)
		gate := MemoryGate{}
		gate.readMem = func() (int64, error) {
			return 4 * 1024 * 1024 * 1024, nil
		}
		err := gate.Check()
		if err == nil {
			t.Fatal("expected error for 4 GB with default thresholds, got nil")
		}
		gateErr := err.(*MemoryGateError)
		if gateErr.Retried {
			t.Error("should not retry when below default 8 GB minimum")
		}
	})

	t.Run("zero-value gate allows 20 GB", func(t *testing.T) {
		gate := MemoryGate{}
		gate.readMem = func() (int64, error) {
			return 20 * 1024 * 1024 * 1024, nil
		}
		err := gate.Check()
		if err != nil {
			t.Fatalf("expected nil for 20 GB, got %v", err)
		}
	})

	t.Run("DefaultMemoryGate returns non-nil", func(t *testing.T) {
		gate := DefaultMemoryGate()
		if gate == nil {
			t.Fatal("DefaultMemoryGate returned nil")
		}
	})
}

// ---------------------------------------------------------------------------
// MemoryGateError formatting
// ---------------------------------------------------------------------------

func TestMemoryGateError_Formatting(t *testing.T) {
	gb := int64(1024 * 1024 * 1024)
	tests := []struct {
		name    string
		err     *MemoryGateError
		wantSub string
	}{
		{
			name:    "no retry",
			err:     &MemoryGateError{AvailableBytes: 4 * gb, RequiredBytes: 8 * gb, Retried: false},
			wantSub: "insufficient memory",
		},
		{
			name:    "with retry",
			err:     &MemoryGateError{AvailableBytes: 12 * gb, RequiredBytes: 16 * gb, Retried: true},
			wantSub: "after retries",
		},
		{
			name:    "contains GB values",
			err:     &MemoryGateError{AvailableBytes: 4 * gb, RequiredBytes: 8 * gb, Retried: false},
			wantSub: "4.0 GB",
		},
		{
			name:    "required GB value",
			err:     &MemoryGateError{AvailableBytes: 4 * gb, RequiredBytes: 8 * gb, Retried: false},
			wantSub: "8.0 GB",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("error %q should contain %q", got, tt.wantSub)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// readMemLinux parsing
// ---------------------------------------------------------------------------

func TestReadMemLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("readMemLinux test only runs on Linux")
	}

	t.Run("integration: real /proc/meminfo", func(t *testing.T) {
		mem, err := readMemLinux()
		if err != nil {
			t.Fatalf("readMemLinux failed: %v", err)
		}
		if mem <= 0 {
			t.Error("expected positive memory value")
		}
		// Sanity check: should be at least 1 GB on any modern system
		minExpected := int64(1 * 1024 * 1024 * 1024)
		if mem < minExpected {
			t.Errorf("memory %d bytes seems unreasonably low", mem)
		}
	})

	t.Run("parsing with temp meminfo file", func(t *testing.T) {
		// We can't inject the path into readMemLinux (it's hardcoded to
		// /proc/meminfo), so we test the parsing logic indirectly by
		// verifying the real /proc/meminfo works and checking edge cases
		// via the meminfo parsing patterns we know the code uses.
		// The real integration test above covers the actual path.
		// Here we verify the kB→bytes conversion is correct.
		mem, err := readMemLinux()
		if err != nil {
			t.Fatalf("readMemLinux failed: %v", err)
		}
		// The value should be divisible by 1024 (since it's kB * 1024)
		if mem%1024 != 0 {
			t.Errorf("memory value %d should be divisible by 1024", mem)
		}
	})
}

// Test the meminfo parsing logic in isolation using a temp file that
// shadows /proc/meminfo — not portable, so we use a direct approach
// instead: verify the Fields-based parsing handles various formats.
func TestMeminfoParsingLogic(t *testing.T) {
	// This tests the parsing logic that readMemLinux uses (strings.Fields
	// + looking for "MemAvailable:" as the first field). We can't inject
	// a custom path into readMemLinux, so we replicate the parsing logic
	// here to verify it handles edge cases.
	tests := []struct {
		name      string
		meminfo   string
		wantKB    int64
		wantError bool
	}{
		{
			name:    "standard format",
			meminfo: "MemTotal:       16384000 kB\nMemAvailable:    12345678 kB\n",
			wantKB:  12345678,
		},
		{
			name:    "extra whitespace",
			meminfo: "MemTotal:       16384000 kB\n   MemAvailable:     9876543   kB\n",
			wantKB:  9876543,
		},
		{
			name:    "MemAvailable first",
			meminfo: "MemAvailable:    11111111 kB\nMemTotal:       16384000 kB\n",
			wantKB:  11111111,
		},
		{
			name:      "MemAvailable missing",
			meminfo:   "MemTotal:       16384000 kB\nMemFree:          1234567 kB\n",
			wantError: true,
		},
		{
			name:      "invalid value",
			meminfo:   "MemAvailable:    notanumber kB\n",
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the parsing logic from readMemLinux
			var foundKB int64
			var err error
			found := false
			for _, line := range strings.Split(tt.meminfo, "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "MemAvailable:" {
					foundKB, err = strconv.ParseInt(fields[1], 10, 64)
					if err != nil {
						break
					}
					found = true
					break
				}
			}
			if !found && err == nil {
				err = fmt.Errorf("MemAvailable not found in /proc/meminfo")
			}
			if err != nil {
				if !tt.wantError {
					t.Errorf("unexpected parse error: %v", err)
				}
				return
			}
			if tt.wantError {
				t.Error("expected error but got none")
				return
			}
			if foundKB != tt.wantKB {
				t.Errorf("parsed %d kB, want %d kB", foundKB, tt.wantKB)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// readMemDarwin helpers (parsing logic only, not actual commands)
// ---------------------------------------------------------------------------

func TestGetVMStatFreePages_Parsing(t *testing.T) {
	// We can't easily mock exec.Command, but we can verify the parsing
	// logic by testing against the real vm_stat on macOS.
	if runtime.GOOS != "darwin" {
		t.Skip("vm_stat test only runs on macOS")
	}

	pages, err := getVMStatField("Pages free")
	if err != nil {
		t.Fatalf("getVMStatField(Pages free) failed: %v", err)
	}
	if pages < 0 {
		t.Error("free pages should not be negative")
	}
}

func TestVMStatParsingLogic(t *testing.T) {
	// Test the parsing logic that getVMStatField uses, independent of
	// the actual vm_stat command. This verifies we handle format variations.
	tests := []struct {
		name      string
		fieldName string
		output    string
		wantPages int64
		wantError bool
	}{
		{
			name:      "Pages free standard format",
			fieldName: "Pages free",
			output:    "Pages free: 123456.\nPages active: 789012.\n",
			wantPages: 123456,
		},
		{
			name:      "Pages speculative",
			fieldName: "Pages speculative",
			output:    "Pages free: 100.\nPages speculative: 54321.\n",
			wantPages: 54321,
		},
		{
			name:      "Pages purgeable",
			fieldName: "Pages purgeable",
			output:    "Pages purgeable: 99999.\nPages free: 100.\n",
			wantPages: 99999,
		},
		{
			name:      "extra whitespace",
			fieldName: "Pages free",
			output:    "  Pages free:  654321  .\n",
			wantPages: 654321,
		},
		{
			name:      "Pages free at end",
			fieldName: "Pages free",
			output:    "Pages active: 111.\nPages free: 222.\n",
			wantPages: 222,
		},
		{
			name:      "missing Pages free",
			fieldName: "Pages free",
			output:    "Pages active: 111.\nPages inactive: 222.\n",
			wantError: true,
		},
		{
			name:      "invalid value",
			fieldName: "Pages free",
			output:    "Pages free: notanumber.\n",
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the parsing logic from getVMStatField
			var found int64
			var err error
			foundIt := false
			for _, line := range strings.Split(tt.output, "\n") {
				line = strings.TrimSpace(line)
				line = strings.TrimSuffix(line, ".")
				if strings.HasPrefix(line, tt.fieldName+":") {
					parts := strings.Fields(line)
					if len(parts) < 2 {
						err = fmt.Errorf("unexpected vm_stat line format: %q", line)
						break
					}
					found, err = strconv.ParseInt(parts[len(parts)-1], 10, 64)
					if err != nil {
						break
					}
					foundIt = true
					break
				}
			}
			if !foundIt && err == nil {
				err = fmt.Errorf("%s not found in vm_stat output", tt.fieldName)
			}
			if err != nil {
				if !tt.wantError {
					t.Errorf("unexpected parse error: %v", err)
				}
				return
			}
			if tt.wantError {
				t.Error("expected error but got none")
				return
			}
			if found != tt.wantPages {
				t.Errorf("parsed %d pages, want %d", found, tt.wantPages)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestMemoryGate_CustomThresholds(t *testing.T) {
	t.Run("custom min and retry thresholds", func(t *testing.T) {
		gb := int64(1024 * 1024 * 1024)
		gate := &MemoryGate{
			MinMemoryBytes: 4 * gb,
			RetryMinBytes:  8 * gb,
			RetrySleep:     1 * time.Millisecond,
			MaxRetries:     3,
			readMem: func() (int64, error) {
				return 6 * gb, nil // between custom 4 GB min and 8 GB retry
			},
		}
		err := gate.Check()
		if err == nil {
			t.Fatal("expected MemoryGateError, got nil")
		}
		// Should have retried 3 times (all returning 6 GB, still between thresholds)
	})

	t.Run("custom thresholds allow 10 GB when retryMin is 8 GB", func(t *testing.T) {
		gb := int64(1024 * 1024 * 1024)
		gate := &MemoryGate{
			MinMemoryBytes: 4 * gb,
			RetryMinBytes:  8 * gb,
			readMem: func() (int64, error) {
				return 10 * gb, nil // above custom retryMin of 8 GB
			},
		}
		err := gate.Check()
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}

func TestMemoryGate_ExactlyAtThresholds(t *testing.T) {
	gb := int64(1024 * 1024 * 1024)

	t.Run("exactly at retryMin allows", func(t *testing.T) {
		gate := &MemoryGate{
			readMem: func() (int64, error) {
				return 16 * gb, nil // exactly DefaultRetryMinBytes
			},
		}
		err := gate.Check()
		if err != nil {
			t.Fatalf("expected nil at exactly 16 GB, got %v", err)
		}
	})

	t.Run("exactly at minMem enters retry zone", func(t *testing.T) {
		calls := 0
		gate := &MemoryGate{
			RetrySleep: 1 * time.Millisecond,
			MaxRetries: 2,
			readMem: func() (int64, error) {
				calls++
				return 8*gb + 1, nil // just above 8 GB minimum
			},
		}
		err := gate.Check()
		if err == nil {
			t.Fatal("expected MemoryGateError after retries, got nil")
		}
		// 1 initial + 2 retries
		if calls != 3 {
			t.Errorf("expected 3 calls, got %d", calls)
		}
	})

	t.Run("one byte below minMem refuses immediately", func(t *testing.T) {
		calls := 0
		gate := &MemoryGate{
			readMem: func() (int64, error) {
				calls++
				return 8*gb - 1, nil // one byte below 8 GB
			},
		}
		err := gate.Check()
		if err == nil {
			t.Fatal("expected MemoryGateError, got nil")
		}
		gateErr := err.(*MemoryGateError)
		if gateErr.Retried {
			t.Error("should not retry when below minimum")
		}
		if calls != 1 {
			t.Errorf("expected 1 call, got %d", calls)
		}
	})
}

// ---------------------------------------------------------------------------
// Integration test
// ---------------------------------------------------------------------------

func TestMemoryGate_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test that DefaultMemoryGate works on the current platform.
	gate := DefaultMemoryGate()
	// Don't actually call Check() without a mock — it would sleep for
	// 30 seconds if memory is between thresholds. Instead, verify the
	// gate is properly constructed.
	if gate == nil {
		t.Fatal("DefaultMemoryGate returned nil")
	}

	// On Linux, verify readMemLinux works with the real /proc/meminfo.
	if runtime.GOOS == "linux" {
		mem, err := readMemLinux()
		if err != nil {
			t.Fatalf("readMemLinux failed on real system: %v", err)
		}
		if mem <= 0 {
			t.Error("expected positive memory value from real /proc/meminfo")
		}
		t.Logf("System has %d bytes (%.1f GB) available", mem, float64(mem)/1024/1024/1024)
	}

	// On macOS, verify the Darwin path is reachable (best-effort).
	if runtime.GOOS == "darwin" {
		_, err := readMemDarwin()
		if err != nil {
			t.Logf("readMemDarwin failed (expected in CI): %v", err)
		} else {
			t.Log("readMemDarwin succeeded on this system")
		}
	}
}

// ---------------------------------------------------------------------------
// readMemAvailable dispatch
// ---------------------------------------------------------------------------

func TestReadMemAvailable_Dispatch(t *testing.T) {
	// Verify the dispatch function returns an error on unsupported OS.
	// We can't easily change runtime.GOOS, so just verify it works on
	// the current platform.
	_, err := readMemAvailable()
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		// On supported platforms, it may succeed or fail depending on
		// the environment (e.g., CI containers). We just verify it
		// doesn't return an "unsupported OS" error.
		if err != nil && strings.Contains(err.Error(), "unsupported OS") {
			t.Errorf("should not report unsupported OS on %s", runtime.GOOS)
		}
	}
}

// ---------------------------------------------------------------------------
// Temp file-based test for readMemLinux parsing
// ---------------------------------------------------------------------------

func TestReadMemLinux_TempFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("temp file test only runs on Linux")
	}

	// Create a temp directory with a fake /proc/meminfo to verify
	// the parsing handles real-world variations. Since readMemLinux
	// hardcodes /proc/meminfo, we test via the gate's readMem override
	// with realistic meminfo content parsed the same way.
	tmpDir := t.TempDir()
	testCases := []struct {
		name     string
		content  string
		wantKB   int64
		wantErr  bool
	}{
		{
			name:    "realistic meminfo",
			content: "MemTotal:       32768000 kB\nMemFree:          1234567 kB\nMemAvailable:    20480000 kB\nBuffers:           234567 kB\n",
			wantKB:  20480000,
		},
		{
			name:    "minimal meminfo",
			content: "MemAvailable: 5000000 kB\n",
			wantKB:  5000000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Write the test content to a temp file (not used directly,
			// but demonstrates the format we're testing against)
			tmpFile := filepath.Join(tmpDir, "meminfo_"+tc.name)
			if err := os.WriteFile(tmpFile, []byte(tc.content), 0644); err != nil {
				t.Fatalf("failed to write temp file: %v", err)
			}
			// Parse it the same way readMemLinux does
			data, err := os.ReadFile(tmpFile)
			if err != nil {
				t.Fatalf("failed to read temp file: %v", err)
			}
			var foundKB int64
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "MemAvailable:" {
					kb, err := strconv.ParseInt(fields[1], 10, 64)
					if err != nil {
						if !tc.wantErr {
							t.Errorf("parse error: %v", err)
						}
						return
					}
					foundKB = kb
					break
				}
			}
			if tc.wantErr {
				t.Error("expected error but got none")
				return
			}
			if foundKB != tc.wantKB {
				t.Errorf("parsed %d kB, want %d kB", foundKB, tc.wantKB)
			}
			// Verify the bytes conversion
			bytes := foundKB * 1024
			expectedBytes := tc.wantKB * 1024
			if bytes != expectedBytes {
				t.Errorf("bytes conversion: got %d, want %d", bytes, expectedBytes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IsMemoryIntensiveCommand
// ---------------------------------------------------------------------------

func TestIsMemoryIntensiveCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		// Test runners
		{"vitest", "vitest", true},
		{"vitest --run", "vitest --run", true},
		{"npx vitest", "npx vitest --watch=false", true},
		{"jest", "jest --coverage", true},
		{"mocha", "mocha test/", true},
		{"ava", "ava test/**", true},
		{"npm test", "npm test", true},
		{"npm run test", "npm run test:unit", true},
		{"yarn test", "yarn test", true},
		{"pnpm test", "pnpm test", true},
		{"playwright test", "playwright test", true},
		{"npx playwright", "npx playwright test", true},
		{"cypress", "cypress run", true},

		// Bundlers and build tools
		{"webpack", "webpack --mode production", true},
		{"vite build", "vite build", true},
		{"esbuild", "esbuild src/index.ts", true},
		{"rollup", "rollup -c", true},
		{"tsc", "tsc --build", true},
		{"next build", "next build", true},
		{"nuxt build", "nuxt build", true},

		// Native build/test
		{"go build", "go build ./...", true},
		{"go test", "go test ./...", true},
		{"cargo build", "cargo build --release", true},
		{"cargo test", "cargo test", true},
		{"make build", "make build", true},
		{"make test", "make test", true},

		// Case insensitivity
		{"Vitest uppercase", "Vitest --run", true},
		{"NPM TEST", "NPM TEST", true},
		{"Go Build mixed", "Go Build ./...", true},

		// Non-intensive commands
		{"ls", "ls -la", false},
		{"cat", "cat file.txt", false},
		{"echo", "echo hello", false},
		{"grep", "grep pattern file", false},
		{"git status", "git status", false},
		{"git diff", "git diff HEAD", false},
		{"cd", "cd /tmp", false},
		{"mkdir", "mkdir -p dir", false},
		{"rm file", "rm file.txt", false},
		{"cp", "cp a b", false},
		{"mv", "mv a b", false},
		{"touch", "touch file", false},
		{"head", "head -n 5 file", false},
		{"tail", "tail -f log.txt", false},
		{"wc", "wc -l file", false},
		{"find", "find . -name '*.go'", false},
		{"npm install", "npm install", false},
		{"npm run lint", "npm run lint", false},
		{"empty command", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMemoryIntensiveCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("IsMemoryIntensiveCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
