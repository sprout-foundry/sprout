//go:build !js

package webui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// helper: return a short-lived unique session ID.
func uniqueSessionID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// --------------------------------------------------------------------------
// Test 1: Rapid close on a session with data in flight – stress test
// --------------------------------------------------------------------------

func TestDetachWhilePTYActive_Race(t *testing.T) {
	// Use multiple goroutines to increase the chance of exposing the race.
	oldProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(oldProcs)

	t.Setenv("TERM", "xterm-256color")

	dir, err := os.MkdirTemp("", "ledit-terminal-race-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	tm := NewTerminalManager(dir)

	const iterations = 100
	panicCount := int32(0)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			id := uniqueSessionID(fmt.Sprintf("detach-race-%d", idx))

			_, err := tm.CreateSession(id)
			if err != nil {
				if strings.Contains(err.Error(), "no suitable shell") ||
					strings.Contains(err.Error(), "no such file") {
					t.Logf("iteration %d: skipping, no shell: %v", idx, err)
					return
				}
				oops := recover()
				if oops != nil {
					mu.Lock()
					panicCount++
					mu.Unlock()
					t.Errorf("iteration %d panicked: %v", idx, oops)
				}
				return
			}

			// Give the PTY reader time to start.
			time.Sleep(time.Millisecond)

			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					panicCount++
					mu.Unlock()
					t.Errorf("iteration %d panicked on close: %v", idx, r)
				}
			}()

			_ = tm.CloseSession(id)
		}(i)
	}

	wg.Wait()

	if panicCount > 0 {
		t.Fatalf("%d of %d iterations panicked", panicCount, iterations)
	}
	t.Logf("all %d iterations completed without panic", iterations)
}

// --------------------------------------------------------------------------
// Test 2: Write data to PTY then close while reader processes it
// --------------------------------------------------------------------------

func TestDetachDoesNotPanicWithBufferedOutput(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	dir, err := os.MkdirTemp("", "ledit-terminal-race-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	tm := NewTerminalManager(dir)
	id := uniqueSessionID("buffered-output")

	sess, err := tm.CreateSession(id)
	if err != nil {
		t.Skipf("skipping: cannot create session: %v", err)
	}

	// Write a bunch of data to the PTY so the reader goroutine has bytes in flight.
	for i := 0; i < 200; i++ {
		_, err := sess.Pty.Write([]byte(fmt.Sprintf("echo line-%04d\n", i)))
		if err != nil {
			break
		}
	}

	time.Sleep(5 * time.Millisecond)

	recovered := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				recovered = true
				t.Errorf("panic during CloseSession: %v", r)
			}
		}()
		_ = tm.CloseSession(id)
	}()

	if recovered {
		t.Fatal("CloseSession panicked — race condition NOT fixed")
	}
	t.Log("no panic observed with buffered output")
}

// --------------------------------------------------------------------------
// Test 3: CloseSession while data is actively flowing
// --------------------------------------------------------------------------

func TestCloseSessionDoesNotPanic(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")

	dir, err := os.MkdirTemp("", "ledit-terminal-race-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	const iterations = 50

	oldProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(oldProcs)

	panicCount := int32(0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			tm := NewTerminalManager(dir)
			id := uniqueSessionID(fmt.Sprintf("close-race-%d", idx))
			sess, err := tm.CreateSession(id)
			if err != nil {
				if strings.Contains(err.Error(), "no suitable shell") ||
					strings.Contains(err.Error(), "no such file") {
					return
				}
				t.Errorf("iteration %d: %v", idx, err)
				return
			}

			// Fire data into the PTY to keep the reader busy.
			for j := 0; j < 50; j++ {
				sess.Pty.Write([]byte(fmt.Sprintf("echo x-%d\n", j)))
			}
			time.Sleep(time.Millisecond)

			func() {
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						panicCount++
						mu.Unlock()
						t.Errorf("iteration %d panicked on CloseSession: %v", idx, r)
					}
				}()
				_ = tm.CloseSession(id)
			}()
		}(i)
	}

	wg.Wait()

	if panicCount > 0 {
		t.Fatalf("%d of %d iterations panicked on CloseSession", panicCount, iterations)
	}
	t.Logf("all %d CloseSession iterations completed without panic", iterations)
}
