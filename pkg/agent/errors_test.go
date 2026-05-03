package agent

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrProviderStartupClosed(t *testing.T) {
	// Verify the sentinel error has the expected message.
	if errProviderStartupClosed.Error() != "provider startup canceled by user" {
		t.Errorf("unexpected error message: got %q", errProviderStartupClosed.Error())
	}

	// Verify it works with errors.Is().
	wrapped := fmt.Errorf("wrapped: %w", errProviderStartupClosed)
	if !errors.Is(wrapped, errProviderStartupClosed) {
		t.Error("errors.Is() should match wrapped sentinel error")
	}
}
