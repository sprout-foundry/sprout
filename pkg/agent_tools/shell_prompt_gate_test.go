//go:build !js

package tools

import (
	"sync"
	"testing"
)

func TestPromptGateDuplicateOnOtherPipeRejected(t *testing.T) {
	g := newPromptGate()
	if !g.claim(pipeStdout, "Password:") {
		t.Fatal("first report on stdout should ask")
	}
	if g.claim(pipeStderr, "Password:") {
		t.Error("stderr reporting the same question must not ask again")
	}
}

func TestPromptGateSamePipeRepeatAllowed(t *testing.T) {
	g := newPromptGate()
	if !g.claim(pipeStdout, "Password:") {
		t.Fatal("first report should ask")
	}
	// A command may prompt twice in a row on one stream; that is a new
	// question, not a duplicate.
	if !g.claim(pipeStdout, "Password:") {
		t.Error("the same pipe asking again must be allowed")
	}
}

func TestPromptGateDifferentTextNotDuplicate(t *testing.T) {
	g := newPromptGate()
	g.claim(pipeStdout, "Password:")
	// A different question on the other stream is not a duplicate.
	if !g.claim(pipeStderr, "Enter passphrase:") {
		t.Error("a different prompt must be asked")
	}
}

// TestPromptGateDelayedCopyAfterOutputRejected pins the case CI caught: the
// other stream's copy of the prompt arrives after the answer and after the
// command's own output. Output in between must not reopen the question —
// identical text on both streams is one question, always.
func TestPromptGateDelayedCopyAfterOutputRejected(t *testing.T) {
	g := newPromptGate()
	if !g.claim(pipeStdout, "Password:") {
		t.Fatal("first report on stdout should ask")
	}
	// The answer is written and the command echoes it; the delayed stderr
	// copy then lands. It must still be refused.
	if g.claim(pipeStderr, "Password:") {
		t.Error("a delayed copy of the answered question must not ask again")
	}
}

func TestPromptGateUnansweredDuplicateRejected(t *testing.T) {
	g := newPromptGate()
	if !g.claim(pipeStdout, "Password:") {
		t.Fatal("first report should ask")
	}
	if g.claim(pipeStderr, "Password:") {
		t.Error("the other stream reporting the in-flight question must not ask again")
	}
}

func TestPromptGateConcurrentClaimSingleWinner(t *testing.T) {
	const pipes = 2
	for i := 0; i < 100; i++ {
		g := newPromptGate()
		var wg sync.WaitGroup
		var mu sync.Mutex
		winners := 0
		wg.Add(pipes)
		for p := 0; p < pipes; p++ {
			go func(pipe int) {
				defer wg.Done()
				if g.claim(pipe, "Password:") {
					mu.Lock()
					winners++
					mu.Unlock()
				}
			}(p)
		}
		wg.Wait()
		if winners != 1 {
			t.Fatalf("iteration %d: winners = %d, want 1 (exactly one stream per question)", i, winners)
		}
	}
}

func TestNormalizePromptText(t *testing.T) {
	if got := normalizePromptText("  Password: \r\n"); got != "Password:" {
		t.Errorf("normalizePromptText = %q, want %q", got, "Password:")
	}
}
