package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/utils"
)

// handleAPIGitDeepReview performs the same deep staged review flow as /review-deep,
// but without routing through /api/query so it doesn't pollute chat history.
func (ws *ReactWebServer) handleAPIGitDeepReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		http.Error(w, "Agent is not available", http.StatusServiceUnavailable)
		return
	}

	// Exit code 1 means staged changes exist.
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--quiet", "--exit-code")
	if err := checkCmd.Run(); err == nil {
		http.Error(w, "No staged changes found", http.StatusBadRequest)
		return
	}

	diffCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached")
	stagedDiffBytes, err := diffCmd.Output()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get staged diff: %v", err), http.StatusInternalServerError)
		return
	}

	stagedDiff := string(stagedDiffBytes)
	stagedDiff = truncateDiffOutput(stagedDiff, 200000)
	if strings.TrimSpace(stagedDiff) == "" {
		http.Error(w, "No actual diff content found in staged changes", http.StatusBadRequest)
		return
	}

	cfg, err := configuration.LoadOrInitConfig(true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}

	logger := utils.GetLogger(true)
	optimizer := utils.NewDiffOptimizerForReview()
	optimizer.WorkingDir = workspaceRoot
	optimizedDiff := optimizer.OptimizeDiff(stagedDiff)

	service := codereview.NewCodeReviewService(cfg, logger)
	agentClient := service.GetDefaultAgentClient()

	activeProvider := strings.TrimSpace(agentInst.GetProvider())
	activeModel := strings.TrimSpace(agentInst.GetModel())
	if activeProvider != "" {
		if sessionClient, err := factory.CreateProviderClient(api.ClientType(activeProvider), activeModel); err == nil {
			agentClient = sessionClient
		}
	}

	if agentClient == nil {
		http.Error(w, "Failed to initialize review client", http.StatusInternalServerError)
		return
	}

	reviewCtx := &codereview.ReviewContext{
		Diff:             optimizedDiff.OptimizedContent,
		Config:           cfg,
		Logger:           logger,
		AgentClient:      agentClient,
		ProjectType:      ws.gitReviewDetectProjectType(workspaceRoot),
		CommitMessage:    ws.gitReviewExtractStagedChangesSummary(workspaceRoot),
		KeyComments:      gitReviewExtractKeyCommentsFromDiff(stagedDiff),
		ChangeCategories: gitReviewCategorizeChanges(stagedDiff),
		FullFileContext:  ws.gitReviewExtractFileContextForChanges(workspaceRoot, stagedDiff),
	}

	if len(optimizedDiff.FileSummaries) > 0 {
		var summaryInfo strings.Builder
		summaryInfo.WriteString("\n\nLarge files optimized for review:\n")
		for file, summary := range optimizedDiff.FileSummaries {
			summaryInfo.WriteString(fmt.Sprintf("- %s: %s\n", file, summary))
		}
		reviewCtx.Diff += summaryInfo.String()
	}

	opts := &codereview.ReviewOptions{
		Type:             codereview.StagedReview,
		SkipPrompt:       true,
		RollbackOnReject: false,
	}

	reviewResponse, err := service.PerformAgenticReview(reviewCtx, opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Deep review failed: %v", err), http.StatusInternalServerError)
		return
	}

	reviewOutput := fmt.Sprintf("%s\n%s\n\nStatus: %s\n\nFeedback:\n%s",
		"[list] AI CODE REVIEW (DEEP PASS)",
		strings.Repeat("═", 50),
		strings.ToUpper(reviewResponse.Status),
		reviewResponse.Feedback)

	if strings.TrimSpace(reviewResponse.DetailedGuidance) != "" {
		reviewOutput += fmt.Sprintf("\n\nDetailed Guidance:\n%s", reviewResponse.DetailedGuidance)
	}
	if reviewResponse.Status == "rejected" && reviewResponse.NewPrompt != "" {
		reviewOutput += fmt.Sprintf("\n\nSuggested New Prompt:\n%s", reviewResponse.NewPrompt)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":              "Deep review completed",
		"status":               reviewResponse.Status,
		"feedback":             reviewResponse.Feedback,
		"detailed_guidance":    reviewResponse.DetailedGuidance,
		"suggested_new_prompt": reviewResponse.NewPrompt,
		"review_output":        reviewOutput,
		"provider":             agentInst.GetProvider(),
		"model":                agentInst.GetModel(),
		"warnings":             optimizedDiff.Warnings,
	})
}

// handleAPIGitDeepReviewFix runs the fix workflow and blocks until completion (legacy API).
func (ws *ReactWebServer) handleAPIGitDeepReviewFix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ReviewOutput  string   `json:"review_output"`
		FixPrompt     string   `json:"fix_prompt"`
		SelectedItems []string `json:"selected_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	reviewOutput := strings.TrimSpace(req.ReviewOutput)
	if reviewOutput == "" {
		http.Error(w, "review_output is required", http.StatusBadRequest)
		return
	}

	job, _, err := ws.startFixReviewJob(reviewOutput, ws.resolveClientID(r), req.FixPrompt, req.SelectedItems)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to start fix workflow: %v", err), http.StatusInternalServerError)
		return
	}

	for {
		status, _, _, result, jobErr := job.snapshot(0)
		if status == "completed" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message":    "Fix workflow completed",
				"result":     strings.TrimSpace(result),
				"session_id": job.SessionID,
			})
			return
		}
		if status == "error" {
			http.Error(w, fmt.Sprintf("Failed to run fix workflow: %s", jobErr), http.StatusInternalServerError)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// handleAPIGitDeepReviewFixStart starts an isolated full-agent fix workflow job.
func (ws *ReactWebServer) handleAPIGitDeepReviewFixStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ReviewOutput  string   `json:"review_output"`
		FixPrompt     string   `json:"fix_prompt"`
		SelectedItems []string `json:"selected_items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	reviewOutput := strings.TrimSpace(req.ReviewOutput)
	if reviewOutput == "" {
		http.Error(w, "review_output is required", http.StatusBadRequest)
		return
	}

	job, _, err := ws.startFixReviewJob(reviewOutput, ws.resolveClientID(r), req.FixPrompt, req.SelectedItems)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to start fix workflow: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "Fix workflow started",
		"job_id":     job.ID,
		"session_id": job.SessionID,
	})
}

// handleAPIGitDeepReviewFixStatus returns incremental status/logs for a running fix workflow job.
func (ws *ReactWebServer) handleAPIGitDeepReviewFixStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := strings.TrimSpace(r.URL.Query().Get("job_id"))
	if jobID == "" {
		http.Error(w, "job_id is required", http.StatusBadRequest)
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
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	// Authorization: only the client that started the job can query its status.
	// Jobs with an empty ClientID pre-date access control (backward compat)
	// and are accessible by any client. No new jobs should have empty ClientID.
	requestClientID := ws.resolveClientID(r)
	if job.ClientID != "" && job.ClientID != requestClientID {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	status, logs, next, result, jobErr := job.snapshot(since)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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
		prompt = fmt.Sprintf("Use this deep review output as input:\n\n%s\n\n%s", reviewOutput, fixInstructions)
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

func (ws *ReactWebServer) gitReviewDetectProjectType(workspaceRoot string) string {
	projectMarkers := []struct {
		name string
		file string
	}{
		{name: "Go project", file: "go.mod"},
		{name: "Node.js project", file: "package.json"},
		{name: "Python project", file: "requirements.txt"},
		{name: "Python project", file: "setup.py"},
		{name: "Python project", file: "pyproject.toml"},
		{name: "Rust project", file: "Cargo.toml"},
		{name: "Ruby project", file: "Gemfile"},
	}

	for _, marker := range projectMarkers {
		if _, err := os.Stat(filepath.Join(workspaceRoot, marker.file)); err == nil {
			return marker.name
		}
	}
	return ""
}

func (ws *ReactWebServer) gitReviewExtractStagedChangesSummary(workspaceRoot string) string {
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	statLines := strings.Split(string(output), "\n")
	if len(statLines) > 0 && strings.TrimSpace(statLines[0]) != "" {
		return fmt.Sprintf("Staged changes summary: %s", strings.TrimSpace(statLines[0]))
	}
	return ""
}

func gitReviewExtractKeyCommentsFromDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	keyComments := make([]string, 0, 8)
	currentFile := ""

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				currentFile = strings.TrimPrefix(parts[3], "b/")
			}
			continue
		}

		if strings.HasPrefix(line, "+") && (strings.Contains(line, "//") || strings.Contains(line, "#")) {
			comment := strings.TrimSpace(strings.TrimPrefix(line, "+"))
			if gitReviewIsImportantComment(comment) {
				keyComments = append(keyComments, fmt.Sprintf("- %s: %s", currentFile, comment))
			}
		}
	}

	if len(keyComments) == 0 {
		return ""
	}
	if len(keyComments) > 10 {
		keyComments = keyComments[:10]
	}
	return strings.Join(keyComments, "\n")
}

func gitReviewIsImportantComment(comment string) bool {
	commentUpper := strings.ToUpper(comment)
	keywords := []string{
		"CRITICAL", "IMPORTANT", "NOTE:", "WARNING", "TODO:", "FIXME",
		"HACK", "BUG", "SECURITY", "FIX", "WORKAROUND",
		"BECAUSE", "REASON:", "WHY:", "INTENT:", "PURPOSE:",
	}

	for _, keyword := range keywords {
		if strings.Contains(commentUpper, keyword) {
			return true
		}
	}
	return strings.HasPrefix(comment, "//") && len(comment) > 50
}

func gitReviewCategorizeChanges(diff string) string {
	lines := strings.Split(diff, "\n")
	categories := make(map[string]int)

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index") {
			continue
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			addedLine := strings.TrimPrefix(line, "+")
			if strings.Contains(strings.ToUpper(addedLine), "SECURITY") ||
				strings.Contains(addedLine, "filesystem.ErrOutsideWorkingDirectory") ||
				strings.Contains(addedLine, "WithSecurityBypass") {
				categories["Security fixes/improvements"]++
			}
			if strings.Contains(addedLine, "error") ||
				strings.Contains(addedLine, "Err") ||
				strings.Contains(addedLine, "return nil") ||
				strings.Contains(addedLine, "if err") {
				categories["Error handling"]++
			}
			if strings.Contains(addedLine, "require(") ||
				strings.Contains(addedLine, "github.com/") ||
				strings.Contains(addedLine, "go.mod") {
				categories["Dependency updates"]++
			}
			if strings.Contains(addedLine, "Test") || strings.Contains(addedLine, "test") {
				categories["Test changes"]++
			}
		}

		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			categories["Code removal/refactoring"]++
		}
	}

	if len(categories) == 0 {
		return ""
	}

	linesOut := make([]string, 0, len(categories))
	for category, count := range categories {
		linesOut = append(linesOut, fmt.Sprintf("- %s (%d changes)", category, count))
	}
	return strings.Join(linesOut, "\n")
}

func (ws *ReactWebServer) gitReviewExtractFileContextForChanges(workspaceRoot, diff string) string {
	lines := strings.Split(diff, "\n")
	changedFiles := make(map[string]bool)

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				changedFiles[strings.TrimPrefix(parts[3], "b/")] = true
			}
		}
	}

	contextParts := make([]string, 0, len(changedFiles))
	for relPath := range changedFiles {
		if !ws.gitReviewIsValidRepoFilePath(workspaceRoot, relPath) || gitReviewShouldSkipFileForContext(relPath) {
			continue
		}

		absPath := filepath.Join(workspaceRoot, relPath)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			continue
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		fileLines := strings.Split(string(content), "\n")
		maxLines := 500
		if len(fileLines) < maxLines {
			maxLines = len(fileLines)
		}
		if maxLines > 0 {
			contextParts = append(contextParts, fmt.Sprintf("### %s\n```go\n%s\n```", relPath, strings.Join(fileLines[:maxLines], "\n")))
		}
	}

	if len(contextParts) == 0 {
		return ""
	}
	return strings.Join(contextParts, "\n\n")
}
