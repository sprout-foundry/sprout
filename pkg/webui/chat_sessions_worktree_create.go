//go:build !js

package webui

// chat_sessions_worktree_create.go — the create-in-worktree flow, split out
// of chat_sessions_worktree_api.go. handleAPIChatSessionCreateInWorktree
// creates a git worktree for a branch, opens a chat session bound to it, and
// optionally auto-switches the client workspace into it. sanitizePathComponent
// and unsafePathCharRe derive a safe on-disk worktree directory name from a
// branch.
import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

var unsafePathCharRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// sanitizePathComponent strips characters that are unsafe or confusing in
// file-system directory names, keeping only alphanumerics, hyphens, underscores,
// and dots. This is used to derive a worktree directory name from a git branch.
func sanitizePathComponent(s string) string {
	return unsafePathCharRe.ReplaceAllString(s, "_")
}

// handleAPIChatSessionCreateInWorktree handles POST /api/chat-sessions/create-in-worktree
// Creates a git worktree, creates a new chat session, associates the worktree with the chat,
// and optionally switches the workspace to the worktree.
func (ws *ReactWebServer) handleAPIChatSessionCreateInWorktree(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)
	var req struct {
		Branch              string `json:"branch"`
		BaseRef             string `json:"base_ref,omitempty"`
		Name                string `json:"name,omitempty"`
		AutoSwitchWorkspace bool   `json:"auto_switch_workspace,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	req.Branch = strings.TrimSpace(req.Branch)
	req.Name = strings.TrimSpace(req.Name)

	if req.Branch == "" {
		writeJSONErr(w, http.StatusBadRequest, "branch_name_required", "Branch name is required")
		return
	}

	clientID := ws.resolveClientID(r)
	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	// Validate branch name using git's own validation
	validateCmd := ws.gitCommandForWorkspace(workspaceRoot, "check-ref-format", "--branch", req.Branch)
	if output, err := validateCmd.CombinedOutput(); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_branch_name", fmt.Sprintf("Invalid branch name: %s", strings.TrimSpace(string(output))))
		return
	}

	// Sanitize branch name for use in worktree path (flatten slashes)
	sanitizedBranch := strings.ReplaceAll(req.Branch, "/", "-")
	// Only allow alphanumeric, hyphens, underscores, and dots in the path component
	safeBranch := sanitizePathComponent(sanitizedBranch)
	worktreePath := filepath.Join(filepath.Dir(workspaceRoot), safeBranch+"-worktree")
	var err error
	worktreePath, err = filepathAbsEval(worktreePath)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_worktree_path", fmt.Sprintf("Invalid worktree path: %v", err))
		return
	}

	// Validate the resolved worktree path stays within daemon root
	ws.mutex.RLock()
	daemonRoot := ws.daemonRoot
	ws.mutex.RUnlock()
	if !isWithinWorkspace(worktreePath, daemonRoot) && worktreePath != daemonRoot {
		writeJSONErr(w, http.StatusBadRequest, "path_outside_workspace", "Worktree path must stay within workspace boundary")
		return
	}

	// Check if the worktree path already exists on disk (path collision)
	if _, statErr := os.Stat(worktreePath); statErr == nil {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":         "A worktree already exists at the computed path. Use a different branch name or manually remove the existing worktree first.",
			"code":          "worktree_path_conflict",
			"worktree_path": worktreePath,
		})
		return
	}

	// Create the git worktree
	args := []string{"worktree", "add"}
	if req.BaseRef != "" {
		args = append(args, "-b", req.Branch, worktreePath, req.BaseRef)
	} else {
		args = append(args, "-b", req.Branch, worktreePath)
	}

	cmd := ws.gitCommandForWorkspace(workspaceRoot, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if the error is due to branch already existing
		outputStr := strings.TrimSpace(string(output))
		if strings.Contains(outputStr, "already exists") || strings.Contains(outputStr, "ref already exists") {
			// Clean up any partial worktree directory that may have been created
			if removeErr := ws.gitCommandForWorkspace(workspaceRoot, "worktree", "remove", "--force", worktreePath).Run(); removeErr != nil {
				// Also try to remove the directory if git worktree remove failed
				_ = os.RemoveAll(worktreePath)
			}
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":         fmt.Sprintf("Branch '%s' already exists", req.Branch),
				"code":          "branch_exists",
				"worktree_path": worktreePath,
			})
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_create_worktree", fmt.Sprintf("Failed to create worktree: %v\nOutput: %s", err, outputStr))
		return
	}

	// Generate a unique chat ID
	chatID := generateChatID()
	name := req.Name

	// Atomically generate name (if needed) and create the chat session
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(clientID)
	ctx.ensureDefaultChatSession()

	if name == "" {
		ctx.nextChatNumber++
		name = "Chat " + strconv.Itoa(ctx.nextChatNumber)
	}

	// Check if a session with this ID already exists
	if _, ok := ctx.ChatSessions[chatID]; ok {
		ws.mutex.Unlock()
		// Clean up the orphan worktree that was created before the conflict
		if removeErr := ws.gitCommandForWorkspace(workspaceRoot, "worktree", "remove", "--force", worktreePath).Run(); removeErr != nil {
			ws.log().Warn("failed to clean up orphan worktree", slog.String("worktree_path", worktreePath), slog.Any("err", removeErr))
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Chat session with this ID already exists",
			"code":  "chat_session_exists",
			"id":    chatID,
		})
		return
	}

	cs := newChatSession(chatID, name)
	ctx.ChatSessions[chatID] = cs
	ctx.markChatCreated(chatID)
	cs.setWorktreePath(worktreePath)

	// Optionally switch the workspace root to the worktree.
	// We update WorkspaceRoot directly instead of calling setClientWorkspaceRoot
	// because setClientWorkspaceRoot resets all chat sessions, which would
	// destroy the session we just created.
	previousWorkspaceRoot := ctx.WorkspaceRoot
	if req.AutoSwitchWorkspace {
		ctx.WorkspaceRoot = worktreePath
		if clientID == defaultWebClientID {
			ws.workspaceRoot = worktreePath
		} else {
			ws.rememberClientWorkspacesLocked()
		}
		// Clear transient state (agent, terminals) like other workspace-switch handlers.
		ctx.Agent = nil
		ctx.Terminal = nil
	}

	// Capture response data while still holding the lock
	chatSession := cs.chatSessionWithMessages()
	newWorkspaceRoot := ctx.WorkspaceRoot
	ws.mutex.Unlock()

	// Notify frontend of workspace change if auto-switched.
	if req.AutoSwitchWorkspace {
		ws.publishClientEvent(clientID, events.EventTypeWorkspaceChanged, map[string]interface{}{
			"daemon_root":             ws.GetDaemonRoot(),
			"workspace_root":          worktreePath,
			"previous_workspace_root": previousWorkspaceRoot,
			"source":                  "worktree_switch",
		})
	}

	ws.log().Info("created chat session with worktree",
		slog.String("chat_id", chatID),
		slog.String("name", name),
		slog.String("worktree_path", worktreePath),
		slog.String("client_id", clientID))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":        "Chat session created in worktree",
		"chat_session":   chatSession,
		"worktree_path":  worktreePath,
		"branch":         req.Branch,
		"workspace_root": newWorkspaceRoot,
	})
}
