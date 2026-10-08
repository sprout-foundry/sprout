package verify

import (
	"context"
	"errors"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// StepBrowser is the verify package's narrow browser seam for interaction
// checks: it runs the plan's scripted browser steps in order
// against a base URL and reports what it observed. The production
// implementation adapts pkg/webcontent (NewWebcontentStepBrowser); tests
// inject a fake so a run is deterministic and needs no Chromium.
//
// The steps come only from the active plan's interaction acceptance items
// — the frozen plan written before the turn, not model output
// produced mid-turn — so running them is a trusted verification act, not a
// model-sourced command.
type StepBrowser interface {
	// RunSteps runs the steps in order against baseURL (the dev server root)
	// and reports the executed actions (evidence). A nil error means every
	// step, including assert-style expected-outcome steps, succeeded.
	// errors.Is(err, webcontent.ErrBrowserUnavailable) marks a browser that
	// cannot render here (skipped, not a failure).
	RunSteps(ctx context.Context, baseURL string, steps []plancontract.BrowseStep) (*StepObservation, error)
}

// StepObservation is what a StepBrowser reports for one scripted run.
type StepObservation struct {
	// FinalURL is the page URL after the last step navigated ("" when
	// unknown).
	FinalURL string
	// Title is the page title after the last step ("" when unknown).
	Title string
	// Actions are the executed step descriptions in order — the evidence an
	// interaction check's excerpt carries (the confirmed outcome).
	Actions []string
}

// webcontentStepBrowser adapts the global webcontent browser to the verify
// StepBrowser seam: it converts the plan's steps field for field into the
// browse step language and issues one Run call with them.
type webcontentStepBrowser struct{}

// NewWebcontentStepBrowser returns the production StepBrowser backed by
// webcontent.GetGlobalBrowser(). The global browser is lazy (Chromium
// launches on first use), so this adapter is cheap to construct and
// WASM-safe: it never launches a browser at construction time.
func NewWebcontentStepBrowser() StepBrowser {
	return &webcontentStepBrowser{}
}

// RunSteps implements StepBrowser.
func (b *webcontentStepBrowser) RunSteps(ctx context.Context, baseURL string, steps []plancontract.BrowseStep) (*StepObservation, error) {
	renderer := webcontent.GetGlobalBrowser()
	result, err := renderer.Run(ctx, baseURL, webcontent.BrowseOptions{Steps: toBrowseSteps(steps)})
	if err != nil {
		return nil, err
	}
	return &StepObservation{
		FinalURL: result.FinalURL,
		Title:    result.Title,
		Actions:  result.Actions,
	}, nil
}

// toBrowseSteps converts the plan's scripted steps to the browse step
// language field for field. The two types are a pinned wire-format mirror
// (plancontract.BrowseStep deliberately mirrors webcontent.BrowseStep — see
// the plancontract doc and pkg/agent/plan_browse_compat_test.go), so this is
// a straight copy; no parsing or reinterpretation happens here.
func toBrowseSteps(steps []plancontract.BrowseStep) []webcontent.BrowseStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]webcontent.BrowseStep, 0, len(steps))
	for _, s := range steps {
		out = append(out, webcontent.BrowseStep{
			Action:         s.Action,
			Selector:       s.Selector,
			Value:          s.Value,
			Key:            s.Key,
			Millis:         s.Millis,
			Script:         s.Script,
			Expect:         s.Expect,
			ScreenshotPath: s.ScreenshotPath,
		})
	}
	return out
}

// runInteractionCheck executes one interaction check: start
// the manifest's dev server, run the plan's scripted browser steps in order
// against the dev server root, and confirm the expected outcome. Each
// interaction acceptance item is its own check with its own dev server,
// because each is an independent scripted flow with its own expected
// outcome — the shared-manifest grouping of page checks does not apply.
//
// Skipped checks (no manifest, no dev command, no dev port, no browser, or
// an unavailable browser) are recorded with a reason and never gate the
// result (the result says what could not be verified). A
// failed check is only ever one that started the server and found a concrete
// problem. The dev server is always stopped (the caller defers it), even on
// failure or cancellation.
//
// The check's steps come only from the plan's interaction acceptance item
// and its command from the manifest's dev command: the plan's acceptance
// Check field is never read for a command.
func (r *Runner) runInteractionCheck(ctx context.Context, root string, c *Check, manifest *startermanifest.StarterManifest) {
	switch {
	case manifest == nil:
		c.Skipped = true
		c.Reason = "no starter manifest: an interaction check needs the manifest's dev command and port"
		return
	case manifestDev(manifest) == "":
		c.Skipped = true
		c.Reason = "no dev command in the starter manifest"
		return
	case manifest.DevPort == 0:
		c.Skipped = true
		c.Reason = "no dev port in the starter manifest (dev_port is 0; runtime port discovery is not implemented)"
		return
	case r.StepBrowser == nil:
		c.Skipped = true
		c.Reason = "no browser configured"
		return
	}

	timeout := r.DevServerTimeout
	if timeout <= 0 {
		timeout = DefaultDevServerTimeout
	}
	devServer, reason := startDevServer(ctx, root, manifestDev(manifest), manifest.DevPort, timeout)
	defer stopDevServer(devServer)
	if reason != "" {
		c.Passed = false
		c.Reason = reason
		c.Excerpt = boundedExcerpt(devServer.output(), r.MaxExcerptBytes)
		return
	}

	obs, err := r.StepBrowser.RunSteps(ctx, devServerBaseURL(manifest.DevPort), c.Steps)
	if err != nil {
		if errors.Is(err, webcontent.ErrBrowserUnavailable) {
			c.Skipped = true
			c.Reason = "browser rendering not available"
			return
		}
		c.Passed = false
		c.Reason = stepFailureReason(c, err)
		c.Excerpt = boundedExcerpt(stepExcerpt(obs, err), r.MaxExcerptBytes)
		return
	}

	c.Passed = true
	c.Excerpt = boundedExcerpt(strings.Join(obs.Actions, "\n"), r.MaxExcerptBytes)
}

// stepFailureReason builds a failing interaction check's Reason: the item id
// followed by the step error. The webcontent error already names the failing
// step ("step[i] <action>: <detail>"), so the reason names both the item and
// the step.
func stepFailureReason(c *Check, err error) string {
	id := ""
	if len(c.Items) > 0 {
		id = c.Items[0]
	}
	if id == "" {
		return err.Error()
	}
	return id + ": " + err.Error()
}

// stepExcerpt builds a failing interaction check's Excerpt: the actions
// executed before the failure (when the browser reported them) followed by
// the step error. The production adapter returns a nil observation on a step
// failure (webcontent.Run returns (nil, err)), so a nil obs means the excerpt
// is the error alone.
func stepExcerpt(obs *StepObservation, err error) string {
	var lines []string
	if obs != nil {
		lines = append(lines, obs.Actions...)
	}
	lines = append(lines, err.Error())
	return strings.Join(lines, "\n")
}

// devServerBaseURL is the dev server root URL the interaction steps start
// from (devServerURL). Steps may navigate elsewhere themselves.
func devServerBaseURL(port int) string {
	return devServerURL(port, "/")
}
