package agent

import (
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// A patch op missing its path must be rejected at parse time. fmt.Sprint(nil)
// is "<nil>", so reading the field that way made the empty-path check pass and
// the op failed later with a confusing "invalid patch path" error.
func TestParsePatchOperationsRejectsAMissingPath(t *testing.T) {
	_, err := parsePatchOperations([]interface{}{
		map[string]interface{}{"op": "add", "value": 1},
	})
	if err == nil {
		t.Fatal("an op with no path was accepted")
	}
	if !agenterrors.IsInvalidInput(err) {
		t.Errorf("error is not classified as invalid input: %v", err)
	}
}

// A non-string path is equally unusable and must not be stringified into one.
func TestParsePatchOperationsRejectsANonStringPath(t *testing.T) {
	_, err := parsePatchOperations([]interface{}{
		map[string]interface{}{"op": "add", "path": 42, "value": 1},
	})
	if err == nil {
		t.Fatal("an op with a numeric path was accepted")
	}
	if !agenterrors.IsInvalidInput(err) {
		t.Errorf("error is not classified as invalid input: %v", err)
	}
}

// The typed errors are what keep the retry loop from re-sending bad input.
func TestParseJSONPointerErrorsAreClassified(t *testing.T) {
	for _, bad := range []string{"", "no-leading-slash"} {
		if _, err := parseJSONPointer(bad); err == nil {
			t.Errorf("parseJSONPointer(%q) accepted an invalid pointer", bad)
		} else if !agenterrors.IsInvalidInput(err) {
			t.Errorf("parseJSONPointer(%q) error is not classified as invalid input: %v", bad, err)
		}
	}
}
