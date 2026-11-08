//go:build !js

package tools

// check_app runs the app check (sprout check app) for the agent: it builds
// the project, serves the build output the way the platform hosts it, uses
// the app in the same headless browser browse_url drives, and returns what a
// person would run into in a short form the model can act on. The report
// itself (.sprout/app-check.json) is left on disk for the platform and the
// person to read.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/appcheck"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// defaultAppCheckToolTimeout bounds a tool call: the platform check's default
// app-use budget plus room for the build and the server start.
const defaultAppCheckToolTimeout = 6 * time.Minute

// checkAppHandler is the check_app tool.
type checkAppHandler struct{}

func (h *checkAppHandler) Name() string { return "check_app" }

func (h *checkAppHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "check_app",
		Description: "Use the built web app in a headless browser and report what a person would run into: " +
			"pages that do not load or are blank, forms that refuse sensible values or send nothing, failed requests, " +
			"console errors, and entries gone after a fresh load. Builds first unless no_build is set and dist/ exists, " +
			"serves dist/ the way the platform hosts it, and writes .sprout/app-check.json. " +
			"Use it after changing a web app's pages or forms; act on the problems it returns.",
		Parameters: []ParameterDef{
			{Name: "no_build", Type: "boolean", Description: "Use the existing dist/ instead of building first"},
			{Name: "budget_seconds", Type: "integer", Description: "Seconds to spend using the app (default 120)"},
		},
	}
}

func (h *checkAppHandler) Validate(args map[string]any) error {
	if v, ok := args["budget_seconds"]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			if n < 0 {
				return agenterrors.NewInvalidInputError("budget_seconds must be zero or more", nil)
			}
		case int:
			if n < 0 {
				return agenterrors.NewInvalidInputError("budget_seconds must be zero or more", nil)
			}
		}
	}
	return nil
}

func (h *checkAppHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	root := strings.TrimSpace(env.WorkspaceRoot)
	if root == "" {
		msg := "check_app: no workspace root, so there is no project to check"
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewPermanentError(msg, nil)
	}
	budget := appcheck.DefaultBudget
	if n, err := extractInt(args, "budget_seconds"); err == nil && n > 0 {
		budget = time.Duration(n) * time.Second
	}
	report, err := appcheck.Run(ctx, appcheck.Options{
		Root:    root,
		NoBuild: getBoolArg(args, "no_build"),
		Budget:  budget,
		Browser: sharedRodBrowser(),
	})
	if err != nil {
		msg := fmt.Sprintf("check_app: %v", err)
		return ToolResult{Output: msg, IsError: true}, agenterrors.Wrapf(err, "check_app")
	}
	out := report.Summary()
	out += fmt.Sprintf("Details: %s\n", filepath.Join(root, ".sprout", "app-check.json"))
	return ToolResult{Output: out}, nil
}

// sharedRodBrowser is the browser the check drives: the same Rod renderer
// browse_url uses (pkg/webcontent), so the app has one browser stack. A
// missing Chromium surfaces as "could not run" in the report, not an error.
func sharedRodBrowser() appcheck.RodBrowser {
	return appcheck.NewWebcontentBrowser()
}

func (h *checkAppHandler) Aliases() []string      { return nil }
func (h *checkAppHandler) Timeout() time.Duration { return defaultAppCheckToolTimeout }
func (h *checkAppHandler) MaxResultSize() int     { return 8192 }
func (h *checkAppHandler) SafeForParallel() bool  { return false }
func (h *checkAppHandler) Interactive() bool      { return false }
