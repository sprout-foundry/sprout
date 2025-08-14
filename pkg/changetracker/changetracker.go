package changetracker

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/filesystem"
	ui "github.com/alantheprice/ledit/pkg/ui"

	"github.com/fatih/color"
)

// RevisionGroup represents a group of changes that belong to the same revision
type RevisionGroup struct {
	RevisionID   string
	Instructions string
	Response     string
	Changes      []ChangeLog
	Timestamp    time.Time
	EditingModel string // Added: Editing model used for this revision
}

// HasActiveChangesForRevision returns whether a revision ID exists and has any active changes
func HasActiveChangesForRevision(revisionID string) (bool, error) {
	changes, err := fetchAllChanges()
	if err != nil {
		return false, fmt.Errorf("failed to fetch all changes: %w", err)
	}
	if len(changes) == 0 {
		return false, nil
	}
	revisionGroups := groupChangesByRevision(changes)
	for i := range revisionGroups {
		if revisionGroups[i].RevisionID == revisionID {
			active := getActiveChanges(revisionGroups[i].Changes)
			return len(active) > 0, nil
		}
	}
	return false, nil
}

// RevertChangeByRevisionID reverts all changes associated with a given revision ID.
func RevertChangeByRevisionID(revisionID string) error {
	changes, err := fetchAllChanges()
	if err != nil {
		return fmt.Errorf("failed to fetch all changes: %w", err)
	}
	if len(changes) == 0 {
		return fmt.Errorf("no changes recorded to revert")
	}

	revisionGroups := groupChangesByRevision(changes)

	var targetGroup *RevisionGroup
	for i := range revisionGroups {
		if revisionGroups[i].RevisionID == revisionID {
			targetGroup = &revisionGroups[i]
			break
		}
	}

	if targetGroup == nil {
		return fmt.Errorf("revision ID '%s' not found", revisionID)
	}

	activeChanges := getActiveChanges(targetGroup.Changes)
	if len(activeChanges) == 0 {
		return fmt.Errorf("no active changes found for revision ID '%s' to revert", revisionID)
	}

	if err := handleRevisionRollback(*targetGroup); err != nil {
		return fmt.Errorf("error during revision rollback for ID '%s': %w", revisionID, err)
	}

	return nil
}

func PrintRevisionHistory() error {
	changes, err := fetchAllChanges() // fetchAllChanges now returns sorted data
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		ui.Out().Print("No changes recorded.\n")
		return nil
	}

	// Group changes by revision ID
	revisionGroups := groupChangesByRevision(changes)

	if len(revisionGroups) == 0 {
		ui.Out().Print("No revisions found.\n")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	currentIndex := 0

	// Display the first revision
	displayRevision(revisionGroups[currentIndex])

	for {
		ui.Out().Print("\nEnter: Show next revision | b: Show previous revision | x: Exit | d: Show all diffs | revert: Rollback revision | restore: Restore revision | p: Show original prompt | l: Show LLM details -> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "x", "exit":
			return nil
		case "b", "back":
			if currentIndex > 0 {
				currentIndex--
				displayRevision(revisionGroups[currentIndex])
			} else {
				ui.Out().Print("Already at the first revision.\n")
			}
		case "d":
			ui.Out().Print("\n\033[1mAll File Diffs for this Revision:\033[0m\n")
			for _, change := range revisionGroups[currentIndex].Changes {
				ui.Out().Printf("\n--- Diff for %s ---\n", change.Filename)
				diff := GetDiff(change.Filename, change.OriginalCode, change.NewCode)
				ui.Out().Print(diff + "\n")
			}
		case "revert":
			activeChanges := getActiveChanges(revisionGroups[currentIndex].Changes)
			if len(activeChanges) > 0 {
				if err := handleRevisionRollback(revisionGroups[currentIndex]); err != nil {
					log.Printf("Error during revision rollback: %v", err)
				}
			} else {
				ui.Out().Print("No active changes in this revision, cannot revert.\n")
			}
		case "restore":
			if err := handleRevisionRestore(revisionGroups[currentIndex]); err != nil {
				log.Printf("Error during revision restore: %v", err)
			}
		case "p": // Show original prompt
			if revisionGroups[currentIndex].Instructions != "" {
				ui.Out().Printf("\n\033[1mOriginal Prompt:\033[0m\n%s\n", revisionGroups[currentIndex].Instructions)
			} else {
				ui.Out().Print("\nNo original prompt recorded.\n")
			}
		case "l": // Show LLM details
			ui.Out().Printf("\n\033[1mEditing Model:\033[0m %s\n", revisionGroups[currentIndex].EditingModel)
			if revisionGroups[currentIndex].Response != "" {
				ui.Out().Printf("\n\033[1mFull LLM Response:\033[0m\n%s\n", revisionGroups[currentIndex].Response)
			} else {
				ui.Out().Print("\nNo LLM response recorded.\n")
			}
		case "":
			// Show next revision
			if currentIndex < len(revisionGroups)-1 {
				currentIndex++
				displayRevision(revisionGroups[currentIndex])
			} else {
				ui.Out().Print("No more revisions to show.\n")
				ui.Out().Print("x: Exit | b: Show previous revision -> ")
				exitInput, _ := reader.ReadString('\n')
				exitInput = strings.TrimSpace(strings.ToLower(exitInput))
				if exitInput == "x" || exitInput == "exit" {
					return nil
				} else if exitInput == "b" || exitInput == "back" {
					if currentIndex > 0 {
						currentIndex--
						displayRevision(revisionGroups[currentIndex])
					}
				}
			}
		default:
			ui.Out().Print("Invalid option. Please try again.\n")
		}
	}
}

// PrintRevisionHistoryBuffer displays the revision history to a buffer for seamless console experience
func PrintRevisionHistoryBuffer() (string, error) {
	changes, err := fetchAllChanges() // fetchAllChanges now returns sorted data
	if err != nil {
		return "", err
	}
	if len(changes) == 0 {
		return "No changes recorded.\n", nil
	}

	// Group changes by revision ID
	revisionGroups := groupChangesByRevision(changes)

	if len(revisionGroups) == 0 {
		return "No revisions found.\n", nil
	}

	var buffer strings.Builder

	// Display all revisions in the buffer
	for i, group := range revisionGroups {
		if i > 0 {
			buffer.WriteString("\n" + strings.Repeat("-", 80) + "\n\n")
		}
		buffer.WriteString(formatRevision(group))
	}

	return buffer.String(), nil
}

func displayRevision(group RevisionGroup) {
	fmt.Printf("\n\033[1mEditing Model:\033[0m %s\n", group.EditingModel)
	fmt.Println(strings.Repeat("=", 80))
	color.New(color.FgCyan).Printf("Revision ID: %s\n", group.RevisionID)
	fmt.Printf("Time: %s\n", group.Timestamp.Format(time.RFC1123))

	// Display the editing model used for this revision
	if group.EditingModel != "" {
		fmt.Printf("Model: %s\n\n", group.EditingModel)
	} else {
		fmt.Printf("Model: Not specified\n\n")
	}

	fmt.Printf("\033[1mFile Changes (%d):\033[0m\n", len(group.Changes))
	for _, change := range group.Changes {
		fmt.Println(strings.Repeat("-", 40))
		color.New(color.FgYellow).Printf("(%s)", change.Filename)
		fmt.Printf(" -- \033[1m%s\033[0m", change.FileRevisionHash)
		if change.Status != "active" {
			fmt.Printf(" - %s%s%s\n", "\033[2m", change.Status, "\033[0m")
		} else {
			color.Green(" - %s\n", change.Status)
		}

		if change.Note.Valid {
			fmt.Printf("    \033[1m%s\033[0m\n\n", change.Note.String)
		}

		// Wrap the description at 72 characters and indent with 4 spaces
		wrappedDesc := wrapAndIndent(change.Description, 72, 4)
		fmt.Print(wrappedDesc + "\n")

		// Show a preview of the diff
		diff := GetDiff(change.Filename, change.OriginalCode, change.NewCode)
		diffLines := strings.Split(diff, "\n")
		if len(diffLines) > 3 {
			for _, line := range diffLines[:3] {
				fmt.Println(line)
			}
			fmt.Println("...")
		} else {
			for _, line := range diffLines {
				fmt.Println(line)
			}
		}
	}
}

func formatRevision(group RevisionGroup) string {
	var buffer strings.Builder

	buffer.WriteString(fmt.Sprintf("\nEditing Model: %s\n", group.EditingModel))
	buffer.WriteString(strings.Repeat("=", 80) + "\n")
	buffer.WriteString(fmt.Sprintf("Revision ID: %s\n", group.RevisionID))
	buffer.WriteString(fmt.Sprintf("Time: %s\n", group.Timestamp.Format(time.RFC1123)))

	// Display the editing model used for this revision
	if group.EditingModel != "" {
		buffer.WriteString(fmt.Sprintf("Model: %s\n\n", group.EditingModel))
	} else {
		buffer.WriteString("Model: Not specified\n\n")
	}

	buffer.WriteString(fmt.Sprintf("File Changes (%d):\n", len(group.Changes)))
	for _, change := range group.Changes {
		buffer.WriteString(strings.Repeat("-", 40) + "\n")
		buffer.WriteString(fmt.Sprintf("(%s)", change.Filename))
		buffer.WriteString(fmt.Sprintf(" -- %s", change.FileRevisionHash))
		if change.Status != "active" {
			buffer.WriteString(fmt.Sprintf(" - %s\n", change.Status))
		} else {
			buffer.WriteString(fmt.Sprintf(" - %s\n", change.Status))
		}

		if change.Note.Valid {
			buffer.WriteString(fmt.Sprintf("    %s\n\n", change.Note.String))
		}

		// Wrap the description at 72 characters and indent with 4 spaces
		wrappedDesc := wrapAndIndent(change.Description, 72, 4)
		buffer.WriteString(wrappedDesc + "\n")

		// Show a preview of the diff
		diff := GetDiff(change.Filename, change.OriginalCode, change.NewCode)
		diffLines := strings.Split(diff, "\n")
		if len(diffLines) > 3 {
			for _, line := range diffLines[:3] {
				buffer.WriteString(line + "\n")
			}
			buffer.WriteString("...\n")
		} else {
			for _, line := range diffLines {
				buffer.WriteString(line + "\n")
			}
		}
	}

	return buffer.String()
}

func groupChangesByRevision(changes []ChangeLog) []RevisionGroup {
	// Group changes by RequestHash (revision ID)
	groupMap := make(map[string]*RevisionGroup)

	for _, change := range changes {
		revisionID := change.RequestHash
		if group, exists := groupMap[revisionID]; exists {
			group.Changes = append(group.Changes, change)
			// Keep the earliest timestamp for the group
			if change.Timestamp.Before(group.Timestamp) {
				group.Timestamp = change.Timestamp
			}
		} else {
			groupMap[revisionID] = &RevisionGroup{
				RevisionID:   revisionID,
				Instructions: change.Instructions,
				Response:     change.Response,
				Changes:      []ChangeLog{change},
				Timestamp:    change.Timestamp,
				EditingModel: change.EditingModel, // Added: Include editing model in group
			}
		}
	}

	// Convert map to slice
	var groups []RevisionGroup
	for _, group := range groupMap {
		// Sort changes within each group by timestamp
		sortChangesByTimestamp(group.Changes)
		groups = append(groups, *group)
	}

	// Sort groups by timestamp in descending order (most recent first)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Timestamp.After(groups[j].Timestamp)
	})

	return groups
}

func sortChangesByTimestamp(changes []ChangeLog) {
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Timestamp.After(changes[j].Timestamp)
	})
}

func getActiveChanges(changes []ChangeLog) []ChangeLog {
	var active []ChangeLog
	for _, change := range changes {
		if change.Status == "active" {
			active = append(active, change)
		}
	}
	return active
}

func handleRevisionRollback(group RevisionGroup) error {
	fmt.Printf("Rolling back all changes in revision %s...\n", group.RevisionID)

	activeChanges := getActiveChanges(group.Changes)
	for _, change := range activeChanges {
		fmt.Printf("  Rolling back %s...\n", change.Filename)
		if err := filesystem.SaveFile(change.Filename, change.OriginalCode); err != nil {
			return fmt.Errorf("failed to rollback %s: %w", change.Filename, err)
		}
		if err := updateChangeStatus(change.FileRevisionHash, "reverted"); err != nil {
			return fmt.Errorf("failed to update status for %s: %w", change.Filename, err)
		}
	}

	fmt.Println("Revision rollback successful.")
	return nil
}

func handleRevisionRestore(group RevisionGroup) error {
	fmt.Printf("Restoring all changes in revision %s...\n", group.RevisionID)

	for _, change := range group.Changes {
		fmt.Printf("  Restoring %s...\n", change.Filename)
		if err := filesystem.SaveFile(change.Filename, change.NewCode); err != nil {
			return fmt.Errorf("failed to restore %s: %w", change.Filename, err)
		}
		// Update status to restored regardless of previous status
		if err := updateChangeStatus(change.FileRevisionHash, "restored"); err != nil {
			return fmt.Errorf("failed to update status for %s: %w", change.Filename, err)
		}
	}

	fmt.Println("Revision restore successful.")
	return nil
}
