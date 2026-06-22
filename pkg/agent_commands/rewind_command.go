package commands

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
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
		targetIndex int
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

	if turnArg == "" {
		fmt.Println("Rewind to a previous turn (0-based index). Use /stats for turn info.")
		fmt.Print("Select turn to rewind to: ")
		reader := bufio.NewReader(os.Stdin)
		line, _, err := reader.ReadLine()
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil {
			return fmt.Errorf("invalid turn number %q: %w", string(line), err)
		}
		targetIndex = n
	} else {
		n, err := strconv.Atoi(turnArg)
		if err != nil {
			return fmt.Errorf("invalid turn number %q: %w", turnArg, err)
		}
		targetIndex = n
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
