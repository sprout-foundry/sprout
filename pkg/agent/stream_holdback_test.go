// stream_holdback_test.go — unit tests for the streaming language-guard
// hold-back (SP-152 §152c): a correct-language stream is buffered until the
// prose threshold, released, and streamed live; a wrong-language stream never
// reaches the sink; short and code-only streams are released (never held);
// and an undetermined user language is a pure passthrough.
package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/langguard"
)

// Reliably detectable prose fixtures (mirrored from pkg/langguard's detect
// fixtures). The English fixture is reliably detected even at ~45+ runes, so
// it drives a reliable mismatch; the Spanish fixture is undetermined below
// ~56 runes, so a Spanish stream is released on the first (undetermined)
// check — both are "not a mismatch" and release per §152c.
const (
	hbSpanishProse = "El paquete está listo para compilar ahora mismo y las pruebas pasan sin errores."
	hbEnglishProse = "The build succeeded after applying the patch, so the tests can run and the release is ready to ship."
)

// sinkRecorder records every delivery to the hold-back's sink, in order.
type sinkRecorder struct {
	calls []string
}

func (r *sinkRecorder) sink(content string) {
	r.calls = append(r.calls, content)
}

func (r *sinkRecorder) joined() string { return strings.Join(r.calls, "") }

var (
	esUser = langguard.Language{Code: "es", Name: "Spanish"}
	enUser = langguard.Language{Code: "en", Name: "English"}
)

// TestStreamHoldbackCorrectLanguage pins the release path: a correct-language
// stream is buffered until the prose is judgable, then the buffered content is
// released to the sink in one call and the remaining chunks stream live — the
// sink receives the buffered content followed by the live chunks, in order,
// and the full reply is delivered (no loss, no duplication).
func TestStreamHoldbackCorrectLanguage(t *testing.T) {
	// Split the Spanish reply into three chunks: the first two buffer up to
	// (and past) the judgable threshold, the third streams live.
	chunks := []string{
		"El paquete está ",
		"listo para compilar ahora ",
		"mismo y las pruebas pasan sin errores.",
	}
	rec := &sinkRecorder{}
	hb := NewStreamHoldback(esUser, rec.sink)

	hb.Write(chunks[0])
	if len(rec.calls) != 0 {
		t.Fatalf("chunk 1 should be buffered (below threshold), sink got %v", rec.calls)
	}
	hb.Write(chunks[1])
	if len(rec.calls) != 1 {
		t.Fatalf("chunk 2 crosses the threshold and releases the buffer once; sink got %d calls", len(rec.calls))
	}
	hb.Write(chunks[2])
	hb.Finish()

	if got, want := hb.State(), HoldbackLive; got != want {
		t.Errorf("State() = %v, want %v", got, want)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("sink got %d calls (release + live chunk), want 2: %v", len(rec.calls), rec.calls)
	}
	// The first delivery is the whole buffered prefix; the second is the
	// live chunk that followed.
	if !strings.HasPrefix(hbSpanishProse, rec.calls[0]) {
		t.Errorf("first delivery %q is not a prefix of the reply", rec.calls[0])
	}
	if rec.calls[0] != chunks[0]+chunks[1] {
		t.Errorf("release delivered %q, want the buffered %q", rec.calls[0], chunks[0]+chunks[1])
	}
	if rec.calls[1] != chunks[2] {
		t.Errorf("live chunk = %q, want %q", rec.calls[1], chunks[2])
	}
	if got, want := rec.joined(), hbSpanishProse; got != want {
		t.Errorf("sink received %q, want the full reply %q", got, want)
	}
	if held := hb.Held(); held != "" {
		t.Errorf("Held() = %q, want \"\" (a correct-language stream is never held)", held)
	}
}

// TestStreamHoldbackWrongLanguage pins the hold path: a reliably wrong-language
// stream never reaches the sink (the wrong content is held, not delivered),
// the hold-back reports held, and Held() returns the whole reply including
// chunks that arrived after the mismatch was detected.
func TestStreamHoldbackWrongLanguage(t *testing.T) {
	// The first chunk alone is reliably English (a mismatch for the Spanish
	// user); the second chunk arrives after the mismatch and is captured.
	chunks := []string{
		"The build succeeded after applying the patch, so the tests ",
		"can run and the release is ready to ship.",
	}
	rec := &sinkRecorder{}
	hb := NewStreamHoldback(esUser, rec.sink)

	hb.Write(chunks[0])
	if hb.State() != HoldbackHeld {
		t.Fatalf("State() = %v, want %v (a reliable mismatch holds the stream)", hb.State(), HoldbackHeld)
	}
	hb.Write(chunks[1])
	hb.Finish()

	if got, want := hb.State(), HoldbackHeld; got != want {
		t.Errorf("State() = %v, want %v", got, want)
	}
	if len(rec.calls) != 0 {
		t.Errorf("the wrong-language content must never reach the client; sink got %v", rec.calls)
	}
	if got, want := hb.Held(), hbEnglishProse; got != want {
		t.Errorf("Held() = %q, want the whole held reply %q", got, want)
	}
}

// TestStreamHoldbackShortStream pins that a reply below the prose threshold is
// not judged (§152a) and is released on Finish — never held.
func TestStreamHoldbackShortStream(t *testing.T) {
	rec := &sinkRecorder{}
	hb := NewStreamHoldback(esUser, rec.sink)

	hb.Write("Hola")
	if hb.State() != HoldbackBuffering {
		t.Fatalf("State() = %v, want %v (below threshold, still buffering)", hb.State(), HoldbackBuffering)
	}
	hb.Finish()

	if got, want := hb.State(), HoldbackLive; got != want {
		t.Errorf("State() = %v, want %v (released on Finish)", got, want)
	}
	if got, want := rec.joined(), "Hola"; got != want {
		t.Errorf("sink received %q, want the short reply %q", got, want)
	}
	if held := hb.Held(); held != "" {
		t.Errorf("Held() = %q, want \"\" (a short stream is never held)", held)
	}
}

// TestStreamHoldbackCodeOnlyStream pins that a long code-only stream (no prose
// — code is excluded from the judgment, §152a) is not held and is released on
// Finish.
func TestStreamHoldbackCodeOnlyStream(t *testing.T) {
	code := "```go\n" + strings.Repeat("x := 1;\n", 20) + "```"
	// Feed in chunks so the raw buffer crosses the threshold, but the
	// extracted prose stays empty (all code).
	chunks := []string{"```go\n", "x := 1;\n", "x := 1;\n", strings.Repeat("x := 1;\n", 18) + "```"}
	rec := &sinkRecorder{}
	hb := NewStreamHoldback(esUser, rec.sink)

	for _, c := range chunks {
		hb.Write(c)
	}
	// Still buffering: the prose (code stripped) is empty, so it was never
	// judged and the hold-back has not resolved.
	if hb.State() != HoldbackBuffering {
		t.Fatalf("State() = %v, want %v (code-only stream stays buffering)", hb.State(), HoldbackBuffering)
	}
	hb.Finish()

	if got, want := hb.State(), HoldbackLive; got != want {
		t.Errorf("State() = %v, want %v (released on Finish)", got, want)
	}
	if got, want := rec.joined(), code; got != want {
		t.Errorf("sink received %q, want the code reply %q", got, want)
	}
	if held := hb.Held(); held != "" {
		t.Errorf("Held() = %q, want \"\" (a code-only stream is never held)", held)
	}
}

// TestStreamHoldbackUndeterminedUserLanguage pins the passthrough: with no
// determined user language the hold-back is inactive — it never holds and
// every chunk reaches the sink directly (byte-identical streaming).
func TestStreamHoldbackUndeterminedUserLanguage(t *testing.T) {
	chunks := []string{"The build ", "succeeded ", "after applying the patch."}
	rec := &sinkRecorder{}
	hb := NewStreamHoldback(langguard.Language{}, rec.sink)

	// Inactive from construction: live (passthrough), never buffering.
	if got, want := hb.State(), HoldbackLive; got != want {
		t.Fatalf("State() = %v, want %v (undetermined user language is a passthrough)", got, want)
	}
	for _, c := range chunks {
		hb.Write(c)
	}
	hb.Finish()

	if got, want := hb.State(), HoldbackLive; got != want {
		t.Errorf("State() = %v, want %v", got, want)
	}
	if len(rec.calls) != len(chunks) {
		t.Errorf("sink got %d calls, want one per chunk (%d): %v", len(rec.calls), len(chunks), rec.calls)
	}
	if got, want := rec.joined(), strings.Join(chunks, ""); got != want {
		t.Errorf("sink received %q, want %q", got, want)
	}
	if held := hb.Held(); held != "" {
		t.Errorf("Held() = %q, want \"\" (a passthrough never holds)", held)
	}
}

// TestStreamHoldbackHeldCapturesLaterChunks pins that, once a stream is held,
// chunks arriving after the mismatch are captured into Held() (the whole wrong
// reply is available for "view original") but never delivered.
func TestStreamHoldbackHeldCapturesLaterChunks(t *testing.T) {
	rec := &sinkRecorder{}
	hb := NewStreamHoldback(esUser, rec.sink)

	hb.Write("The build succeeded after applying the patch, so the tests ")
	hb.Write("can run ")
	hb.Write("and the release is ready to ship.")
	hb.Finish()

	if hb.State() != HoldbackHeld {
		t.Fatalf("State() = %v, want %v", hb.State(), HoldbackHeld)
	}
	if got, want := hb.Held(), hbEnglishProse; got != want {
		t.Errorf("Held() = %q, want the whole reply %q", got, want)
	}
	if len(rec.calls) != 0 {
		t.Errorf("the held content must never reach the client; sink got %v", rec.calls)
	}
}

// TestStreamHoldbackResetDiscardsState pins that Reset (a provider retry)
// drops a held or buffered attempt so it never leaks into the next one.
func TestStreamHoldbackResetDiscardsState(t *testing.T) {
	rec := &sinkRecorder{}
	hb := NewStreamHoldback(esUser, rec.sink)

	// First attempt: a wrong-language stream is held.
	hb.Write("The build succeeded after applying the patch, so the tests ")
	if hb.State() != HoldbackHeld {
		t.Fatalf("State() = %v, want %v", hb.State(), HoldbackHeld)
	}
	hb.Reset()

	if got, want := hb.State(), HoldbackBuffering; got != want {
		t.Fatalf("after Reset State() = %v, want %v (a fresh attempt)", got, want)
	}
	if held := hb.Held(); held != "" {
		t.Errorf("after Reset Held() = %q, want \"\" (the discarded attempt is gone)", held)
	}
	// A correct-language stream on the fresh attempt is released.
	hb.Write("El paquete está listo para compilar ahora mismo y ")
	hb.Write("las pruebas pasan sin errores.")
	hb.Finish()
	if got, want := hb.State(), HoldbackLive; got != want {
		t.Errorf("State() = %v, want %v (the fresh correct-language stream is released)", got, want)
	}
	if got, want := rec.joined(), hbSpanishProse; got != want {
		t.Errorf("sink received %q, want the fresh reply %q", got, want)
	}
}

// TestStreamHoldbackUserExposesUserLanguage pins the User() accessor used to
// build the §152b notice.
func TestStreamHoldbackUserExposesUserLanguage(t *testing.T) {
	hb := NewStreamHoldback(enUser, func(string) {})
	if got := hb.User(); got != enUser {
		t.Errorf("User() = %v, want %v", got, enUser)
	}
}

// TestStreamHoldbackRawLenTracksBufferedContent pins RawLen, which the
// reasoning-model fallback uses to tell "the hold-back already holds this
// response's content" (RawLen > 0 — the finalize releases or holds it) from
// "the model streamed it as reasoning and the hold-back holds nothing"
// (RawLen == 0 — route the content through the hold-back). A released
// hold-back has an empty buffer too, so RawLen > 0 must mean "still holding
// content" (buffering or held), never "already released".
func TestStreamHoldbackRawLenTracksBufferedContent(t *testing.T) {
	// Fresh (buffering): nothing buffered yet.
	hb := NewStreamHoldback(esUser, func(string) {})
	if got := hb.RawLen(); got != 0 {
		t.Fatalf("fresh hold-back RawLen() = %d, want 0", got)
	}

	// Buffering with content: the pending prefix is buffered.
	hb.Write("El paquete está ")
	if hb.RawLen() == 0 {
		t.Errorf("buffering hold-back RawLen() = 0, want > 0 (pending content buffered)")
	}

	// A released (correct-language) hold-back resets its buffer: RawLen is 0
	// even though content was delivered — so a caller must also confirm the
	// streaming buffer is empty before assuming the hold-back holds nothing.
	hb2 := NewStreamHoldback(esUser, func(string) {})
	hb2.Write("El paquete está listo para compilar ahora mismo y las pruebas pasan sin errores.")
	hb2.Finish()
	if got, want := hb2.State(), HoldbackLive; got != want {
		t.Fatalf("correct-language hold-back State() = %v, want %v (released)", got, want)
	}
	if got := hb2.RawLen(); got != 0 {
		t.Errorf("released hold-back RawLen() = %d, want 0 (buffer reset on release)", got)
	}

	// A held hold-back keeps its content: RawLen > 0.
	hb3 := NewStreamHoldback(esUser, func(string) {})
	hb3.Write("The build succeeded after applying the patch, so the tests can run and the release is ready to ship.")
	hb3.Finish()
	if got, want := hb3.State(), HoldbackHeld; got != want {
		t.Fatalf("wrong-language hold-back State() = %v, want %v (held)", got, want)
	}
	if hb3.RawLen() == 0 {
		t.Errorf("held hold-back RawLen() = 0, want > 0 (the held content is kept)")
	}
}
