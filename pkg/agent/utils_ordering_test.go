package agent

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPrintLineAsyncPreservesOrderingUnderBackpressure(t *testing.T) {
	agent := &Agent{
		outputMutex: &sync.Mutex{},
	}
	agent.asyncBufferSize = 4
	agent.SetStreamingEnabled(true)

	const total = 100

	var (
		outputs []string
		mu      sync.Mutex
		wg      sync.WaitGroup
	)
	wg.Add(total)

	agent.SetStreamingCallback(func(text string) {
		defer wg.Done()

		mu.Lock()
		outputs = append(outputs, strings.TrimSuffix(text, "\n"))
		mu.Unlock()

		time.Sleep(200 * time.Microsecond)
	})

	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		for i := 0; i < total; i++ {
			agent.PrintLineAsync(fmt.Sprintf("msg-%03d", i))
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for async output to drain")
	}

	select {
	case <-sendDone:
	case <-time.After(time.Second):
		t.Fatal("sender goroutine did not complete")
	}

	if len(outputs) != total {
		t.Fatalf("expected %d outputs, got %d", total, len(outputs))
	}

	for i := 0; i < total; i++ {
		expected := fmt.Sprintf("msg-%03d", i)
		if outputs[i] != expected {
			t.Fatalf("output[%d] = %q, want %q", i, outputs[i], expected)
		}
	}
}
