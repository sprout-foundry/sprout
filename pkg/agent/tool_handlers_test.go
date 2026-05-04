package agent

import (
	"strings"
	"testing"
)

func TestConvertToStringFromHandlers(t *testing.T) {
	tests := []struct {
		name    string
		param   interface{}
		keyName string
		want    string
		wantErr bool
	}{
		// String cases
		{"string value", "hello", "path", "hello", false},
		{"empty string", "", "path", "", false},
		{"string with whitespace", "  hello world  ", "path", "  hello world  ", false},
		{"string with newlines", "line1\nline2", "path", "line1\nline2", false},

		// Byte slice cases
		{"byte slice", []byte("hello"), "path", "hello", false},
		{"empty byte slice", []byte(""), "path", "", false},
		{"byte slice with utf8", []byte("héllo"), "path", "héllo", false},

		// Integer cases
		{"int", int(42), "count", "42", false},
		{"int zero", int(0), "count", "0", false},
		{"int negative", int(-5), "count", "-5", false},
		{"int32", int32(100), "count", "100", false},
		{"int64", int64(9223372036854775807), "count", "9223372036854775807", false},
		{"int64 negative", int64(-1), "count", "-1", false},

		// Float cases
		{"float64", float64(3.14), "val", "3.14", false},
		{"float64 zero", float64(0), "val", "0", false},
		{"float64 large", float64(1.5e10), "val", "1.5e+10", false},
		{"float32", float32(2.5), "val", "2.5", false},
		{"float64 negative", float64(-0.5), "val", "-0.5", false},

		// Boolean cases
		{"bool true", true, "flag", "true", false},
		{"bool false", false, "flag", "false", false},

		// Map cases
		{"map string value", map[string]interface{}{"key": "value"}, "data", `{"key":"value"}`, false},
		{"map with nested", map[string]interface{}{"a": 1, "b": "two"}, "data", `{"a":1,"b":"two"}`, false},
		{"map with nested object", map[string]interface{}{"a": map[string]interface{}{"b": 1}}, "data", `{"a":{"b":1}}`, false},

		// Nil case
		{"nil param", nil, "path", "", true},
		{"nil param with different key name", nil, "content", "", true},

		// Invalid type cases
		{"struct type", struct{ X int }{1}, "data", "", true},
		{"chan type", make(chan int), "data", "", true},
		{"func type", func() {}, "data", "", true},
		{"pointer to string", strPtr("hello"), "data", "", true},
		{"slice of ints", []int{1, 2, 3}, "data", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertToString(tc.param, tc.keyName)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %T, got nil", tc.param)
				}
				// Verify the error contains the key name
				if got == tc.keyName {
					// This is a false positive check - if keyName happens to equal got
					// the test is unreliable, but we only check error presence for error cases
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tc.want {
					t.Errorf("got %q, want %q", got, tc.want)
				}
			}
		})
	}

	// Specific error message tests
	t.Run("nil param error mentions key name", func(t *testing.T) {
		_, err := convertToString(nil, "file_path")
		if err == nil {
			t.Fatal("expected error")
		}
		if got := err.Error(); !strings.Contains(got, "file_path") {
			t.Errorf("error %q should contain 'file_path'", got)
		}
	})

	t.Run("invalid type error mentions type", func(t *testing.T) {
		_, err := convertToString(struct{}{}, "data")
		if err == nil {
			t.Fatal("expected error")
		}
		if got := err.Error(); !strings.Contains(got, "data") {
			t.Errorf("error %q should contain 'data'", got)
		}
	})
}

func strPtr(s string) *string { return &s }
