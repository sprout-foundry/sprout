package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// RedactedContentMarker is the canonical marker used when file content is
// redacted because the file is outside the workspace root (to avoid leaking
// sensitive data). It is defined here, in the lower-level history package, so
// both pkg/history and pkg/agent reference a single source of truth instead of
// maintaining duplicate copies that can silently drift. pkg/agent references
// this via history.RedactedContentMarker.
const RedactedContentMarker = "[REDACTED - external file]"

// RevisionGroup represents a group of changes that belong to the same revision
type RevisionGroup struct {
	RevisionID   string
	Instructions string
	Response     string
	Changes      []ChangeLog
	Timestamp    time.Time
	AgentModel   string       // Editing model used for this revision
	Conversation []APIMessage // Full conversation history for multi-turn conversations
}

// APIMessage represents a message in the conversation (imported from agent_api to avoid circular dependency)
type APIMessage struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	ToolCalls        []APIToolCall `json:"tool_calls,omitempty"`
}

// APIToolCall represents a tool call in a message
type APIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// HasActiveChangesForRevision returns whether a revision ID exists and has any active changes
// GetFilesForRevision returns the file paths of all active changes in a revision.
// Returns an empty slice if the revision is not found or has no active changes.
func GetFilesForRevision(revisionID string) ([]string, error) {
	changes, err := fetchAllChanges()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch changes: %w", err)
	}

	revisionGroups := groupChangesByRevision(changes)
	for _, group := range revisionGroups {
		if group.RevisionID == revisionID {
			active := getActiveChanges(group.Changes)
			paths := make([]string, 0, len(active))
			for _, change := range active {
				paths = append(paths, change.Filename)
			}
			return paths, nil
		}
	}

	return nil, nil
}

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

// GetRevisionGroups returns all revision groups sorted by timestamp (most recent first)
func GetRevisionGroups() ([]RevisionGroup, error) {
	changes, err := fetchAllChanges()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch changes: %w", err)
	}

	return groupChangesByRevision(changes), nil
}

// RevertChangeByRevisionID reverts all changes associated with a given revision ID.
func RevertChangeByRevisionID(revisionID string) error {
	changes, err := fetchAllChanges()
	if err != nil {
		return fmt.Errorf("failed to fetch all changes: %w", err)
	}
	if len(changes) == 0 {
		return errors.New("no changes recorded to revert")
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

// PrintRevisionHistoryWithReader allows custom input reader for interactive navigation
func PrintRevisionHistoryWithReader(inputReader *bufio.Reader) error {
	changes, err := fetchAllChanges() // fetchAllChanges now returns sorted data
	if err != nil {
		return fmt.Errorf("fetch all changes: %w", err)
	}
	if len(changes) == 0 {
		fmt.Print("No committed revisions recorded yet.\n")
		return nil
	}

	// Group changes by revision ID
	revisionGroups := groupChangesByRevision(changes)

	if len(revisionGroups) == 0 {
		fmt.Print("No committed revisions recorded yet.\n")
		return nil
	}

	reader := inputReader
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
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
				fmt.Print("Already at the first revision.\n")
			}
		case "d":
			fmt.Print("\n\033[1mAll File Diffs for this Revision:\033[0m\n")
			for _, change := range revisionGroups[currentIndex].Changes {
				fmt.Printf("\n--- Diff for %s ---\n", change.Filename)
				diff := GetDiff(change.Filename, change.OriginalCode, change.NewCode)
				fmt.Print(diff + "\n")
			}
		case "revert":
			activeChanges := getActiveChanges(revisionGroups[currentIndex].Changes)
			if len(activeChanges) > 0 {
				if err := handleRevisionRollback(revisionGroups[currentIndex]); err != nil {
					log.Printf("Error during revision rollback: %v", err)
				}
			} else {
				fmt.Print("No active changes in this revision, cannot revert.\n")
			}
		case "restore":
			if err := handleRevisionRestore(revisionGroups[currentIndex]); err != nil {
				log.Printf("Error during revision restore: %v", err)
			}
		case "p": // Show original prompt
			if revisionGroups[currentIndex].Instructions != "" {
				fmt.Printf("\n\033[1mOriginal Prompt:\033[0m\n%s\n", revisionGroups[currentIndex].Instructions)
			} else {
				fmt.Print("\nNo original prompt recorded.\n")
			}
		case "l": // Show LLM details
			fmt.Printf("\n\033[1mEditing Model:\033[0m %s\n", revisionGroups[currentIndex].AgentModel)
			if revisionGroups[currentIndex].Response != "" {
				fmt.Printf("\n\033[1mFull LLM Response:\033[0m\n%s\n", revisionGroups[currentIndex].Response)
			} else {
				fmt.Print("\nNo LLM response recorded.\n")
			}
		case "":
			// Show next revision
			if currentIndex < len(revisionGroups)-1 {
				currentIndex++
				displayRevision(revisionGroups[currentIndex])
			} else {
				fmt.Print("No more revisions to show.\n")
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
			fmt.Print("Invalid option. Please try again.\n")
		}
	}
}

func PrintRevisionHistory() error {
	return PrintRevisionHistoryWithReader(nil)
}

// PrintRevisionHistoryBuffer displays the revision history to a buffer for seamless console experience
func PrintRevisionHistoryBuffer() (string, error) {
	changes, err := fetchAllChanges() // fetchAllChanges now returns sorted data
	if err != nil {
		return "", fmt.Errorf("failed to fetch changes: %w", err)
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
	fmt.Printf("\r\n\033[1mEditing Model:\033[0m %s\r\n", group.AgentModel)
	fmt.Print(strings.Repeat("=", 80) + "\r\n")
	fmt.Printf("\033[36mRevision ID: %s\033[0m\r\n", group.RevisionID)
	fmt.Printf("Time: %s\r\n", group.Timestamp.Format(time.RFC1123))

	// Display the editing model used for this revision
	if group.AgentModel != "" {
		fmt.Printf("Model: %s\r\n\r\n", group.AgentModel)
	} else {
		fmt.Print("Model: Not specified\r\n\r\n")
	}

	fmt.Printf("\033[1mFile Changes (%d):\033[0m\r\n", len(group.Changes))
	for _, change := range group.Changes {
		fmt.Print(strings.Repeat("-", 40) + "\r\n")
		fmt.Printf("\033[33m(%s)\033[0m", change.Filename)
		fmt.Printf(" -- \033[1m%s\033[0m", change.FileRevisionHash)
		if change.Status != "active" {
			fmt.Printf(" - %s%s%s\r\n", "\033[2m", change.Status, "\033[0m")
		} else {
			fmt.Printf(" - \033[32m%s\033[0m\r\n", change.Status)
		}

		if change.Note.Valid {
			fmt.Printf("    \033[1m%s\033[0m\r\n\r\n", change.Note.String)
		}

		// Wrap the description at 72 characters and indent with 4 spaces
		wrappedDesc := wrapAndIndent(change.Description, 72, 4)
		fmt.Print(wrappedDesc + "\r\n")

		// Show a preview of the diff
		diff := GetDiff(change.Filename, change.OriginalCode, change.NewCode)
		diffLines := strings.Split(diff, "\n")
		if len(diffLines) > 3 {
			for _, line := range diffLines[:3] {
				fmt.Print(line + "\r\n")
			}
			fmt.Print("...\r\n")
		} else {
			for _, line := range diffLines {
				fmt.Print(line + "\r\n")
			}
		}
	}
}

func formatRevision(group RevisionGroup) string {
	var buffer strings.Builder

	buffer.WriteString(fmt.Sprintf("\nEditing Model: %s\n", group.AgentModel))
	buffer.WriteString(strings.Repeat("=", 80) + "\n")
	buffer.WriteString(fmt.Sprintf("Revision ID: %s\n", group.RevisionID))
	buffer.WriteString(fmt.Sprintf("Time: %s\n", group.Timestamp.Format(time.RFC1123)))

	// Display the editing model used for this revision
	if group.AgentModel != "" {
		buffer.WriteString(fmt.Sprintf("Model: %s\n\n", group.AgentModel))
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
			// Load conversation if it exists
			var conversation []APIMessage
			if change.HasConversation {
				conversation = loadConversationForRevision(revisionID)
			}

			groupMap[revisionID] = &RevisionGroup{
				RevisionID:   revisionID,
				Instructions: change.Instructions,
				Response:     change.Response,
				Changes:      []ChangeLog{change},
				Timestamp:    change.Timestamp,
				AgentModel:   change.AgentModel,
				Conversation: conversation,
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

// loadConversationForRevision loads the conversation JSON file for a revision
func loadConversationForRevision(revisionID string) []APIMessage {
	revisionPath := filepath.Join(GetRevisionsDir(), revisionID, "conversation.json")
	conversationBytes, err := filesystem.ReadFileBytes(revisionPath)
	if err != nil {
		// Conversation doesn't exist or couldn't be read
		return nil
	}

	var conversation []APIMessage
	if err := json.Unmarshal(conversationBytes, &conversation); err != nil {
		// Failed to parse conversation
		return nil
	}

	return conversation
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

// isTmpPath reports whether the resolved path lives under the system temp
// directory. macOS resolves /tmp to /private/tmp via a symlink, so both forms
// are checked. This mirrors filesystem.isInTmpPath, which is not exported.
func isTmpPath(path string) bool {
	cleanPath := filepath.Clean(path)
	if strings.HasPrefix(cleanPath, "/tmp/") || cleanPath == "/tmp" ||
		strings.HasPrefix(cleanPath, "/private/tmp/") || cleanPath == "/private/tmp" {
		return true
	}
	// Windows-style temp paths
	lowerPath := strings.ToLower(cleanPath)
	if strings.Contains(lowerPath, "\\temp\\") || strings.Contains(lowerPath, "\\tmp\\") {
		return true
	}
	return false
}

// isWithinWorkspace reports whether the given filename resolves to a path
// inside the current workspace root (determined from os.Getwd()). It is a
// safety guard used by rollback/restore to prevent the history store from
// silently overwriting files outside the project — e.g. committed files whose
// old snapshots are still in the DB.
//
// The history package does not receive an explicit workspace root, so CWD is
// the best available proxy, consistent with how SafeResolvePath in the
// filesystem package falls back to CWD when no root is configured. /tmp paths
// are always allowed (same exception as SafeResolvePath).
//
// Any error during path resolution causes this to return false (skip the
// file) — failing closed is safer than writing to an unexpected location.
func isWithinWorkspace(filename string) bool {
	if filename == "" {
		return false
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}

	cleanPath := filepath.Clean(filename)
	absPath := cleanPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwdAbs, cleanPath)
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return false
	}

	// /tmp is always allowed (same exception as SafeResolvePath).
	if isTmpPath(absPath) {
		return true
	}

	// Resolve symlinks on the file path. The file may not exist yet (rollback
	// can restore a deleted file), so fall back to the parent directory if the
	// file itself cannot be evaluated.
	resolvedAbs, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// Try resolving the parent directory instead (file may not exist).
		resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(absPath))
		if parentErr != nil {
			return false
		}
		resolvedAbs = filepath.Join(resolvedParent, filepath.Base(absPath))
	}

	resolvedCwd, err := filepath.EvalSymlinks(cwdAbs)
	if err != nil {
		return false
	}

	relPath, err := filepath.Rel(resolvedCwd, resolvedAbs)
	if err != nil {
		return false
	}

	// A relative path starting with ".." escapes the workspace root.
	return !strings.HasPrefix(relPath, "..")
}

func handleRevisionRollback(group RevisionGroup) error {
	fmt.Printf("Rolling back all changes in revision %s...\n", group.RevisionID)

	activeChanges := getActiveChanges(group.Changes)
	for _, change := range activeChanges {
		// Skip files with redacted content (external files)
		if change.OriginalCode == RedactedContentMarker {
			fmt.Printf("  Skipping %s: content was redacted (external file)\n", change.Filename)
			continue
		}

		// Safety check: never write to files outside the current workspace.
		// The history DB may contain snapshots of files that were later moved
		// or committed elsewhere; blindly restoring them would clobber
		// intentional changes outside this project.
		if !isWithinWorkspace(change.Filename) {
			fmt.Printf("  Skipping %s: outside current workspace (safety check)\n", change.Filename)
			continue
		}

		fmt.Printf("  Rolling back %s...\n", change.Filename)

		// Write content directly to avoid any encoding transformations
		// Use filesystem.WriteFileWithDir which does raw binary write
		err := filesystem.WriteFileWithDir(change.Filename, []byte(change.OriginalCode), 0644)
		if err != nil {
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
		// Skip files with redacted content (external files)
		if change.NewCode == RedactedContentMarker {
			fmt.Printf("  Skipping %s: content was redacted (external file)\n", change.Filename)
			continue
		}

		// Safety check: never write to files outside the current workspace.
		// See handleRevisionRollback for rationale.
		if !isWithinWorkspace(change.Filename) {
			fmt.Printf("  Skipping %s: outside current workspace (safety check)\n", change.Filename)
			continue
		}

		fmt.Printf("  Restoring %s...\n", change.Filename)

		// Write content directly to avoid any encoding transformations
		err := filesystem.WriteFileWithDir(change.Filename, []byte(change.NewCode), 0644)
		if err != nil {
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
