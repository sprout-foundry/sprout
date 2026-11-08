//go:build !js

package appcheck

import (
	"testing"

	"github.com/go-rod/rod/lib/proto"
)

// An uncaught exception reads as its message, not Chrome's bare "Uncaught".
func TestExceptionTextIsTheThrownMessage(t *testing.T) {
	d := &proto.RuntimeExceptionDetails{
		Text:      "Uncaught",
		Exception: &proto.RuntimeRemoteObject{Description: "TypeError: Cannot read properties of null (reading 'x')\n    at /err/:1:30"},
	}
	if got, want := exceptionText(d), "TypeError: Cannot read properties of null (reading 'x')"; got != want {
		t.Fatalf("exceptionText = %q, want %q", got, want)
	}
	if got := exceptionText(&proto.RuntimeExceptionDetails{Text: "Uncaught"}); got != "Uncaught" {
		t.Fatalf("without a thrown value: %q, want the details text", got)
	}
	if got := exceptionText(nil); got != "" {
		t.Fatalf("nil details: %q", got)
	}
}
