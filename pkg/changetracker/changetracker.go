package changetracker

import (
	"bufio"
	"fmt"
	"ledit/pkg/utils"
	"log"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
)

func PrintRevisionHistory() error {
	changes, err := fetchAllChanges() // fetchAllChanges now returns sorted data
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Println("No changes recorded.")
		return nil
	}
	reader := bufio.NewReader(os.Stdin)

	for _, change := range changes {
		fmt.Println(strings.Repeat("-", 80))
		color.New(color.FgYellow).Printf("(%s)", change.Filename)
		fmt.Printf(" -- \033[1m%s\033[0m", change.FileRevisionHash)
		if change.Status != "active" {
			fmt.Printf(" - %s%s%s\n", "\033[2m", change.Status, "\033[0m")
		} else {
			color.Green(" - %s\n", change.Status)
		}
		fmt.Printf("\033[1mTime:\033[0m %s\n\n", change.Timestamp.Format(time.RFC1123))
		if change.Note.Valid {
			fmt.Printf("    \033[1m%s\033[0m\n\n", change.Note.String)
		}
		// Wrap the description at 100 characters and indent with 4 spaces
		wrappedDesc := wrapAndIndent(change.Description, 72, 4)
		fmt.Print(wrappedDesc + "\n\n")

		diff := GetDiff(change.Filename, change.OriginalCode, change.NewCode)
		diffLines := strings.Split(diff, "\n")
		if len(diffLines) > 5 {
			for _, line := range diffLines[:5] {
				fmt.Println(line)
			}
			fmt.Println("...")
		} else {
			for _, line := range diffLines {
				fmt.Println(line)
			}
		}

		for {
			fmt.Print("\nEnter: Show next | x: Exit | o: Original | u: Updated | d: Full Diff | revert: Rollback | restore: Restore -> ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))

			switch input {
			case "x", "exit":
				return nil
			case "o":
				fmt.Println("\n\033[1mOriginal Code:\033[0m")
				fmt.Println(change.OriginalCode)
			case "u":
				fmt.Println("\n\033[1mUpdated Code:\033[0m")
				fmt.Println(change.NewCode)
			case "d":
				fmt.Println("\n\033[1mFull Diff:\033[0m")
				fmt.Println(diff)
			case "revert":
				if change.Status == "active" {
					if err := handleRollback(change); err != nil {
						log.Printf("Error during rollback: %v", err)
					}
				} else {
					fmt.Println("Change is not active, cannot revert.")
				}
			case "restore":
				if err := handleRestore(change); err != nil {
					log.Printf("Error during restore: %v", err)
				}
			default:
				fmt.Println("Invalid option. Please try again.")
			}
			if input == "" || input == "x" || input == "exit" {
				break
			}
		}
	}
	return nil
}

func handleRollback(change ChangeLog) error {
	fmt.Printf("Rolling back changes for %s...\n", change.Filename)
	if err := utils.SaveFile(change.Filename, change.OriginalCode); err != nil {
		return err
	}
	if err := updateChangeStatus(change.FileRevisionHash, "reverted"); err != nil {
		return err
	}
	fmt.Println("Rollback successful.")
	return nil
}

func handleRestore(change ChangeLog) error {
	fmt.Printf("Restoring changes for %s...\n", change.Filename)
	if err := utils.SaveFile(change.Filename, change.NewCode); err != nil {
		return err
	}
	// Typically restore would be used on a reverted change, but here we just re-apply.
	// The status logic might need refinement based on desired workflow.
	if err := updateChangeStatus(change.FileRevisionHash, "restored"); err != nil {
		return err
	}
	fmt.Println("Restore successful.")
	return nil
}
