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
	g.release(pipeStdout, "Password:")
	if g.claim(pipeStderr, "Password:") {
		t.Error("stderr reporting the same answered question must not ask again")
	}
}

func TestPromptGateSamePipeRepeatAllowed(t *testing.T) {
	g := newPromptGate()
	if !g.claim(pipeStdout, "Password:") {
		t.Fatal("first report should ask")
	}
	g.release(pipeStdout, "Password:")
	// A command may prompt twice in a row on one stream; that is a new
	// question, not a duplicate.
	if !g.claim(pipeStdout, "Password:") {
		t.Error("the same pipe asking again must be allowed")
	}
}

func TestPromptGateDifferentTextNotDuplicate(t *testing.T) {
	g := newPromptGate()
	g.claim(pipeStdout, "Password:")
	g.release(pipeStdout, "Password:")
	// A different question on the other stream is not a duplicate.
	if !g.claim(pipeStderr, "Enter passphrase:") {
		t.Error("a different prompt must be asked")
	}
}

// TestPromptGateCrossPipeSecondQuestion is the case the both-pipes design got
// wrong: the first question is reported on one stream only, and the next
// question arrives on the other stream. It must be asked, not dropped.
func TestPromptGateCrossPipeSecondQuestion(t *testing.T) {
	g := newPromptGate()
	if !g.claim(pipeStdout, "Password:") {
		t.Fatal("first report on stdout should ask")
	}
	g.release(pipeStdout, "Password:")
	// The command moved on ("ok") and asks again, this time on stderr.
	g.observe()
	if !g.claim(pipeStderr, "Password:") {
		t.Error("a second question on the other stream must be asked")
	}
}

func TestPromptGateUnansweredDuplicateAllowed(t *testing.T) {
	g := newPromptGate()
	// Both streams report before either is answered: the first claims, and the
	// second is a duplicate of a question not yet answered. It must not ask
	// again while the first is in flight.
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
