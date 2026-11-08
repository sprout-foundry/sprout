package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ShellPartInfo is a projection of agent.ShellPart that carries only the
// fields the CLI picker needs. It lives in pkg/console to avoid a cyclic
// import (pkg/agent imports pkg/console for the picker; pkg/console
// cannot import pkg/agent).
type ShellPartInfo struct {
	ID        string // stable ID (e.g. "part-0")
	Text      string // raw text of this part
	Kind      string // CommandKind string value
	Semantic  string // human-readable description
	RiskLabel string // short risk-tier label: CRITICAL, HIGH, MEDIUM, LOW
}

// shellPickerSuspend / shellPickerResume bracket the picker's stdin reads
// with the same clihooks suspension askForSecurityApprovalWriter uses —
// without it the CLI's SteerInputReader (raw-mode stdin) and streaming
// output clobber the prompt. Tests swap them to observe call order; the
// resume half runs in LIFO unwind order (streaming, steer, indicator)
// mirroring the deferred-resume order in security_prompt.go.
var (
	shellPickerSuspend = func() {
		clihooks.SuspendIndicator()
		clihooks.PauseSteer()
		clihooks.SuspendStreaming()
	}
	shellPickerResume = func() {
		clihooks.ResumeStreaming()
		clihooks.ResumeSteer()
		clihooks.ResumeIndicator()
	}
)

// PromptShellApprovalParts shows the user one line per part of the shell
// proposal and prompts y/n per part. Returns a decisions map keyed by
// part ID. Supports bulk 'a' (accept all remaining) and 'r' (reject all
// remaining) for fast triage.
//
// The picker is intentionally line-based (not SelectList) so it stays
// testable via io.Reader injection and so it works in non-TTY contexts
// like CI / piped stdin. The arrow-key picker in security_prompt.go is
// reserved for the single 4-option gate.
func PromptShellApprovalParts(ctx context.Context, parts []ShellPartInfo) (map[string]bool, error) {
	release, err := AcquireTerminalQuestion(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	shellPickerSuspend()
	defer shellPickerResume()
	return promptShellApprovalPartsIO(ctx, parts, os.Stdin, os.Stdout)
}

func promptShellApprovalPartsIO(ctx context.Context, parts []ShellPartInfo, in io.Reader, out io.Writer) (map[string]bool, error) {
	decisions := make(map[string]bool, len(parts))
	if len(parts) == 0 {
		return decisions, nil
	}

	fmt.Fprintf(out, "\n  Shell command has %d part(s). Approve each:\n", len(parts))
	fmt.Fprintf(out, "    (y=approve · n=reject · a=approve all remaining · r=reject all remaining · q=quit)\n\n")

	// A poll-gated LineReader owns stdin for the whole prompt: reads are
	// synchronous and leave nothing parked on the descriptor when the
	// prompt returns (a timeout, Ctrl+C, EOF), so the steer reader or the
	// REPL prompt that takes over next sees every byte the user typed.
	// The previous scanner-goroutine reader stayed blocked in Scan() after
	// a timeout and swallowed the next line with its read-ahead buffer.
	fd := -1
	if f, ok := in.(*os.File); ok {
		fd = int(f.Fd())
	}
	lines := utils.NewLineReader(fd, in)

	bulkAccept := false
	bulkReject := false

	readLine := func() (string, error) {
		return lines.ReadLine(ctx, 0)
	}

	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			denyRemaining(decisions, parts, i)
			return decisions, err
		}
		if bulkAccept {
			decisions[part.ID] = true
			continue
		}
		if bulkReject {
			decisions[part.ID] = false
			continue
		}

		fmt.Fprintf(out, "  [%s] %s    [%s]\n", part.ID, part.Text, part.RiskLabel)
		fmt.Fprintf(out, "        %s\n", part.Semantic)
		for {
			fmt.Fprintf(out, "    approve? [y/n/a/r/q]: ")
			choice, readErr := readLine()
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					// EOF — treat as deny for safety on remaining parts.
					denyRemaining(decisions, parts, i)
					return decisions, nil
				}
				fmt.Fprintf(out, "\n  Shell approval interrupted — denying all parts for safety.\n")
				denyRemaining(decisions, parts, i)
				return decisions, readErr
			}
			choice = strings.ToLower(strings.TrimSpace(choice))
			switch choice {
			case "y", "yes":
				decisions[part.ID] = true
				break
			case "n", "no":
				decisions[part.ID] = false
				break
			case "a", "all":
				decisions[part.ID] = true
				bulkAccept = true
				break
			case "r", "reject":
				decisions[part.ID] = false
				bulkReject = true
				break
			case "q", "quit":
				denyRemaining(decisions, parts, i)
				return decisions, nil
			default:
				fmt.Fprintln(out, "      invalid input — type one of: y / n / a / r / q")
				continue
			}
			break
		}
	}
	return decisions, nil
}

// denyRemaining marks parts[from:] as rejected in decisions. Callers use
// it so the agent treats un-answered parts as denied after EOF, quit, or
// context cancellation/timeout.
func denyRemaining(decisions map[string]bool, parts []ShellPartInfo, from int) {
	for j := from; j < len(parts); j++ {
		decisions[parts[j].ID] = false
	}
}
