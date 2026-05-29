// Command model_probe runs the capability probe against a single provider/model
// and prints the result as JSON. Intended for the daily registry pipeline to
// probe newly-seen models (where provider API keys are configured as secrets).
//
//	model_probe --provider openrouter --model anthropic/claude-3.5-sonnet
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/modelprobe"
)

func main() {
	provider := flag.String("provider", "", "provider ID (e.g. openrouter, deepinfra, openai)")
	model := flag.String("model", "", "model ID to probe")
	timeout := flag.Duration("timeout", 300*time.Second, "max time for the full probe (gates + complex stage)")
	inputCost := flag.Float64("input-cost", -1, "model's input price (USD per million tokens); -1 = unknown")
	outputCost := flag.Float64("output-cost", -1, "model's output price (USD per million tokens); -1 = unknown")
	maxProbeCost := flag.Float64("max-probe-cost", 0.10, "cost budget: skip models whose estimated per-probe cost exceeds this (USD); 0 disables")
	flag.Parse()

	if *provider == "" || *model == "" {
		fmt.Fprintln(os.Stderr, "usage: model_probe --provider <id> --model <id> [--input-cost N --output-cost N --max-probe-cost N --timeout 180s]")
		os.Exit(2)
	}

	modelprobe.LimitRequestTokens()

	// Cost guard: don't spend inference on models we can't confirm are within
	// budget. Emit a Skipped result (distinct from a failed probe).
	costKnown := *inputCost >= 0 && *outputCost >= 0
	if ok, reason := modelprobe.WithinCostBudget(*inputCost, *outputCost, costKnown, *maxProbeCost); !ok {
		emit(modelprobe.SkippedResult(*provider, *model, reason))
		return
	}

	clientType, err := api.ParseProviderName(*provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unknown provider %q: %v\n", *provider, err)
		os.Exit(2)
	}

	client, err := factory.CreateProviderClient(clientType, *model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create client for %s/%s: %v\n", *provider, *model, err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Run reports a probe transport failure as a non-passing Result (and an
	// error). We still emit the JSON either way; the exit code reflects only
	// whether we could produce a result, not whether the model passed — the
	// caller reads "passed" from the JSON.
	res, _ := modelprobe.Run(ctx, client, *provider, *model)
	emit(res)
}

func emit(res modelprobe.Result) {
	out, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode result: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}
