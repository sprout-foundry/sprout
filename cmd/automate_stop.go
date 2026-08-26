//go:build !js

package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
)

var automateStopAll bool

func runAutomateStop(sessionID string) error {
	sproutDir, err := automateSessionRoot()
	if err != nil {
		return err
	}

	info, err := automate.ReadSessionFile(sproutDir, sessionID)
	if err != nil {
		return err
	}

	if info.EndedAt != nil {
		// Finalized record — retained deliberately for post-mortem
		// observability; stopping must not delete it.
		exit := "?"
		if info.ExitCode != nil {
			exit = fmt.Sprint(*info.ExitCode)
		}
		console.GlyphInfo.Printf("Session %s already finished (status: %s, exit: %s). Record retained until the retention window passes.", sessionID, info.Status, exit)
		return nil
	}

	if !automate.IsProcessAlive(info.PID) {
		// Dead but never finalized (parent killed before its defer ran) —
		// finalize with -1 rather than deleting, preserving the post-mortem.
		if err := automate.FinalizeSessionFile(sproutDir, sessionID, -1); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", err)
		}
		console.GlyphInfo.Printf("Session %s (PID %d) was already stopped. Record finalized for post-mortem (exit -1).", sessionID, info.PID)
		return nil
	}

	// PID-reuse guard: verify the process at this PID is the same one that
	// started this session. If the OS recycled the PID after the workflow
	// died without cleaning up, we must not signal the unrelated process.
	if !automate.VerifyProcessStartedBefore(info.PID, info.StartedAt) {
		console.GlyphWarning.Printf(
			"Session %s recorded PID %d started at %s, but the current process at that PID started later — possible PID reuse. Refusing to signal. Cleaned up PID file.",
			sessionID, info.PID, info.StartedAt.Format(time.RFC3339),
		)
		if err := automate.RemoveSessionFile(sproutDir, sessionID); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", err)
		}
		return nil
	}

	console.GlyphAction.Printf("Stopping session %s (PID %d, workflow: %s)...", sessionID, info.PID, info.Workflow)

	ok, err := automate.StopProcess(info.PID)
	if err != nil {
		console.GlyphWarning.Printf("Error stopping process %d: %v", info.PID, err)
	}

	// Remove PID file regardless
	if err := automate.RemoveSessionFile(sproutDir, sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v\n", err)
	}

	if ok {
		console.GlyphSuccess.Printf("Stopped session %s (PID %d).", sessionID, info.PID)
	} else {
		console.GlyphWarning.Printf("Session %s (PID %d) may still be running — verify manually.", sessionID, info.PID)
	}
	return nil
}

func runAutomateStopAll() error {
	sproutDir, err := automateSessionRoot()
	if err != nil {
		return err
	}

	sessions, err := readAllSessions(sproutDir)
	if err != nil {
		return err
	}

	stopped := 0
	for _, s := range sessions {
		if s.EndedAt != nil {
			// Finalized records are not running — and are retained for
			// post-mortem observability, so stop --all must not delete them.
			continue
		}
		if !automate.IsProcessAlive(s.PID) {
			// Dead but unfinalized — finalize rather than delete so the
			// post-mortem record survives stop --all.
			_ = automate.FinalizeSessionFile(sproutDir, s.SessionID, -1)
			continue
		}
		// PID-reuse guard (same as runAutomateStop).
		if !automate.VerifyProcessStartedBefore(s.PID, s.StartedAt) {
			console.GlyphWarning.Printf(
				"Session %s PID %d appears recycled — skipping and cleaning up.",
				s.SessionID, s.PID,
			)
			_ = automate.RemoveSessionFile(sproutDir, s.SessionID)
			continue
		}
		console.GlyphAction.Printf("Stopping session %s (PID %d)...", s.SessionID, s.PID)
		ok, err := automate.StopProcess(s.PID)
		if err != nil {
			console.GlyphWarning.Printf("Error stopping PID %d: %v", s.PID, err)
		}
		_ = automate.RemoveSessionFile(sproutDir, s.SessionID)
		if ok {
			stopped++
		}
	}

	if stopped == 0 {
		console.GlyphInfo.Printf("No running sessions to stop.")
	} else {
		console.GlyphSuccess.Printf("Stopped %d session(s).", stopped)
	}
	return nil
}
