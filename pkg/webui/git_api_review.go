//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// handleAPIGitDeepReview reviews the staged change with reviewer subagents
// (the same review_changes pipeline as /review-deep) and returns structured
// MUST_FIX/VERIFY guidance for the review tab's fix picker. It bypasses
// /api/query so the review doesn't enter chat history.
func (ws *ReactWebServer) handleAPIGitDeepReview(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_not_available", "Agent is not available")
		return
	}

	// Exit code 1 means staged changes exist.
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--quiet", "--exit-code")
	if err := checkCmd.Run(); err == nil {
		writeJSONErr(w, http.StatusBadRequest, "no_staged_changes", "No staged changes found")
		return
	}

	review, err := agentInst.ReviewChanges(r.Context(), agent.ReviewChangesOptions{Scope: "staged", Dir: workspaceRoot})
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "deep_review_failed", fmt.Sprintf("Review failed: %v", err))
		return
	}

	status := map[string]string{"APPROVE": "approved", "CHANGES_REQUIRED": "needs_revision"}[review.Verdict]
	if status == "" {
		status = "inconclusive"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":           "Review completed",
		"status":            status,
		"feedback":          review.Summary(),
		"detailed_guidance": review.GuidanceJSON(),
		"review_output":     review.Markdown(),
		"provider":          agentInst.GetProvider(),
		"model":             agentInst.GetModel(),
	})
}

// handleAPIGitDeepReviewFix runs the fix workflow and blocks until completion (legacy API).
func (ws *ReactWebServer) handleAPIGitDeepReviewFix(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		ReviewOutput  string   `json:"review_output"`
		FixPrompt     string   `json:"fix_prompt"`
		SelectedItems []string `json:"selected_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	reviewOutput := strings.TrimSpace(req.ReviewOutput)
	if reviewOutput == "" {
		writeJSONErr(w, http.StatusBadRequest, "review_output_required", "review_output is required")
		return
	}

	job, _, err := ws.startFixReviewJob(reviewOutput, ws.resolveClientID(r), req.FixPrompt, req.SelectedItems)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_start_fix_workflow", fmt.Sprintf("Failed to start fix workflow: %v", err))
		return
	}

	for {
		status, _, _, result, jobErr := job.snapshot(0)
		if status == "completed" {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"message":    "Fix workflow completed",
				"result":     strings.TrimSpace(result),
				"session_id": job.SessionID,
			})
			return
		}
		if status == "error" {
			writeJSONErr(w, http.StatusInternalServerError, "failed_to_run_fix_workflow", fmt.Sprintf("Failed to run fix workflow: %s", jobErr))
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// handleAPIGitDeepReviewFixStart starts an isolated full-agent fix workflow job.
func (ws *ReactWebServer) handleAPIGitDeepReviewFixStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		ReviewOutput  string   `json:"review_output"`
		FixPrompt     string   `json:"fix_prompt"`
		SelectedItems []string `json:"selected_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	reviewOutput := strings.TrimSpace(req.ReviewOutput)
	if reviewOutput == "" {
		writeJSONErr(w, http.StatusBadRequest, "review_output_required", "review_output is required")
		return
	}

	job, _, err := ws.startFixReviewJob(reviewOutput, ws.resolveClientID(r), req.FixPrompt, req.SelectedItems)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_start_fix_workflow", fmt.Sprintf("Failed to start fix workflow: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":    "Fix workflow started",
		"job_id":     job.ID,
		"session_id": job.SessionID,
	})
}

// handleAPIGitDeepReviewFixStatus returns incremental status/logs for a running fix workflow job.
func (ws *ReactWebServer) handleAPIGitDeepReviewFixStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	jobID := strings.TrimSpace(r.URL.Query().Get("job_id"))
	if jobID == "" {
		writeJSONErr(w, http.StatusBadRequest, "job_id_required", "job_id is required")
		return
	}

	since := 0
	if rawSince := strings.TrimSpace(r.URL.Query().Get("since")); rawSince != "" {
		_, _ = fmt.Sscanf(rawSince, "%d", &since)
		if since < 0 {
			since = 0
		}
	}

	ws.fixReviewMu.RLock()
	job, ok := ws.fixReviewJobs[jobID]
	ws.fixReviewMu.RUnlock()
	if !ok {
		writeJSONErr(w, http.StatusNotFound, "job_not_found", "job not found")
		return
	}

	// Authorization: only the client that started the job can query its status.
	// Jobs with an empty ClientID pre-date access control (backward compat)
	// and are accessible by any client. No new jobs should have empty ClientID.
	requestClientID := ws.resolveClientID(r)
	if job.ClientID != "" && job.ClientID != requestClientID {
		writeJSONErr(w, http.StatusNotFound, "job_not_found", "job not found")
		return
	}

	status, logs, next, result, jobErr := job.snapshot(since)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":    "success",
		"job_id":     job.ID,
		"session_id": job.SessionID,
		"status":     status,
		"logs":       logs,
		"next_index": next,
		"result":     result,
		"error":      jobErr,
	})
}

func (ws *ReactWebServer) startFixReviewJob(reviewOutput, clientID, fixPrompt string, selectedItems []string) (*gitFixReviewJob, string, error) {
	var prompt string
	if len(selectedItems) > 0 {
		selectedSection := strings.Join(selectedItems, "\n\n")
		prompt = fmt.Sprintf("Use these selected review items as input:\n\n%s", selectedSection)
		if strings.TrimSpace(fixPrompt) != "" {
			prompt += fmt.Sprintf("\n\nAdditional instructions from the user:\n%s", fixPrompt)
		}
		prompt += "\n\nFirst validate that each of these selected review items is a valid issue, then use subagents to address the valid ones. When resolved, use a code review subagent to review the solution and iterate until the issues are resolved."
	} else {
		fixInstructions := "First validate that all of these review items are valid issues, then use subagents to address any of the valid issues. When they are resolved, use a code review subagent to review the solution and fix any issues that come out of it and iterate through the process until the issues are resolved."
		prompt = fmt.Sprintf("Use this review output as input:\n\n%s\n\n%s", reviewOutput, fixInstructions)
		if strings.TrimSpace(fixPrompt) != "" {
			prompt += fmt.Sprintf("\n\nAdditional instructions from the user:\n%s", fixPrompt)
		}
	}

	provider := ""
	model := ""
	workspaceRoot := ""
	if agentInst, err := ws.getClientAgent(clientID); err == nil && agentInst != nil {
		provider = strings.TrimSpace(agentInst.GetProvider())
		model = strings.TrimSpace(agentInst.GetModel())
		workspaceRoot = agentInst.GetWorkspaceRoot()
	}
	// Fallback: if getClientAgent failed or returned empty workspace root,
	// resolve from the client context directly.
	if strings.TrimSpace(workspaceRoot) == "" {
		if clientCtx := ws.getOrCreateClientContext(clientID); clientCtx != nil {
			workspaceRoot = clientCtx.WorkspaceRoot
		}
	}

	jobID := generateCryptoID("rfx")
	sessionID := generateCryptoID("rfxs")
	job := &gitFixReviewJob{
		ID:            jobID,
		SessionID:     sessionID,
		ClientID:      clientID,
		WorkspaceRoot: workspaceRoot,
		Status:        "running",
		Logs:          []string{"Starting isolated fix session..."},
		StartedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	ws.fixReviewMu.Lock()
	ws.fixReviewJobs[jobID] = job
	ws.fixReviewMu.Unlock()

	go ws.runFixReviewJob(job, prompt, provider, model, workspaceRoot)

	return job, prompt, nil
}

func (ws *ReactWebServer) runFixReviewJob(job *gitFixReviewJob, prompt, provider, model, workspaceRoot string) {
	// When running in daemon mode, the process CWD is the daemon's directory, not the
	// client's workspace. Many downstream code paths (agent init, SaveState, context
	// discovery, git operations) rely on os.Getwd(), so we must switch to the correct
	// workspace directory before creating the agent.
	// Use withAgentWorkspace to serialize CWD changes via workspaceExecMu, avoiding
	// races with other goroutines that also change CWD (e.g. withAgentWorkspace calls
	// from getClientAgent / regular chat).
	workspaceRoot = strings.TrimSpace(workspaceRoot)

	if workspaceRoot == "" {
		job.setError("No workspace root resolved; cannot run fix review in daemon mode. " +
			"Ensure the browser has set a workspace before triggering fix review.")
		return
	}

	err := ws.withAgentWorkspace(workspaceRoot, func() error {
		job.appendLog(fmt.Sprintf("Changed CWD to workspace: %s", workspaceRoot))

		reviewAgent, agentErr := agent.NewAgentWithModel("")
		if agentErr != nil {
			job.setError(fmt.Sprintf("Failed to initialize isolated agent: %v", agentErr))
			return agentErr
		}
		defer reviewAgent.Shutdown()

		reviewAgent.SetWorkspaceRoot(workspaceRoot)

		reviewAgent.SetSessionID(job.SessionID)
		if p := strings.TrimSpace(provider); p != "" {
			if err := reviewAgent.SetProvider(api.ClientType(p)); err != nil {
				job.appendLog(fmt.Sprintf("Warning: failed to set provider %s: %v", p, err))
			}
		}
		if m := strings.TrimSpace(model); m != "" {
			if err := reviewAgent.SetModel(m); err != nil {
				job.appendLog(fmt.Sprintf("Warning: failed to set model %s: %v", m, err))
			}
		}

		reviewAgent.SetStreamingEnabled(true)
		reviewAgent.SetStreamingCallback(func(text string) {
			job.appendStreamText(text)
		})

		job.appendLog("Running fix workflow with full agentic path...")
		result, procErr := reviewAgent.ProcessQuery(prompt)
		job.flushStreamBuffer()
		if procErr != nil {
			job.setError(procErr.Error())
			return procErr
		}
		job.setCompleted(strings.TrimSpace(result))
		return nil
	})

	// If withAgentWorkspace itself failed (e.g. CWD change error) and we haven't
	// already recorded a more specific error via job.setError, record it now.
	if err != nil && job.Status == "running" {
		job.setError(fmt.Sprintf("Workspace setup failed: %v", err))
	}
}

func (j *gitFixReviewJob) appendLog(line string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.Logs = append(j.Logs, line)
	if len(j.Logs) > 2000 {
		j.Logs = j.Logs[len(j.Logs)-2000:]
	}
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) appendStreamText(text string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	j.streamBuf.WriteString(text)
	raw := j.streamBuf.String()
	if raw == "" {
		return
	}

	parts := strings.Split(raw, "\n")
	for _, part := range parts[:len(parts)-1] {
		line := strings.TrimSpace(part)
		if line == "" {
			continue
		}
		j.Logs = append(j.Logs, line)
	}

	j.streamBuf.Reset()
	j.streamBuf.WriteString(parts[len(parts)-1])
	if len(j.Logs) > 2000 {
		j.Logs = j.Logs[len(j.Logs)-2000:]
	}
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) flushStreamBuffer() {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	line := strings.TrimSpace(j.streamBuf.String())
	j.streamBuf.Reset()
	if line == "" {
		return
	}
	j.Logs = append(j.Logs, line)
	if len(j.Logs) > 2000 {
		j.Logs = j.Logs[len(j.Logs)-2000:]
	}
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) setCompleted(result string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.Status = "completed"
	j.Result = result
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) setError(err string) {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.Status = "error"
	j.Error = strings.TrimSpace(err)
	if j.Error == "" {
		j.Error = "Unknown error"
	}
	j.UpdatedAt = time.Now()
}

func (j *gitFixReviewJob) snapshot(since int) (status string, logs []string, nextIndex int, result string, err string) {
	j.mutex.RLock()
	defer j.mutex.RUnlock()

	total := len(j.Logs)
	if since < 0 {
		since = 0
	}
	if since > total {
		since = total
	}
	chunk := make([]string, total-since)
	copy(chunk, j.Logs[since:])

	return j.Status, chunk, total, j.Result, j.Error
}
