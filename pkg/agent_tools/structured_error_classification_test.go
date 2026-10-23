package tools

import (
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Tool arguments arrive from the model, so a malformed patch is a bad input,
// not a transient failure. The retry loop treats an untyped error as retryable
// and re-sends the identical request, so these must stay classified as invalid
// input: it fails the turn immediately instead.
func TestMalformedPatchInputIsClassifiedAsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		call func() error
	}{
		{"patch_ops is not an array", func() error {
			_, err := parsePatchOperations("not an array")
			return err
		}},
		{"patch op is missing a path", func() error {
			_, err := parsePatchOperations([]interface{}{
				map[string]interface{}{"op": "add", "value": 1},
			})
			return err
		}},
		{"patch op is unsupported", func() error {
			_, err := parsePatchOperations([]interface{}{
				map[string]interface{}{"op": "frobnicate", "path": "/a"},
			})
			return err
		}},
		{"pointer has no leading slash", func() error {
			_, err := parseJSONPointer("no-leading-slash")
			return err
		}},
		{"pointer is empty", func() error {
			_, err := parseJSONPointer("")
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !agenterrors.IsInvalidInput(err) {
				t.Errorf("error is not classified as invalid input, so the retry loop will retry it: %v", err)
			}
		})
	}
}
