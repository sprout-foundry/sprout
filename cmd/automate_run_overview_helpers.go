//go:build !js

package cmd

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// printPriceCard renders the provider/model rates for the initial agent and
// each subagent persona that will run. It walks pricing for every model
// named in the workflow so the user sees the actual rate card before
// approving the run. Unknown rates are shown explicitly as "unknown" — we
// never fabricate a price. Followed by a footer when any row is incomplete.
//
// Split from automate_run.go to keep that file under the AGENTS.md 500-line
// guideline. All callers live in the same `cmd` package and reach this
// helper directly; no exported surface changes.
func printPriceCard(summary *automate.Summary) {
	if summary == nil || summary.Initial == nil {
		return
	}

	type row struct {
		Role        string
		Persona     string
		Provider    string
		Model       string
		InputUsd    float64
		OutputUsd   float64
		HasPricing  bool
		IsInherited bool
	}

	rows := []row{}
	primaryProvider := summary.Initial.Provider
	primaryModel := summary.Initial.Model
	if primaryProvider != "" && primaryModel != "" {
		p := lookupModelPricing(primaryProvider, primaryModel)
		rows = append(rows, row{
			Role:       "Initial",
			Persona:    displayOrDefault(summary.Initial.Persona, "default"),
			Provider:   primaryProvider,
			Model:      primaryModel,
			InputUsd:   p.InputUsdPerM,
			OutputUsd:  p.OutputUsdPerM,
			HasPricing: p.HasPricing,
		})
	}

	for _, ov := range summary.Initial.SubagentOverrides {
		provider := ov.Provider
		model := ov.Model
		inherited := false
		if provider == "" {
			provider = primaryProvider
			inherited = true
		}
		if model == "" {
			model = primaryModel
			inherited = true
		}
		if provider == "" || model == "" {
			rows = append(rows, row{
				Role:        "Subagent",
				Persona:     ov.Persona,
				Provider:    displayOrDefault(provider, "(inherit)"),
				Model:       displayOrDefault(model, "(inherit)"),
				IsInherited: inherited,
			})
			continue
		}
		p := lookupModelPricing(provider, model)
		rows = append(rows, row{
			Role:        "Subagent",
			Persona:     ov.Persona,
			Provider:    provider,
			Model:       model,
			InputUsd:    p.InputUsdPerM,
			OutputUsd:   p.OutputUsdPerM,
			HasPricing:  p.HasPricing,
			IsInherited: inherited,
		})
	}

	if len(rows) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("Models that will run:")
	missing := 0
	for _, r := range rows {
		priceCol := "      pricing: unknown"
		if r.HasPricing {
			priceCol = fmt.Sprintf("$%6.2f / $%6.2f per Mtok", r.InputUsd, r.OutputUsd)
		} else {
			missing++
		}
		inheritedTag := ""
		if r.IsInherited {
			inheritedTag = " (inherited)"
		}
		fmt.Printf("  %-9s %-20s %-13s %-30s %s%s\n",
			r.Role, r.Persona, r.Provider, r.Model, priceCol, inheritedTag,
		)
	}
	if missing > 0 {
		console.GlyphWarning.Printf("Pricing data incomplete for %d of %d models — actual cost may exceed what's shown.",
			missing, len(rows))
	}
}

// printBudgetLine renders the configured USD budget if set, including warn
// thresholds expressed in dollars (not just fractions) so the user sees the
// concrete numbers they'll be billed against.
func printBudgetLine(summary *automate.Summary) {
	if summary == nil || summary.Budget == nil || summary.Budget.USD <= 0 {
		return
	}
	parts := []string{fmt.Sprintf("$%.2f USD cap", summary.Budget.USD)}
	if len(summary.Budget.WarnAt) > 0 {
		dollars := make([]string, 0, len(summary.Budget.WarnAt))
		for _, t := range summary.Budget.WarnAt {
			dollars = append(dollars, fmt.Sprintf("$%.2f", t*summary.Budget.USD))
		}
		parts = append(parts, "warn at "+strings.Join(dollars, ", "))
	}
	fmt.Println()
	fmt.Printf("Budget: %s\n", strings.Join(parts, ", "))
}

// printAllowedPaths renders the "External paths this workflow will access"
// section introduced by SP-128 Phase 2. Suppressed entirely when the
// workflow declares no allowed_paths — workflows that stay inside the
// workspace see no new noise. Layout:
//
//	External paths this workflow will access:
//
//	  • /srv/datasets           [read_write]
//	    "Read training data"
//	  • /var/log/sprout         [read_only]
//	    "Tail logs for the run"
//
// The mode label is right-aligned via fmt padding so the bracket edges line
// up across entries; the reason is indented 4 spaces on the next line and
// only emitted when the entry has one. The longest path is measured once so
// a single workflow with N entries prints N times without re-scanning.
func printAllowedPaths(summary *automate.Summary) {
	if summary == nil || len(summary.AllowedPaths) == 0 {
		return
	}

	maxPath := 0
	for _, ap := range summary.AllowedPaths {
		if len(ap.Path) > maxPath {
			maxPath = len(ap.Path)
		}
	}

	fmt.Println()
	fmt.Println("External paths this workflow will access:")
	for _, ap := range summary.AllowedPaths {
		fmt.Printf("  \u2022 %-*s  [%s]\n", maxPath, ap.Path, ap.Mode)
		if reason := strings.TrimSpace(ap.Reason); reason != "" {
			fmt.Printf("    \"%s\"\n", reason)
		}
	}
}

// displayOrDefault returns fallback when value is empty after trimming
// whitespace. Used by the overview renderer to swap placeholder strings for
// unset workflow fields.
func displayOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// stepDetail returns the human-readable one-liner used in the overview
// step list. For shell steps, the command preview is shown verbatim; for
// agent steps, the persona/provider/model/when tuple is concatenated.
func stepDetail(step automate.StepSummary) string {
	switch step.Kind {
	case "shell":
		if step.CommandPreview != "" {
			return step.CommandPreview
		}
		return "(shell command)"
	default:
		details := []string{}
		if step.Persona != "" {
			details = append(details, "persona="+step.Persona)
		}
		if step.Provider != "" {
			details = append(details, "provider="+step.Provider)
		}
		if step.Model != "" {
			details = append(details, "model="+step.Model)
		}
		if step.When != "" && step.When != "always" {
			details = append(details, "when="+step.When)
		}
		if len(details) == 0 {
			return "(inference)"
		}
		return strings.Join(details, " ")
	}
}
