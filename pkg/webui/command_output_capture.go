//go:build !js

package webui

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
)

// captureCommandOutput runs a slash command through the registry, capturing
// its output via an invocation-local pipe rather than process-global os.Stdout.
// The onChunk callback (if non-nil) receives streamed output chunks in real
// time; all output is accumulated into the returned string regardless.
//
// If recoverFromPanic is true, panics from registry.Execute are converted to
// errors (used by synchronous callers like steer dispatch). If false, panics
// propagate to the caller's own recovery (used by goroutine-based callers).
//
// If pipe creation fails (FD exhaustion), the command still runs with output
// discarded — the error is logged but not returned.
func captureCommandOutput(
	logger *slog.Logger,
	logTag string,
	registry *agent_commands.CommandRegistry,
	input string,
	chatAgent *agent.Agent,
	onChunk func(string),
	recoverFromPanic bool,
) (string, error) {
	buf := new(strings.Builder)
	pipeR, pipeW, pipeErr := os.Pipe()

	if pipeErr != nil {
		logger.Warn("command output pipe creation failed; output will be lost",
			slog.String("handler", logTag),
			slog.Any("err", pipeErr),
		)
		registry.SetOutput(io.Discard)
		err := registry.Execute(input, chatAgent)
		registry.SetOutput(nil)
		return "", err
	}

	registry.SetOutput(pipeW)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		if onChunk != nil {
			streamPipeChunks(pipeR, buf, onChunk)
		} else if _, copyErr := io.Copy(buf, pipeR); copyErr != nil {
			logger.Error("command output pipe read failed",
				slog.String("handler", logTag),
				slog.Any("err", copyErr),
			)
		}
	}()

	var cmdErr error
	func() {
		if recoverFromPanic {
			defer func() {
				if rec := recover(); rec != nil {
					cmdErr = fmt.Errorf("command panicked: %v", rec)
				}
			}()
		}
		cmdErr = registry.Execute(input, chatAgent)
	}()

	registry.SetOutput(nil)
	_ = pipeW.Close()
	<-readerDone
	_ = pipeR.Close()

	return buf.String(), cmdErr
}
