// Package agent: security accessors and content security checks (split from agent_getters.go)
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/prompts"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// GetUnsafeMode returns whether unsafe mode is enabled.
// Returns false when the security submanager is unset (typical for
// partially-constructed agents in unit tests).
func (a *Agent) GetUnsafeMode() bool {
	if a == nil || a.security == nil {
		return false
	}
	return a.security.GetUnsafeMode()
}

// SetUnsafeMode sets the unsafe mode flag. No-op when the security
// submanager is unset so bare-agent tests don't panic.
func (a *Agent) SetUnsafeMode(unsafe bool) {
	if a == nil || a.security == nil {
		return
	}
	a.security.SetUnsafeMode(unsafe)
}

// GetUnsafeShellMode returns whether unsafe shell mode is enabled.
// Returns false when the security submanager is unset.
func (a *Agent) GetUnsafeShellMode() bool {
	if a == nil || a.security == nil {
		return false
	}
	return a.security.GetUnsafeShellMode()
}

// SetUnsafeShellMode sets the unsafe shell mode flag. No-op when the
// security submanager is unset.
func (a *Agent) SetUnsafeShellMode(unsafe bool) {
	if a == nil || a.security == nil {
		return
	}
	a.security.SetUnsafeShellMode(unsafe)
}

// IsSecurityBypassApproved returns whether the user has approved any
// external filesystem access this session. Coarse signal: prefer the
// per-path IsFolderSessionAllowed for new code.
// Returns false when the security submanager is unset.
func (a *Agent) IsSecurityBypassApproved() bool {
	if a == nil || a.security == nil {
		return false
	}
	return a.security.IsSecurityBypassApproved()
}

// IsFolderSessionAllowed reports whether absPath sits under a folder
// the user has allowlisted via "Allow this folder for the rest of the
// session" on the filesystem approval dialog. Returns false when the
// security submanager is unset.
func (a *Agent) IsFolderSessionAllowed(absPath string) bool {
	if a == nil || a.security == nil {
		return false
	}
	return a.security.IsFolderSessionAllowed(absPath)
}

// IsFolderSessionWriteAllowed reports whether absPath sits under an
// allowlisted folder whose declared mode permits writes. Returns false
// when the security submanager is unset, mirroring the
// IsFolderSessionAllowed contract.
func (a *Agent) IsFolderSessionWriteAllowed(absPath string) bool {
	if a == nil || a.security == nil {
		return false
	}
	return a.security.IsFolderSessionWriteAllowed(absPath)
}

// AddSessionAllowedFolder records the folder picked by the user from
// the filesystem approval dialog so future accesses under it are
// auto-approved for the rest of this session. No-op when the security
// submanager is unset.
func (a *Agent) AddSessionAllowedFolder(folder string) {
	if a == nil || a.security == nil {
		return
	}
	a.security.AddSessionAllowedFolder(folder)
}

// SetSessionAllowedFolderMode records the declared mode for an
// already-allowlisted folder. The folder must already be on the
// session allowlist (call AddSessionAllowedFolder first); passing a
// mode for an unallowlisted folder is a no-op so the mode cannot
// widen access the user never approved. No-op when the security
// submanager is unset.
func (a *Agent) SetSessionAllowedFolderMode(folder, mode string) {
	if a == nil || a.security == nil {
		return
	}
	a.security.SetSessionAllowedFolderMode(folder, mode)
}

// SnapshotSessionAllowedFolders returns a copy of the session
// allowlist. Used by SubagentRunner to seed a new subagent's
// allowlist from the parent (so previously approved folders remain
// usable inside delegated work). Returns nil when the security
// submanager is unset.
func (a *Agent) SnapshotSessionAllowedFolders() []string {
	if a == nil || a.security == nil {
		return nil
	}
	return a.security.SnapshotSessionAllowedFolders()
}

// SnapshotSessionAllowedFolderModes returns a copy of the
// folder-mode map. Used alongside SnapshotSessionAllowedFolders to
// seed a subagent's declared modes so workflow read_only
// constraints survive delegation. Returns nil when the security
// submanager is unset.
func (a *Agent) SnapshotSessionAllowedFolderModes() map[string]string {
	if a == nil || a.security == nil {
		return nil
	}
	return a.security.SnapshotSessionAllowedFolderModes()
}

// CheckFileContentSecurity runs security concern detection on file content after a write.
// In WebUI mode, it uses the event-bus-based ApprovalManager to show a dialog.
// In CLI mode, it falls back to the interactive logger prompt.
// Ignored concerns are tracked per-file so they are not re-prompted.
func (a *Agent) CheckFileContentSecurity(filePath string, content string) {
	promptManager := a.security.GetSecurityApprovalMgr()
	eventBus := a.GetEventBus()

	if promptManager == nil && eventBus == nil {
		return
	}

	concerns, snippets := security.DetectSecurityConcernsWithContext(content, filePath)
	if len(concerns) == 0 {
		return
	}

	logger := utils.GetLogger(false)

	for _, concern := range concerns {
		if a.security.IsConcernIgnored(filePath, concern) {
			continue
		}

		snippet := ""
		if snippets != nil {
			snippet = snippets[concern]
		}
		prompt := prompts.PotentialSecurityConcernsFound(filePath, concern, snippet)

		var userResponse bool

		if eventBus != nil && promptManager != nil && a.security.HasActiveWebUIClients() {
			extras := map[string]string{
				"file_path": filePath,
				"concern":   concern,
			}
			userResponse = promptManager.RequestPrompt(eventBus, a.GetEventUserID(), prompt, true, extras)
			logger.Logf("Security concern '%s' in %s user response: %v", concern, filePath, userResponse)
		} else {
			userResponse = logger.AskForConfirmation(prompt, true, false)
		}

		if userResponse {
			logger.Logf("Security concern '%s' in %s noted as an issue.", concern, filePath)
		} else {
			logger.Logf("Security concern '%s' in %s noted as unimportant.", concern, filePath)
			a.security.SetConcernIgnored(filePath, concern)
		}
	}
}

// GetSecurityApprovalMgr returns the security approval manager. Returns nil
// when the security subsystem is not initialized (e.g., bare &Agent{} in
// tests), so callers can safely nil-check the result.
func (a *Agent) GetSecurityApprovalMgr() *security.ApprovalManager {
	if a.security == nil {
		return nil
	}
	return a.security.GetSecurityApprovalMgr()
}

// SetHasActiveWebUIClients sets a callback that returns whether any WebUI
// clients are currently connected. The security prompting logic uses this
// to decide between WebUI event-bus routing and CLI-based prompting.
func (a *Agent) SetHasActiveWebUIClients(fn func() bool) {
	a.security.SetHasActiveWebUIClients(fn)
}

// HasActiveWebUIClients calls the registered callback (or returns false if
// none is set) to check whether WebUI clients are connected. Returns false
// when the security submanager is unset (typical for partially-constructed
// agents in unit tests).
func (a *Agent) HasActiveWebUIClients() bool {
	if a == nil || a.security == nil {
		return false
	}
	return a.security.HasActiveWebUIClients()
}
