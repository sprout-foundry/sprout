package console

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
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
// resume half runs in LIFO order (streaming, then steer) like the other
// prompt callers.
var (
	shellPickerSuspend = func() {
		clihooks.SuspendIndicator()
		clihooks.PauseSteer()
		clihooks.SuspendStreaming()
	}
	shellPickerResume = func() {
		clihooks.ResumeStreaming()
		clihooks.ResumeSteer()
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
	shellPickerSuspend()
	defer shellPickerResume()
	return promptShellApprovalPartsIO(ctx, parts, os.Stdin, os.Stdout)
}

// readLineCtx reads one line from scanner, racing the blocking read
// against ctx.Done() so the 30-minute approval timeout (or a user
// interrupt) can abort a pending prompt. On cancellation the outstanding
// read goroutine is left to resolve on its own; the caller must NOT
// continue reading from the same scanner afterwards — this function
// returns the error, so promptShellApprovalPartsIO does not.
func readLineCtx(scanner *bufio.Scanner, ctx context.Context) (string, error) {
	type readResult struct {
		line string
		ok   bool
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		ok := scanner.Scan()
		ch <- readResult{line: scanner.Text(), ok: ok, err: scanner.Err()}
	}()
	select {
	case res := <-ch:
		if !res.ok {
			if res.err != nil {
				return "", res.err
			}
			return "", io.EOF
		}
		return res.line, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func promptShellApprovalPartsIO(ctx context.Context, parts []ShellPartInfo, in io.Reader, out io.Writer) (map[string]bool, error) {
	decisions := make(map[string]bool, len(parts))
	if len(parts) == 0 {
		return decisions, nil
	}

	fmt.Fprintf(out, "\n  Shell command has %d part(s). Approve each:\n", len(parts))
	fmt.Fprintf(out, "    (y=approve · n=reject · a=approve all remaining · r=reject all remaining · q=quit)\n\n")

	scanner := bufio.NewScanner(in)
	bulkAccept := false
	bulkReject := false

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
			choice, err := readLineCtx(scanner, ctx)
			if err != nil {
				if errors.Is(err, io.EOF) {
					// EOF — treat as deny for safety on remaining parts.
					denyRemaining(decisions, parts, i)
					return decisions, nil
				}
				fmt.Fprintf(out, "\n  Shell approval interrupted — denying all parts for safety.\n")
				denyRemaining(decisions, parts, i)
				return decisions, err
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
