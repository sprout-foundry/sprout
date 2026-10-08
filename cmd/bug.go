//go:build !js

package cmd

import (
	"fmt"
	"net/url"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/repo"
)

// bugReportTemplate mirrors the web UI's short template (webui/src/services/
// reportBug.ts): what happened / what you expected / steps, then the
// environment. It carries no file paths, workspace names, session ids,
// provider keys, prompts or model output — this is a public repo.
const bugReportTemplate = `### What happened



### What you expected



### Steps to reproduce

1. 
`

// bugReportTitle is the default issue title.
const bugReportTitle = "Bug: "

// maxBugURLBytes keeps the whole URL comfortably under GitHub's ~8 KB cap.
const maxBugURLBytes = 7000

var bugCmd = &cobra.Command{
	Use:   "bug",
	Short: "Report a bug on the public repository",
	Long: `Open a new GitHub issue on the public Sprout repository, prefilled with a
short template and your environment (version, OS).

The issue URL is printed as well, so it can be copied into a browser by hand.`,
	Example: `  sprout bug`,
	RunE: func(cmd *cobra.Command, args []string) error {
		issueURL := buildBugReportURL()
		console.GlyphSuccess.Print("Opening a new issue in your browser")
		fmt.Println(issueURL)
		if err := openURL(issueURL); err != nil {
			console.Hintln(os.Stderr, "Open the URL above in your browser.")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(bugCmd)
}

// buildBugReportURL builds the prefilled new-issue URL with the running
// version and OS filled in. Pure, so it is directly testable.
func buildBugReportURL() string {
	body := fmt.Sprintf(
		"%s\n### Environment\n\n- sprout version: %s\n- OS: %s\n",
		bugReportTemplate, bugVersion(), runtime.GOOS,
	)
	return buildBugReportURLForBody(body)
}

// buildBugReportURLForBody builds the URL for an arbitrary body, truncating it
// so the whole (percent-encoded) URL stays under maxBugURLBytes. The trim is
// against the ENCODED length, since URL encoding inflates the body (newlines
// become %0A, etc.) — trimming against the raw length would overshoot the cap.
func buildBugReportURLForBody(body string) string {
	build := func(b string) string {
		params := url.Values{}
		params.Set("title", bugReportTitle)
		params.Set("body", b)
		params.Set("labels", "bug")
		return repo.IssuesNewURL + "?" + params.Encode()
	}

	full := build(body)
	if len(full) <= maxBugURLBytes {
		return full
	}

	marker := "\n\n…(truncated)"
	fixed := len(build("")) // endpoint + title + labels + separators
	keep := maxBugURLBytes - fixed - len(marker)
	if keep < 0 {
		keep = 0
	}
	for {
		full = build(body[:keep] + marker)
		if len(full) <= maxBugURLBytes || keep == 0 {
			break
		}
		step := (len(full) - maxBugURLBytes) / 3
		if step < 1 {
			step = 1
		}
		keep -= step
		if keep < 0 {
			keep = 0
		}
	}
	return full
}

// bugVersion reports the running build's version (never a literal).
func bugVersion() string {
	return buildinfo.Version
}
