package console

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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
	return promptShellApprovalPartsIO(ctx, parts, os.Stdin, os.Stdout)
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

	for _, part := range parts {
		if err := ctx.Err(); err != nil {
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
			if !scanner.Scan() {
				// EOF — treat as deny for safety on remaining parts.
				decisions[part.ID] = false
				for _, remaining := range parts {
					if _, ok := decisions[remaining.ID]; !ok {
						decisions[remaining.ID] = false
					}
				}
				return decisions, nil
			}
			choice := strings.ToLower(strings.TrimSpace(scanner.Text()))
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
				decisions[part.ID] = false
				for _, remaining := range parts {
					if _, ok := decisions[remaining.ID]; !ok {
						decisions[remaining.ID] = false
					}
				}
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
