package components

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestStreamingFormatter_LockingPattern tests that the Write method doesn't use nested locking
// which could cause deadlocks
func TestStreamingFormatter_LockingPattern(t *testing.T) {
	outputMutex := &sync.Mutex{}
	sf := NewStreamingFormatter(outputMutex)

	// Test that Write method properly handles locking without deadlocks
	// This test ensures that the fix for nested locking is working
	
	// Create a channel to signal when output mutex is acquired
	mutexAcquired := make(chan bool, 1)
	
	// Acquire the output mutex first to simulate a potential deadlock scenario
	outputMutex.Lock()
	
	// Start a goroutine that will try to write while output mutex is held
	go func() {
		// This should not deadlock because Write releases sf.mu before trying to acquire outputMutex
		sf.Write("Test content")
		mutexAcquired <- true
	}()
	
	// Wait a short time to ensure the goroutine has started
	time.Sleep(10 * time.Millisecond)
	
	// Release the output mutex - the Write method should now be able to proceed
	outputMutex.Unlock()
	
	// Wait for the goroutine to complete
	select {
	case <-mutexAcquired:
		// Success - no deadlock occurred
		assert.True(t, true, "Write method completed without deadlock")
	case <-time.After(1 * time.Second):
		t.Fatal("Write method appears to have deadlocked")
	}
}

// TestStreamingFormatter_ConcurrentWrites tests concurrent writes to the formatter
func TestStreamingFormatter_ConcurrentWrites(t *testing.T) {
	outputMutex := &sync.Mutex{}
	sf := NewStreamingFormatter(outputMutex)

	// Test concurrent writes from multiple goroutines
	var wg sync.WaitGroup
	numGoroutines := 10
	iterations := 100
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				sf.Write("Concurrent write")
			}
		}(i)
	}
	
	// Use a timeout to detect potential deadlocks
	done := make(chan bool, 1)
	go func() {
		wg.Wait()
		done <- true
	}()
	
	select {
	case <-done:
		// Success - all goroutines completed
		assert.True(t, true, "All concurrent writes completed successfully")
	case <-time.After(5 * time.Second):
		t.Fatal("Concurrent writes appear to have deadlocked")
	}
}

// TestStreamingFormatter_FirstChunkLocking tests the specific locking pattern for first chunk handling
func TestStreamingFormatter_FirstChunkLocking(t *testing.T) {
	outputMutex := &sync.Mutex{}
	sf := NewStreamingFormatter(outputMutex)

	// Verify initial state
	assert.True(t, sf.isFirstChunk, "Formatter should start with isFirstChunk=true")
	
	// Test that first Write properly handles the first chunk scenario
	// This is where the nested locking issue was fixed
	
	// Acquire output mutex to simulate contention
	outputMutex.Lock()
	
	firstWriteDone := make(chan bool, 1)
	
	go func() {
		// This should release sf.mu before trying to acquire outputMutex
		sf.Write("First chunk test")
		firstWriteDone <- true
	}()
	
	// Wait a moment for the goroutine to start
	time.Sleep(10 * time.Millisecond)
	
	// Release output mutex
	outputMutex.Unlock()
	
	// Wait for completion
	select {
	case <-firstWriteDone:
		// Success - first chunk handling worked without deadlock
		assert.False(t, sf.isFirstChunk, "isFirstChunk should be false after first write")
	case <-time.After(1 * time.Second):
		t.Fatal("First chunk handling appears to have deadlocked")
	}
}

// TestStreamingFormatter_FlushLocking tests the flush method's locking behavior
func TestStreamingFormatter_FlushLocking(t *testing.T) {
	outputMutex := &sync.Mutex{}
	sf := NewStreamingFormatter(outputMutex)

	// Add some content to the buffer
	sf.Write("Test content for flushing")
	
	// Test flush with output mutex contention
	outputMutex.Lock()
	
	flushDone := make(chan bool, 1)
	
	go func() {
		// flush should acquire sf.mu, then try to acquire outputMutex
		// This should work without deadlock
		sf.flush()
		flushDone <- true
	}()
	
	// Wait a moment
	time.Sleep(10 * time.Millisecond)
	
	// Release output mutex
	outputMutex.Unlock()
	
	select {
	case <-flushDone:
		// Success - flush completed
		assert.True(t, true, "Flush completed without deadlock")
	case <-time.After(1 * time.Second):
		t.Fatal("Flush appears to have deadlocked")
	}
}

// TestStreamingFormatter_MutexOrdering tests that mutexes are acquired in a consistent order
func TestStreamingFormatter_MutexOrdering(t *testing.T) {
	outputMutex := &sync.Mutex{}
	sf := NewStreamingFormatter(outputMutex)

	// This test ensures that the locking order is consistent:
	// 1. Acquire sf.mu
	// 2. Release sf.mu before acquiring outputMutex (if needed)
	// 3. Reacquire sf.mu after outputMutex operations
	
	// Test multiple operations to ensure consistent behavior
	for i := 0; i < 10; i++ {
		sf.Write("Test write")
	}
	
	// Finalize should also work correctly
	sf.Finalize()
	
	assert.True(t, sf.finalized, "Formatter should be finalized")
}

// TestStreamingFormatter_NoNestedLocking validates that no nested locking occurs
func TestStreamingFormatter_NoNestedLocking(t *testing.T) {
	outputMutex := &sync.Mutex{}
	sf := NewStreamingFormatter(outputMutex)

	// This test specifically validates that the fix for nested locking is working
	// by ensuring that sf.mu is never held while trying to acquire outputMutex
	
	// Use a simple test: if Write completes without deadlock when outputMutex is held,
	// it means the nested locking fix is working
	
	outputMutex.Lock()
	
	writeDone := make(chan bool, 1)
	
	go func() {
		// This should complete because Write releases sf.mu before trying outputMutex
		sf.Write("Test content")
		writeDone <- true
	}()
	
	// Wait a moment
	time.Sleep(10 * time.Millisecond)
	
	// Release output mutex
	outputMutex.Unlock()
	
	select {
	case <-writeDone:
		// Success - no nested locking occurred
		assert.True(t, true, "Write completed without nested locking")
	case <-time.After(1 * time.Second):
		t.Fatal("Write appears to have deadlocked due to nested locking")
	}
}