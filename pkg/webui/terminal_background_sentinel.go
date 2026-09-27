//go:build !js

package webui

// terminal_background_sentinel.go — the background completion-sentinel
// machinery, split out of terminal_background.go. wrapBackgroundCommand
// wraps a command so it echoes the completion marker; checkBackgroundSentinel
// scans PTY output for that marker (recording the exit code and closing
// bgDone exactly once); closeBackgroundDoneLocked is the no-sentinel
// fallback for sessions that die before reporting.
import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// wrapBackgroundCommand wraps a command with the completion sentinel, the
// same convention ExecuteCommandAndWait uses. Both the success and failure
// branches echo the marker with the command's exit status, so the sentinel
// fires regardless of exit code.
func wrapBackgroundCommand(command, marker string) string {
	escapedCmd := strings.ReplaceAll(command, "'", "'\\''")
	return fmt.Sprintf(
		"/bin/sh -c '%s && echo \"%s$?\" || echo \"%s$?\"'\n",
		escapedCmd, sentinelPrefix+marker+":", sentinelPrefix+marker+":",
	)
}

// checkBackgroundSentinel examines newly-arrived output for the session's
// completion sentinel. Called from the PTY reader with session.mutex held.
// Carries a small tail of previously-scanned bytes so a sentinel straddling
// a chunk boundary is still detected. Closes bgDone exactly once when the
// sentinel is found, recording the parsed exit code.
//
// Returns (justCompleted, exitCode, true) exactly once — on the transition
// to completed — so the caller can fire the lifecycle hook AFTER releasing
// session.mutex (the hook takes tm.mutex; holding both would invert the
// manager→session lock order and can deadlock).
func (s *TerminalSession) checkBackgroundSentinel(chunk []byte) (justCompleted bool, exitCode int, wasBackground bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.bgMarker == "" || s.bgDone == nil {
		return false, 0, false
	}
	select {
	case <-s.bgDone:
		return false, 0, false // already completed
	default:
	}

	markerStr := []byte(sentinelPrefix + s.bgMarker + ":")

	window := make([]byte, 0, len(s.bgTail)+len(chunk))
	window = append(window, s.bgTail...)
	window = append(window, chunk...)

	idx := bytes.Index(window, markerStr)
	for idx >= 0 {
		after := idx + len(markerStr)
		if after < len(window) {
			next := window[after]
			if next >= '0' && next <= '9' {
				// Real sentinel echo (not the PTY's echo of the wrapped
				// command line, which ends with ":$?"). Parse exit digits.
				var codeStr []byte
				for j := after; j < len(window); j++ {
					if window[j] >= '0' && window[j] <= '9' {
						codeStr = append(codeStr, window[j])
					} else {
						break
					}
				}
				if len(codeStr) > 0 && len(codeStr) <= 3 {
					if code, err := strconv.Atoi(string(codeStr)); err == nil {
						// A BgExitStopped recorded by StopBackgroundSession
						// wins over a stray post-SIGINT sentinel echo: a
						// deliberate stop is the more truthful outcome.
						if s.bgExitCode != BgExitStopped {
							s.bgExitCode = code
						}
						close(s.bgDone)
						s.bgTail = nil
						return true, code, true
					}
				}
			}
		}
		// Not the exit echo — try the next occurrence (e.g. the command's
		// own echo of the wrapped line, which ends with ":$?").
		remaining := window[idx+1:]
		nextIdx := bytes.Index(remaining, markerStr)
		if nextIdx >= 0 {
			idx = idx + 1 + nextIdx
		} else {
			idx = -1
		}
	}

	// Keep only the trailing bytes that could still complete a marker.
	if keep := len(window) - bgTailKeep; keep > 0 {
		s.bgTail = append(s.bgTail[:0:0], window[keep:]...)
	} else {
		s.bgTail = append(s.bgTail[:0:0], window...)
	}
	return false, 0, false
}

// closeBackgroundDoneLocked closes bgDone if not already closed, marking the
// command as finished with code BgExitNone. Used when a session dies before
// producing a sentinel. Caller holds session.mutex. A BgExitStopped recorded
// by StopBackgroundSession is preserved — a deliberate stop is not "ended
// without a report".
func (s *TerminalSession) closeBackgroundDoneLocked() {
	if s.bgDone == nil {
		return
	}
	select {
	case <-s.bgDone:
	default:
		if s.bgExitCode != BgExitStopped {
			s.bgExitCode = BgExitNone
		}
		close(s.bgDone)
	}
}
