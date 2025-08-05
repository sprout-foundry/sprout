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

func PrintRevisionHistory() error {
	changes, err := fetchAllChanges() // fetchAllChanges now returns sorted data
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Println("No changes recorded.")
		return nil
	}

	// Group changes by revision ID
	revisionGroups := groupChangesByRevision(changes)
	
	if len(revisionGroups) == 0 {
		fmt.Println("No revisions found.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	currentIndex := 0

	// Display the first revision
	displayRevision(revisionGroups[currentIndex])

	for {
		fmt.Print("\nEnter: Show next revision | b: Show previous revision | x: Exit | d: Show all diffs | revert: Rollback revision | restore: Restore revision | p: Show original prompt | l: Show LLM details -> ")
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
				fmt.Println("Already at the first revision.")
			}
		case "d":
			fmt.Println("\n\033[1mAll File Diffs for this Revision:\033[0m")
			for _, change := range revisionGroups[currentIndex].Changes {
				fmt.Printf("\n--- Diff for %s ---\n", change.Filename)
				diff := GetDiff(change.Filename, change.OriginalCode, change.NewCode)
				fmt.Println(diff)
			}
		case "revert":
			activeChanges := getActiveChanges(revisionGroups[currentIndex].Changes)
			if len(activeChanges) > 0 {
				if err := handleRevisionRollback(revisionGroups[currentIndex]); err != nil {
					log.Printf("Error during revision rollback: %v", err)
				}
			} else {
				fmt.Println("No active changes in this revision, cannot revert.")
			}
		case "restore":
			if err := handleRevisionRestore(revisionGroups[currentIndex]); err != nil {
				log.Printf("Error during revision restore: %v", err)
			}
		case "p": // Show original prompt
			if revisionGroups[currentIndex].Instructions != "" {
				fmt.Printf("\n\033[1mOriginal Prompt:\033[0m\n%s\n", revisionGroups[currentIndex].Instructions)
			} else {
				fmt.Println("\nNo original prompt recorded.")
			}
		case "l": // Show LLM details
			fmt.Printf("\n\033[1mEditing Model:\033[0m %s\n", revisionGroups[currentIndex].EditingModel)
			if revisionGroups[currentIndex].Response != "" {
				fmt.Printf("\n\033[1mFull LLM Response:\033[0m\n%s\n", revisionGroups[currentIndex].Response)
			} else {
				fmt.Println("\nNo LLM response recorded.")
			}
		case "":
			// Show next revision
			if currentIndex < len(revisionGroups)-1 {
				currentIndex++
				displayRevision(revisionGroups[currentIndex])
			} else {
				fmt.Println("No more revisions to show.")
				fmt.Print("x: Exit | b: Show previous revision -> ")
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
			fmt.Println("Invalid option. Please try again.")
		}
	}
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
