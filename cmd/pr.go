//go:build !js

package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

var (
	prTitle      string
	prBody       string
	prBase       string
	prDraft      bool
	prWeb        bool
	prSkipPrompt bool
)

// ---------------------------------------------------------------------------
// Command
// ---------------------------------------------------------------------------

var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Create a GitHub pull request for the current branch",
	Long: `Create a pull request on GitHub for the current working branch.

If --title or --body are omitted and --yes is not set, a pre-filled
editor will open for you to review and adjust before submission.

Resolution order (handled by the backend):
  1. GitHub REST API (requires GH_TOKEN env var)
  2. gh pr create shell-out (fallback when no token)
  3. Structured error with the exact gh command you can run manually

Examples:
  sprout pr --title "Fix login bug" --body "Resolved the OAuth redirect issue"
  sprout pr --base develop --draft
  sprout pr --web                       # Open gh's web UI for PR creation
  sprout pr --yes                       # Use synthesised title/body without editor`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		repoDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		// --- Web mode: delegate to gh pr create --web ---
		if prWeb {
			return runWebMode(ctx, repoDir)
		}

		// --- Resolve base branch (needed for title synthesis) ---
		base := prBase
		if base == "" {
			base, err = git.GetDefaultBranch(ctx, repoDir)
			if err != nil {
				return fmt.Errorf("determine default branch: %w", err)
			}
		}

		// --- Synthesise title / body if needed and not skipping prompt ---
		title := prTitle
		body := prBody

		if !prSkipPrompt && (title == "" || body == "") {
			title, body, err = promptWithEditor(ctx, repoDir, base, title, body)
			if err != nil {
				return err
			}
		}

		// --- Final fallback for empty title ---
		if title == "" {
			title, err = synthesizeTitle(ctx, repoDir, base)
			if err != nil {
				return fmt.Errorf("synthesise title: %w", err)
			}
		}

		// --- Create the PR ---
		result, err := git.CreatePullRequest(ctx, repoDir, git.PullRequestRequest{
			Title: title,
			Body:  body,
			Base:  base,
			Draft: prDraft,
		})
		if err != nil {
			if errors.Is(err, git.ErrNoGitHubAuth) {
				return err
			}
			return fmt.Errorf("creating PR: %w", err)
		}

		reportPRCreated(result)
		return nil
	},
}

// reportPRCreated prints the PR URL alone on stdout (so `url=$(sprout pr …)`
// works) and the human summary on stderr.
func reportPRCreated(result *git.PullRequestResult) {
	summary := "Created PR"
	if result.Number > 0 {
		summary += fmt.Sprintf(" #%d", result.Number)
	}
	if result.State != "" {
		summary += " · " + result.State
	}
	console.GlyphSuccess.Print(summary)
	fmt.Println(result.URL)
}

func init() {
	prCmd.Flags().StringVar(&prTitle, "title", "", "PR title (synthesised from commits if omitted)")
	prCmd.Flags().StringVar(&prBody, "body", "", "PR body (synthesised from commits if omitted)")
	prCmd.Flags().StringVar(&prBase, "base", "", "Target branch (defaults to repo default)")
	prCmd.Flags().BoolVar(&prDraft, "draft", false, "Create as a draft PR")
	prCmd.Flags().BoolVar(&prWeb, "web", false, "Open PR creation in browser via gh CLI")
	prCmd.Flags().BoolVarP(&prSkipPrompt, "yes", "y", false, "Skip the editor; use the synthesised title/body")
	boolFlagAlias(prCmd.Flags(), &prSkipPrompt, "skip-prompt", "yes", aliasDeprecated)
}

// ---------------------------------------------------------------------------
// Web mode
// ---------------------------------------------------------------------------

func runWebMode(ctx context.Context, repoDir string) error {
	// Resolve head branch
	head, err := getCurrentBranch(ctx, repoDir)
	if err != nil {
		return err
	}

	// Resolve base branch
	base := prBase
	if base == "" {
		base, err = git.GetDefaultBranch(ctx, repoDir)
		if err != nil {
			return fmt.Errorf("determine default branch: %w", err)
		}
	}

	// Try gh pr create --web
	args := []string{"pr", "create", "--web", "--base", base, "--head", head}
	if prTitle != "" {
		args = append(args, "--title", prTitle)
	}
	if prBody != "" {
		args = append(args, "--body", prBody)
	}
	if prDraft {
		args = append(args, "--draft")
	}

	_, err = git.RunGhCommand(ctx, repoDir, args...)
	if err != nil {
		console.GlyphWarning.Printf("gh couldn't open the PR form (%v) — creating the PR directly", err)

		// Fallback: create via API/CLI then open the URL
		result, err := git.CreatePullRequest(ctx, repoDir, git.PullRequestRequest{
			Title: prTitle,
			Body:  prBody,
			Base:  base,
			Draft: prDraft,
		})
		if err != nil {
			return err
		}

		reportPRCreated(result)
		if err := openURL(result.URL); err != nil {
			console.Hintln(os.Stderr, "Open the URL above in your browser.")
		}
		return nil
	}

	console.GlyphSuccess.Print("Opened PR creation in browser.")
	return nil
}

// openURL launches the given URL in the user's default browser.
//
// Intentionally fire-and-forget: it uses exec.Command(...).Start() without
// exec.CommandContext.  This is a UX best-effort helper — by the time it runs
// the PR is already created, so a cancelled context would leave the user with
// a working PR but no browser window.  Documenting the trade-off rather than
// threading context through for marginal gain.
func openURL(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		// Linux / WSL
		if _, err := exec.LookPath("wslview"); err == nil {
			return exec.Command("wslview", url).Start()
		}
		return exec.Command("xdg-open", url).Start()
	}
}

// ---------------------------------------------------------------------------
// Editor prompt
// ---------------------------------------------------------------------------

func promptWithEditor(
	ctx context.Context,
	repoDir string,
	base string,
	existingTitle string,
	existingBody string,
) (title, body string, err error) {
	// Synthesise title if missing
	if existingTitle == "" {
		existingTitle, err = synthesizeTitle(ctx, repoDir, base)
		if err != nil {
			return "", "", fmt.Errorf("synthesise title: %w", err)
		}
	}

	// Synthesise body if missing
	if existingBody == "" {
		existingBody, err = synthesizeBodyForPrompt(ctx, repoDir, base)
		if err != nil {
			// Non-fatal for body synthesis in prompt context
			existingBody = ""
		}
	}

	// Write template to a temp file
	template := fmt.Sprintf(`# Pull Request Title
%s

# Pull Request Body
%s
`, existingTitle, existingBody)

	tmpFile, err := os.CreateTemp("", "sprout-pr-*.md")
	if err != nil {
		return "", "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(template); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("write template: %w", err)
	}
	tmpFile.Close()

	// Open editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	// Handle editors with arguments (e.g. "vim -p")
	editorCmd := strings.Fields(editor)
	editExe := editorCmd[0]

	// Check editor exists
	if _, lookupErr := exec.LookPath(editExe); lookupErr != nil {
		// Try common fallbacks
		for _, candidate := range []string{"nano", "vim", "vi"} {
			if _, err := exec.LookPath(candidate); err == nil {
				editExe = candidate
				editorCmd = []string{candidate}
				break
			}
		}
	}

	cmd := exec.CommandContext(ctx, editExe, append(editorCmd[1:], tmpPath)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("run %s: %w", editExe, err)
	}

	// Parse the edited file
	title, body, err = parseEditedFile(tmpPath)
	os.Remove(tmpPath)
	return title, body, err
}

func parseEditedFile(path string) (title, body string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("open edited file: %w", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("read edited file (possible binary or oversized content): %w", err)
	}

	content := strings.Join(lines, "\n")

	// Extract title: first non-empty line after "# Pull Request Title"
	titleStart := strings.Index(content, "# Pull Request Title")
	if titleStart >= 0 {
		afterTitle := content[titleStart+len("# Pull Request Title"):]
		bodyStart := strings.Index(afterTitle, "# Pull Request Body")
		var titleBlock string
		if bodyStart >= 0 {
			titleBlock = afterTitle[:bodyStart]
		} else {
			titleBlock = afterTitle
		}
		title = extractFirstLine(titleBlock)
	}

	// Extract body: everything after "# Pull Request Body"
	bodyStart := strings.Index(content, "# Pull Request Body")
	if bodyStart >= 0 {
		bodyAfter := content[bodyStart+len("# Pull Request Body"):]
		body = strings.TrimSpace(bodyAfter)
	}

	if title == "" {
		return "", "", fmt.Errorf("PR title is empty after editing; please provide a title")
	}

	return title, body, nil
}

func extractFirstLine(s string) string {
	idx := strings.Index(s, "\n")
	if idx >= 0 {
		line := strings.TrimSpace(s[:idx])
		if line != "" {
			return line
		}
		// First line is empty; recurse into the rest to find the first
		// non-empty line.
		return extractFirstLine(s[idx+1:])
	}
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------------------
// Synthesis helpers
// ---------------------------------------------------------------------------

func getCurrentBranch(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("no current branch (detached HEAD)")
	}
	return branch, nil
}

func synthesizeTitle(ctx context.Context, repoDir, base string) (string, error) {
	head, err := getCurrentBranch(ctx, repoDir)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "git", "log", "--format=%s", "-1", fmt.Sprintf("%s..%s", base, head))
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git log for title: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func synthesizeBodyForPrompt(ctx context.Context, repoDir, base string) (string, error) {
	head, err := getCurrentBranch(ctx, repoDir)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "git", "log", "--format=%s", fmt.Sprintf("%s..%s", base, head))
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git log for body: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var subjects []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			subjects = append(subjects, l)
		}
	}
	if len(subjects) == 0 {
		return "", nil
	}

	var buf strings.Builder
	buf.WriteString("## Commits\n\n")
	for _, s := range subjects {
		buf.WriteString(fmt.Sprintf("- %s\n", s))
	}
	return buf.String(), nil
}
