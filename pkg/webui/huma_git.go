//go:build !js

// huma_git.go holds the Huma operations and their thin handlers for the git
// family: the /api/git/* read and write routes (registered via
// registerGitHumaOperations from registerHumaOperations in huma_routes.go).
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are
// unchanged and the documented request/response schemas in the seed carry
// through the merge.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerGitHumaOperations registers the git-family Huma operations.
func registerGitHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "gitStatus",
		Method:      http.MethodGet,
		Path:        "/api/git/status",
		Summary:     "Report the workspace's git status.",
		Description: "Reports the workspace's git status: staged, unstaged, and untracked changes, plus branch and ahead/behind counts.",
		Tags:        []string{"git"},
	}, ws.gitStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitStage",
		Method:      http.MethodPost,
		Path:        "/api/git/stage",
		Summary:     "Stage a file.",
		Description: "Adds the file at `path` to the git index.",
		Tags:        []string{"git"},
	}, ws.gitStageHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitUnstage",
		Method:      http.MethodPost,
		Path:        "/api/git/unstage",
		Summary:     "Unstage a file.",
		Description: "Removes the file at `path` from the git index.",
		Tags:        []string{"git"},
	}, ws.gitUnstageHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitDiscard",
		Method:      http.MethodPost,
		Path:        "/api/git/discard",
		Summary:     "Discard changes to a file.",
		Description: "Reverts the file at `path` to its committed state.",
		Tags:        []string{"git"},
	}, ws.gitDiscardHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitCommit",
		Method:      http.MethodPost,
		Path:        "/api/git/commit",
		Summary:     "Commit the currently staged changes.",
		Description: "Commits the staged changes with `message`. A dry-run first surfaces the files that would be committed.",
		Tags:        []string{"git"},
	}, ws.gitCommitHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitCommitMessage",
		Method:      http.MethodPost,
		Path:        "/api/git/commit-message",
		Summary:     "Generate an AI commit message from the staged changes.",
		Description: "Generates an AI commit message for the currently staged changes using the active agent.",
		Tags:        []string{"git"},
	}, ws.gitCommitMessageHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitConfirm",
		Method:      http.MethodPost,
		Path:        "/api/git/confirm",
		Summary:     "Respond to a pending security approval or prompt.",
		Description: "Responds to a pending security approval or prompt (the `request_id`/`response` pair), unblocking the agent that was waiting for it.",
		Tags:        []string{"git"},
	}, ws.gitConfirmHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitDeepReview",
		Method:      http.MethodPost,
		Path:        "/api/git/deep-review",
		Summary:     "Review the staged changes with reviewer subagents.",
		Description: "Reviews the staged changes with reviewer subagents and returns the aggregated findings.",
		Tags:        []string{"git"},
	}, ws.gitDeepReviewHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitDeepReviewFix",
		Method:      http.MethodPost,
		Path:        "/api/git/deep-review/fix",
		Summary:     "Run the deep-review fix workflow and block until it completes.",
		Description: "Runs the deep-review fix workflow in the foreground and blocks until it completes, returning the final report.",
		Tags:        []string{"git"},
	}, ws.gitDeepReviewFixHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitDeepReviewFixStart",
		Method:      http.MethodPost,
		Path:        "/api/git/deep-review/fix/start",
		Summary:     "Start an isolated deep-review fix job.",
		Description: "Starts an isolated deep-review fix job and returns its job ID so the status endpoint can be polled.",
		Tags:        []string{"git"},
	}, ws.gitDeepReviewFixStartHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitDeepReviewFixStatus",
		Method:      http.MethodGet,
		Path:        "/api/git/deep-review/fix/status",
		Summary:     "Poll the status and logs of a running deep-review fix job.",
		Description: "Polls the status and incremental logs of a running deep-review fix job.",
		Tags:        []string{"git"},
	}, ws.gitDeepReviewFixStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitStageAll",
		Method:      http.MethodPost,
		Path:        "/api/git/stage-all",
		Summary:     "Stage all changes.",
		Description: "Adds every changed file to the git index.",
		Tags:        []string{"git"},
	}, ws.gitStageAllHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitUnstageAll",
		Method:      http.MethodPost,
		Path:        "/api/git/unstage-all",
		Summary:     "Unstage all changes.",
		Description: "Removes every staged file from the git index.",
		Tags:        []string{"git"},
	}, ws.gitUnstageAllHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitDiff",
		Method:      http.MethodGet,
		Path:        "/api/git/diff",
		Summary:     "Return the staged and unstaged diff for a file.",
		Description: "Returns the staged and unstaged diff for the file at `path` (or the whole index when `path` is omitted).",
		Tags:        []string{"git"},
	}, ws.gitDiffHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitBranches",
		Method:      http.MethodGet,
		Path:        "/api/git/branches",
		Summary:     "List local and remote branches.",
		Description: "Lists the local and remote branches with the current branch flagged.",
		Tags:        []string{"git"},
	}, ws.gitBranchesHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitWorktrees",
		Method:      http.MethodGet,
		Path:        "/api/git/worktrees",
		Summary:     "List git worktrees.",
		Description: "Lists the repository's git worktrees.",
		Tags:        []string{"git"},
	}, ws.gitWorktreesHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitWorktreeCreate",
		Method:      http.MethodPost,
		Path:        "/api/git/worktree/create",
		Summary:     "Create a new worktree.",
		Description: "Creates a new git worktree at `path` from `branch` (or a new branch based on the current one).",
		Tags:        []string{"git"},
	}, ws.gitWorktreeCreateHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitWorktreeRemove",
		Method:      http.MethodPost,
		Path:        "/api/git/worktree/remove",
		Summary:     "Remove an existing worktree.",
		Description: "Removes the git worktree at `path`.",
		Tags:        []string{"git"},
	}, ws.gitWorktreeRemoveHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitWorktreeCheckout",
		Method:      http.MethodPost,
		Path:        "/api/git/worktree/checkout",
		Summary:     "Switch to an existing worktree.",
		Description: "Switches the client's active workspace to the git worktree at `path`.",
		Tags:        []string{"git"},
	}, ws.gitWorktreeCheckoutHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitCheckout",
		Method:      http.MethodPost,
		Path:        "/api/git/checkout",
		Summary:     "Switch to an existing branch.",
		Description: "Checks out the branch at `branch` in the workspace.",
		Tags:        []string{"git"},
	}, ws.gitCheckoutHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitRevert",
		Method:      http.MethodPost,
		Path:        "/api/git/revert",
		Summary:     "Revert a commit.",
		Description: "Reverts the commit at `sha`, producing a new commit that undoes it.",
		Tags:        []string{"git"},
	}, ws.gitRevertHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitPullRequest",
		Method:      http.MethodPost,
		Path:        "/api/git/pull-request",
		Summary:     "Create a pull request.",
		Description: "Creates a pull request for the current branch against `base`.",
		Tags:        []string{"git"},
	}, ws.gitPullRequestHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitBranchCreate",
		Method:      http.MethodPost,
		Path:        "/api/git/branch/create",
		Summary:     "Create a new branch and check it out.",
		Description: "Creates a new branch at `branch` (based on `base` or the current branch) and checks it out.",
		Tags:        []string{"git"},
	}, ws.gitBranchCreateHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitPull",
		Method:      http.MethodPost,
		Path:        "/api/git/pull",
		Summary:     "Pull the latest changes from the upstream branch.",
		Description: "Pulls the latest changes for the current branch from its upstream.",
		Tags:        []string{"git"},
	}, ws.gitPullHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitPush",
		Method:      http.MethodPost,
		Path:        "/api/git/push",
		Summary:     "Push the current branch to its upstream.",
		Description: "Pushes the current branch to its upstream, optionally with `-u` to set it on the first push.",
		Tags:        []string{"git"},
	}, ws.gitPushHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitLog",
		Method:      http.MethodGet,
		Path:        "/api/git/log",
		Summary:     "List recent commits, paginated.",
		Description: "Lists recent commits, paginated by `page` and `per_page`, optionally filtered to a `ref`.",
		Tags:        []string{"git"},
	}, ws.gitLogHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitCommitShow",
		Method:      http.MethodGet,
		Path:        "/api/git/commit/show",
		Summary:     "Show a commit's metadata, diff, and file list.",
		Description: "Shows a commit's metadata, diff, and file list for the commit at `sha`.",
		Tags:        []string{"git"},
	}, ws.gitCommitShowHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "gitCommitFileDiff",
		Method:      http.MethodGet,
		Path:        "/api/git/commit/show/file",
		Summary:     "Show the diff of a single file within a commit.",
		Description: "Shows the diff of the single file at `path` within the commit at `sha`.",
		Tags:        []string{"git"},
	}, ws.gitCommitFileDiffHumaHandler)
}

// gitStatusHumaHandler is the Huma handler for GET /api/git/status.
func (ws *ReactWebServer) gitStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitStageHumaHandler is the Huma handler for POST /api/git/stage.
func (ws *ReactWebServer) gitStageHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitStage(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitUnstageHumaHandler is the Huma handler for POST /api/git/unstage.
func (ws *ReactWebServer) gitUnstageHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitUnstage(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitDiscardHumaHandler is the Huma handler for POST /api/git/discard.
func (ws *ReactWebServer) gitDiscardHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitDiscard(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitCommitHumaHandler is the Huma handler for POST /api/git/commit.
func (ws *ReactWebServer) gitCommitHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitCommit(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitCommitMessageHumaHandler is the Huma handler for POST /api/git/commit-message.
func (ws *ReactWebServer) gitCommitMessageHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitCommitMessage(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitConfirmHumaHandler is the Huma handler for POST /api/git/confirm.
func (ws *ReactWebServer) gitConfirmHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIConfirm(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitDeepReviewHumaHandler is the Huma handler for POST /api/git/deep-review.
func (ws *ReactWebServer) gitDeepReviewHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitDeepReview(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitDeepReviewFixHumaHandler is the Huma handler for POST /api/git/deep-review/fix.
func (ws *ReactWebServer) gitDeepReviewFixHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitDeepReviewFix(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitDeepReviewFixStartHumaHandler is the Huma handler for POST /api/git/deep-review/fix/start.
func (ws *ReactWebServer) gitDeepReviewFixStartHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitDeepReviewFixStart(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitDeepReviewFixStatusHumaHandler is the Huma handler for GET /api/git/deep-review/fix/status.
func (ws *ReactWebServer) gitDeepReviewFixStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitDeepReviewFixStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitStageAllHumaHandler is the Huma handler for POST /api/git/stage-all.
func (ws *ReactWebServer) gitStageAllHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitStageAll(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitUnstageAllHumaHandler is the Huma handler for POST /api/git/unstage-all.
func (ws *ReactWebServer) gitUnstageAllHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitUnstageAll(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitDiffHumaHandler is the Huma handler for GET /api/git/diff.
func (ws *ReactWebServer) gitDiffHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitDiff(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitBranchesHumaHandler is the Huma handler for GET /api/git/branches.
func (ws *ReactWebServer) gitBranchesHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitBranches(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitWorktreesHumaHandler is the Huma handler for GET /api/git/worktrees.
func (ws *ReactWebServer) gitWorktreesHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitWorktrees(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitWorktreeCreateHumaHandler is the Huma handler for POST /api/git/worktree/create.
func (ws *ReactWebServer) gitWorktreeCreateHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitWorktreeCreate(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitWorktreeRemoveHumaHandler is the Huma handler for POST /api/git/worktree/remove.
func (ws *ReactWebServer) gitWorktreeRemoveHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitWorktreeRemove(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitWorktreeCheckoutHumaHandler is the Huma handler for POST /api/git/worktree/checkout.
func (ws *ReactWebServer) gitWorktreeCheckoutHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitWorktreeCheckout(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitCheckoutHumaHandler is the Huma handler for POST /api/git/checkout.
func (ws *ReactWebServer) gitCheckoutHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitCheckout(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitRevertHumaHandler is the Huma handler for POST /api/git/revert.
func (ws *ReactWebServer) gitRevertHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitRevert(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitPullRequestHumaHandler is the Huma handler for POST /api/git/pull-request.
func (ws *ReactWebServer) gitPullRequestHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitPullRequest(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitBranchCreateHumaHandler is the Huma handler for POST /api/git/branch/create.
func (ws *ReactWebServer) gitBranchCreateHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitCreateBranch(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitPullHumaHandler is the Huma handler for POST /api/git/pull.
func (ws *ReactWebServer) gitPullHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitPull(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitPushHumaHandler is the Huma handler for POST /api/git/push.
func (ws *ReactWebServer) gitPushHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitPush(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitLogHumaHandler is the Huma handler for GET /api/git/log.
func (ws *ReactWebServer) gitLogHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitLog(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitCommitShowHumaHandler is the Huma handler for GET /api/git/commit/show.
func (ws *ReactWebServer) gitCommitShowHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitCommitShow(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// gitCommitFileDiffHumaHandler is the Huma handler for GET /api/git/commit/show/file.
func (ws *ReactWebServer) gitCommitFileDiffHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGitCommitFileDiff(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
