package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// RewindCommand handles rewinding conversation to a previous turn
type RewindCommand struct{}

func (c *RewindCommand) Name() string {
	return "rewind"
}

func (c *RewindCommand) Description() string {
	return "Rewind conversation to a previous turn"
}

func (c *RewindCommand) Usage() string {
	return strings.Join([]string{
		"/rewind <turn>          Rewind to turn <turn> (0-based), reverting file changes.",
		"/rewind <turn> --no-revert  Rewind without reverting file changes.",
		"/rewind                   Interactive: pick a turn.",
		"",
		"Turn numbers are 0-based. Use /stats to see current turn info.",
		"By default file changes from discarded turns are reverted. Pass",
		"--no-revert to keep your files as-is.",
	}, "\n")
}

func (c *RewindCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	if !chatAgent.HasTurnCheckpoints() {
		return errors.New("no turns available to rewind to")
	}

	var (
		revertFiles = true
		turnArg     string
	)

	for _, arg := range args {
		if arg == "--no-revert" {
			revertFiles = false
		} else if !strings.HasPrefix(arg, "-") {
			turnArg = arg
		}
	}

	// Resolve the target turn. When no turn argument is supplied, show a
	// REPL-safe picker instead of reading raw stdin. The old code reached
	// for bufio.NewReader(os.Stdin).ReadLine(), which fights the REPL's
	// terminal state (cooked mode, scroll regions, status footer).
	// console.NewSelectList drives its own raw-mode setup + teardown and
	// falls back to a numbered stdin prompt in non-TTY contexts, so the
	// command remains scriptable.
	targetIndex, err := resolveRewindTarget(turnArg, chatAgent)
	if err != nil {
		return err
	}

	opts := agent.RewindOptions{
		ToTurnIndex: targetIndex,
		RevertFiles: revertFiles,
	}
	result, err := chatAgent.Rewind(opts)
	if err != nil {
		return fmt.Errorf("rewind failed: %w", err)
	}

	fmt.Printf("\n[rewind] Rewound to turn %d\n", targetIndex)
	fmt.Printf("          Turns discarded: %d\n", result.TurnsDiscarded)
	fmt.Printf("          Messages removed: %d\n", result.MessagesRemoved)
	if len(result.FilesReverted) > 0 {
		fmt.Printf("          Files reverted: %d\n", len(result.FilesReverted))
	}
	if len(result.FilesSkipped) > 0 {
		fmt.Printf("          Files skipped (modified outside agent): %d\n", len(result.FilesSkipped))
	}
	fmt.Printf("          Checkpoints dropped: %d\n", result.CheckpointsDropped)

	return nil
}

// resolveRewindTarget turns an optional turn argument into a 0-based index.
// When turnArg parses as a number it is used directly. When empty, an
// interactive SelectList lets the user pick a turn. Validation of the final
// index is delegated to agent.Rewind, which rejects out-of-range values.
func resolveRewindTarget(turnArg string, chatAgent *agent.Agent) (int, error) {
	if turnArg != "" {
		n, err := strconv.Atoi(turnArg)
		if err != nil {
			return 0, fmt.Errorf("invalid turn number %q: %w", turnArg, err)
		}
		return n, nil
	}
	return promptRewindTurn(chatAgent)
}

// promptRewindTurn builds a SelectList of available turns and returns the
// chosen turn's 0-based index. Returns an error if the user cancels.
func promptRewindTurn(chatAgent *agent.Agent) (int, error) {
	checkpoints := chatAgent.GetTurnCheckpoints()
	if len(checkpoints) == 0 {
		return 0, errors.New("no turns available to rewind to")
	}

	// Render newest-first so the most likely rewind target (the most
	// recent turn) is at the top, matching the /sessions picker convention.
	items := make([]console.SelectItem, 0, len(checkpoints))
	for i := len(checkpoints) - 1; i >= 0; i-- {
		cp := checkpoints[i]
		label := strings.TrimSpace(cp.Summary)
		if label == "" {
			label = fmt.Sprintf("turn %d", i)
		}
		// Collapse to a single line so the picker rows stay tidy.
		label = strings.ReplaceAll(label, "\n", " ")
		const maxLabel = 80
		if len(label) > maxLabel {
			label = label[:maxLabel-1] + "…"
		}
		items = append(items, console.SelectItem{
			Label:  label,
			Detail: fmt.Sprintf("turn %d · msgs [%d..%d]", i, cp.StartIndex, cp.EndIndex),
			// Value carries the 0-based index so the caller doesn't have
			// to reverse-map the display order.
			Value: strconv.Itoa(i),
		})
	}

	picker := console.NewSelectList(console.SelectListOptions{
		Title:      "Rewind to a previous turn (0-based)",
		Items:      items,
		PageSize:   12,
		Searchable: true,
	})
	chosen, ok, err := picker.Run(context.Background())
	if err != nil {
		return 0, fmt.Errorf("rewind picker: %w", err)
	}
	if !ok || chosen == "" {
		return 0, errors.New("rewind cancelled")
	}
	n, err := strconv.Atoi(chosen)
	if err != nil {
		return 0, fmt.Errorf("invalid turn selection %q: %w", chosen, err)
	}
	return n, nil
}
