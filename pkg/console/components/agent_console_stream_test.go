package components

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEnqueueBestEffortPreservesOrdering(t *testing.T) {
	ac := &AgentConsole{
		streamCh: make(chan string, 1),
	}

	ac.streamingFormatter = NewStreamingFormatter(&ac.outputMutex)
	ac.streamingFormatter.isFirstChunk = false
	ac.streamingFormatter.lastWasNewline = true

	var (
		outputs []string
		mu      sync.Mutex
	)

	ac.streamingFormatter.SetOutputFunc(func(text string) {
		mu.Lock()
		outputs = append(outputs, text)
		mu.Unlock()
	})

	startWorker := make(chan struct{})
	workerDone := make(chan struct{})

	go func() {
		<-startWorker
		for i := 0; i < 2; i++ {
			content := <-ac.streamCh
			ac.streamingFormatter.Write(content)
		}
		close(workerDone)
	}()

	if err := ac.enqueueBestEffort("first\n"); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ac.enqueueBestEffort("second\n"); err != nil {
			t.Errorf("enqueue second: %v", err)
		}
	}()

	time.Sleep(25 * time.Millisecond)
	close(startWorker)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second enqueue never completed")
	}

	select {
	case <-workerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not finish processing messages")
	}

	mu.Lock()
	combined := strings.Join(outputs, "")
	mu.Unlock()

	firstIdx := strings.Index(combined, "first")
	secondIdx := strings.Index(combined, "second")

	if firstIdx == -1 || secondIdx == -1 {
		t.Fatalf("combined output missing expected substrings: %q", combined)
	}

	if firstIdx > secondIdx {
		t.Fatalf("outputs out of order: %q", combined)
	}
}
