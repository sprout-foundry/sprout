//go:build !js && !windows

package appcheck

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// hostProcess is one started server process (wrangler) in its own process
// group, so the whole tree — wrangler and the worker it runs — stops with one
// signal and the port is actually freed.
type hostProcess struct {
	cmd     *exec.Cmd
	out     *syncBuffer
	exited  chan struct{}
	logDone chan struct{}
}

// startHostProcess starts command in dir, collecting its combined output into
// log.
func startHostProcess(command string, args []string, dir string, env []string, log *logSink) (*hostProcess, error) {
	out := newSyncBuffer()
	cmd := exec.Command(command, args...) //nolint:gosec // G204: the project's own wrangler binary
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}
	p := &hostProcess{cmd: cmd, out: out, exited: make(chan struct{}), logDone: make(chan struct{})}
	go func() {
		defer close(p.logDone)
		p.pump(log)
	}()
	go func() {
		defer close(p.exited)
		_ = cmd.Wait()
	}()
	return p, nil
}

// pump copies the process's output into log as it arrives.
func (p *hostProcess) pump(log *logSink) {
	for {
		select {
		case <-p.logDone:
			return
		default:
		}
		chunk, err := p.out.next()
		if chunk != "" {
			log.append(chunk)
		}
		if err != nil {
			return
		}
	}
}

// stop asks the process group to exit, then kills it if it has not within
// timeout.
func (p *hostProcess) stop(timeout time.Duration) {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-p.exited:
		return
	case <-time.After(timeout):
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// wait blocks until the process has been reaped.
func (p *hostProcess) wait() {
	if p == nil {
		return
	}
	<-p.exited
	<-p.logDone
}

// maxServerLogBytes bounds the buffered server output.
const maxServerLogBytes = 1 << 20

// syncBuffer is an output sink one goroutine writes and another reads
// without losing the tail.
type syncBuffer struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  bytes.Buffer
	done bool
}

func newSyncBuffer() *syncBuffer {
	b := &syncBuffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() > maxServerLogBytes {
		// Keep the end of a chatty server log, where the error is.
		kept := b.buf.String()
		b.buf.Reset()
		b.buf.WriteString(kept[len(kept)/2:])
	}
	n, err := b.buf.Write(p)
	b.cond.Signal()
	return n, err
}

// next blocks until output is available and returns it with any read error.
func (b *syncBuffer) next() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.buf.Len() == 0 && !b.done {
		b.cond.Wait()
	}
	chunk := b.buf.String()
	b.buf.Reset()
	return chunk, nil
}
