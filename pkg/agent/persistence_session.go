package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// SessionInfo represents session information with timestamp
type SessionInfo struct {
	SessionID        string    `json:"session_id"`
	LastUpdated      time.Time `json:"last_updated"`
	Name             string    `json:"name"`              // Human-readable session name
	WorkingDirectory string    `json:"working_directory"` // Directory where session was created
	StoragePath      string    `json:"storage_path,omitempty"`
}

type fileInfoDirEntry struct {
	os.FileInfo
}

func (f fileInfoDirEntry) Type() os.FileMode          { return f.Mode().Type() }
func (f fileInfoDirEntry) Info() (os.FileInfo, error) { return f.FileInfo, nil }

func readSessionInfo(path string, d os.DirEntry) (SessionInfo, bool) {
	fileInfo, err := d.Info()
	if err != nil {
		return SessionInfo{}, false
	}

	lastUpdated := fileInfo.ModTime()
	name := ""
	workingDir := ""
	sessionID := strings.TrimSuffix(d.Name(), ".json")
	if strings.HasPrefix(sessionID, legacySessionPrefix) {
		sessionID = strings.TrimPrefix(sessionID, legacySessionPrefix)
	}
	if data, err := os.ReadFile(path); err == nil {
		var state ConversationState
		if err := json.Unmarshal(data, &state); err == nil {
			if !state.LastUpdated.IsZero() {
				lastUpdated = state.LastUpdated
			}
			name = state.Name
			if strings.TrimSpace(state.WorkingDirectory) != "" {
				normalizedWorkingDir, normErr := normalizeWorkingDirectory(state.WorkingDirectory)
				if normErr == nil {
					workingDir = normalizedWorkingDir
				}
			}
			if strings.TrimSpace(state.SessionID) != "" {
				sessionID = strings.TrimSpace(state.SessionID)
			}
		}
	}

	return SessionInfo{
		SessionID:        sessionID,
		LastUpdated:      lastUpdated,
		Name:             name,
		WorkingDirectory: workingDir,
		StoragePath:      path,
	}, true
}

func listSessionFilesForScope(stateDir, workingDir string) ([]string, error) {
	scopeDir := filepath.Join(stateDir, scopedSessionsDirName, workingDirectoryScopeHash(workingDir))
	files := make([]string, 0, 16)

	if entries, err := os.ReadDir(scopeDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			files = append(files, filepath.Join(scopeDir, entry.Name()))
		}
	} else if !os.IsNotExist(err) {
		return nil, agenterrors.Wrap(err, "failed to read scoped session directory")
	}

	// Include any legacy root sessions that explicitly recorded the same working directory.
	if entries, err := os.ReadDir(stateDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			files = append(files, filepath.Join(stateDir, entry.Name()))
		}
	} else {
		return nil, agenterrors.Wrap(err, "failed to read session root directory")
	}

	return files, nil
}

// ListSessionsWithTimestamps returns sessions for the current working directory scope.
func ListSessionsWithTimestamps() ([]SessionInfo, error) {
	workingDir, _ := os.Getwd()
	return ListSessionsWithTimestampsScoped(workingDir)
}

// ListAllSessionsWithTimestamps returns all available sessions across all scopes.
func ListAllSessionsWithTimestamps() ([]SessionInfo, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to get state directory", err)
	}

	var sessions []SessionInfo
	walkErr := filepath.WalkDir(stateDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".json" {
			return nil
		}
		session, ok := readSessionInfo(path, d)
		if ok {
			sessions = append(sessions, session)
		}
		return nil
	})
	if walkErr != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to scan session directory", walkErr)
	}

	// Get current working directory for prioritization
	currentDir, _ := os.Getwd()

	// Sort sessions: current directory first, then by last updated (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		// Always move current directory sessions to top
		iIsCurrent := sessions[i].WorkingDirectory == currentDir
		jIsCurrent := sessions[j].WorkingDirectory == currentDir
		if iIsCurrent != jIsCurrent {
			return iIsCurrent
		}

		// For same directory type, sort by last updated (newest first)
		return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
	})

	return sessions, nil
}

// ListSessionsWithTimestampsScoped returns sessions only for the given working directory scope.
func ListSessionsWithTimestampsScoped(workingDir string) ([]SessionInfo, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to get state directory", err)
	}
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to normalize working directory")
	}

	sessionFiles, err := listSessionFilesForScope(stateDir, cleanWorkingDir)
	if err != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to list session files for scope", err)
	}

	sessions := make([]SessionInfo, 0, len(sessionFiles))
	for _, path := range sessionFiles {
		entry, err := os.Stat(path)
		if err != nil {
			continue
		}
		session, ok := readSessionInfo(path, fileInfoDirEntry{FileInfo: entry})
		if !ok {
			continue
		}
		if strings.TrimSpace(session.WorkingDirectory) != cleanWorkingDir {
			continue
		}
		sessions = append(sessions, session)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
	})
	return sessions, nil
}

// GetSessionPreview returns the first 50 characters of the first user message
func GetSessionPreview(sessionID string) string {
	workingDir, _ := os.Getwd()
	return GetSessionPreviewScoped(sessionID, workingDir)
}

func GetSessionPreviewScoped(sessionID, workingDir string) string {
	stateDir, err := GetStateDir()
	if err != nil {
		return ""
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return ""
	}

	// Find the first user message
	for _, msg := range state.Messages {
		if msg.Role == "user" {
			content := strings.TrimSpace(StripUserMessageTimestamp(msg.Content))
			if content == "" {
				continue
			}
			// Get first 50 characters, clean up whitespace. Truncate at a
			// rune boundary so multi-byte UTF-8 prompts aren't split
			// mid-codepoint.
			runes := []rune(content)
			if len(runes) > 50 {
				content = string(runes[:50]) + "..."
			}
			// Replace newlines with spaces to keep it on one line
			content = strings.ReplaceAll(content, "\n", " ")
			return content
		}
	}

	return ""
}

// GetSessionName returns the name of a session
func GetSessionName(sessionID string) string {
	workingDir, _ := os.Getwd()
	return GetSessionNameScoped(sessionID, workingDir)
}

func GetSessionNameScoped(sessionID, workingDir string) string {
	stateDir, err := GetStateDir()
	if err != nil {
		return ""
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return ""
	}

	return state.Name
}

// RenameSession renames a session by updating the name field in the state file
func RenameSession(sessionID string, newName string) error {
	workingDir, _ := os.Getwd()
	return RenameSessionScoped(sessionID, newName, workingDir)
}

func RenameSessionScoped(sessionID, newName, workingDir string) error {
	stateDir, err := GetStateDir()
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to get state directory", err)
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to resolve session file", err)
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to read session file", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return agenterrors.NewAgent("persistence", "failed to unmarshal state", err)
	}

	// Update the name
	state.Name = newName

	// Write back to file
	newData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to marshal state", err)
	}

	if err := os.WriteFile(stateFile, newData, 0600); err != nil {
		return agenterrors.NewAgent("persistence", "failed to write session file", err)
	}

	return nil
}

// ListSessions returns all available session IDs
func ListSessions() ([]string, error) {
	sessions, err := ListSessionsWithTimestamps()
	if err != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to list sessions", err)
	}

	var sessionIDs []string
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.SessionID)
	}

	return sessionIDs, nil
}

// DeleteSession removes a session state file
func DeleteSession(sessionID string) error {
	workingDir, _ := os.Getwd()
	return DeleteSessionScoped(sessionID, workingDir)
}

func DeleteSessionScoped(sessionID, workingDir string) error {
	stateDir, err := GetStateDir()
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to get state directory", err)
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to resolve session file", err)
	}
	if err := os.Remove(stateFile); err != nil {
		return agenterrors.NewAgent("persistence", fmt.Sprintf("failed to delete session file %q", stateFile), err)
	}
	if err := os.Remove(stateFile + ".bak"); err != nil && !os.IsNotExist(err) {
		return agenterrors.NewAgent("persistence", fmt.Sprintf("failed to delete session backup %q", stateFile+".bak"), err)
	}
	return nil
}

// cleanupMemorySessions removes old sessions for the current working directory scope.
func cleanupMemorySessions() error {
	workingDir, err := os.Getwd()
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to resolve current working directory for session cleanup", err)
	}
	sessions, err := ListSessionsWithTimestampsScoped(workingDir)
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to list sessions", err)
	}

	if len(sessions) <= sessionRetentionLimit {
		return nil // No cleanup needed
	}

	// Sort sessions by last updated (oldest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.Before(sessions[j].LastUpdated)
	})

	// Delete oldest sessions beyond the retention limit for this directory scope.
	for i := 0; i < len(sessions)-sessionRetentionLimit; i++ {
		if err := DeleteSessionScoped(sessions[i].SessionID, sessions[i].WorkingDirectory); err != nil {
			return agenterrors.NewAgent("persistence", fmt.Sprintf("failed to delete session %s", sessions[i].SessionID), err)
		}
	}

	return nil
}
