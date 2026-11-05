package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// The retry notice is the only thing a user sees while a provider request is
// being retried, so it must lead with what broke rather than with the layers
// of error wrapping. A raw chain spends its budget on repeated "[network]"
// prefixes and the local ephemeral port, pushing the leaf cause past the
// truncation point.
func TestRetryNoticeMessageKeepsTheLeafCause(t *testing.T) {
	leaf := errors.New("read tcp 192.168.1.134:43440->104.21.37.56:443: read: connection reset by peer")
	err := agenterrors.NewNetwork("chat request failed (streaming)",
		agenterrors.NewNetwork("failed to read streaming response", leaf))

	got := retryNoticeMessage(err)

	if !strings.Contains(got, "connection reset by peer") {
		t.Errorf("notice dropped the leaf cause; got %q", got)
	}
	if strings.Contains(got, "192.168.1.134") {
		t.Errorf("notice kept the local address, which identifies nothing; got %q", got)
	}
	if !strings.Contains(got, "104.21.37.56") {
		t.Errorf("notice dropped the remote address, which identifies the peer; got %q", got)
	}
	// Stripping the local half must not weld the protocol word onto the
	// remote address ("tcp104.21.37.56").
	if !strings.Contains(got, "tcp 104.21.37.56:443") {
		t.Errorf("notice mangled the address; want %q, got %q", "tcp 104.21.37.56:443", got)
	}
	if len(got) > retryNoticeMaxLen {
		t.Errorf("notice is %d chars, want <= %d", len(got), retryNoticeMaxLen)
	}
}

// A long chain must still fit, and truncation must not eat the leaf cause.
func TestRetryNoticeMessageTruncatesWithoutLosingTheCause(t *testing.T) {
	leaf := errors.New("connection reset by peer")
	err := agenterrors.NewNetwork(strings.Repeat("very long outer message ", 20),
		agenterrors.NewNetwork(strings.Repeat("middle ", 20), leaf))

	got := retryNoticeMessage(err)

	if len(got) > retryNoticeMaxLen {
		t.Errorf("notice is %d chars, want <= %d", len(got), retryNoticeMaxLen)
	}
	if !strings.Contains(got, "connection reset by peer") {
		t.Errorf("truncation dropped the cause; got %q", got)
	}
}

func TestRetryNoticeMessageHandlesPlainAndNilErrors(t *testing.T) {
	if got := retryNoticeMessage(nil); got != "" {
		t.Errorf("nil error produced %q, want empty", got)
	}
	plain := errors.New("dial tcp: lookup example.invalid: no such host")
	if got := retryNoticeMessage(plain); got != plain.Error() {
		t.Errorf("plain error rewritten to %q, want %q", got, plain.Error())
	}
}

// A single wrapped error must not repeat its own text when the leaf is already
// present in the message.
func TestRetryNoticeMessageDoesNotDuplicateTheCause(t *testing.T) {
	leaf := errors.New("connection reset by peer")
	err := agenterrors.NewNetwork("stream failed", leaf)

	got := retryNoticeMessage(err)
	if n := strings.Count(got, "connection reset by peer"); n != 1 {
		t.Errorf("cause appears %d times in %q, want once", n, got)
	}
	_ = fmt.Sprint()
}
