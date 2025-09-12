package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/spf13/cobra"
)

var pricingCmd = &cobra.Command{
	Use:   "pricing",
	Short: "Pricing utilities for managing model costs across all providers",
	Long: `Pricing utilities to sync model pricing from provider APIs, calculate costs, and manage pricing information.

The pricing system automatically syncs from multiple providers:
- DeepInfra: Direct API pricing extraction
- OpenRouter: Models API with pricing information  
- Built-in defaults for major model families

Examples:
  ledit pricing sync-all          # Sync from all provider APIs
  ledit pricing sync-openai       # Sync only OpenAI pricing
  ledit pricing sync-deepinfra    # Sync only DeepInfra pricing
  ledit pricing sync-openrouter   # Sync only OpenRouter pricing
  ledit pricing list              # Show all model pricing
  ledit pricing cost <model> <input-tokens> <output-tokens>  # Calculate cost`,
}

var pricingSyncAllCmd = &cobra.Command{
	Use:   "sync-all",
	Short: "Sync pricing from all provider APIs",
	RunE: func(cmd *cobra.Command, args []string) error {
		service := api.NewPricingService()
		if err := service.SyncAllProviders(); err != nil {
			return fmt.Errorf("failed to sync pricing: %w", err)
		}
		fmt.Println("✅ All provider pricing synced and saved")
		return nil
	},
}

var pricingSyncOpenAICmd = &cobra.Command{
	Use:   "sync-openai",
	Short: "Sync pricing from OpenAI models API and pricing page",
	RunE: func(cmd *cobra.Command, args []string) error {
		service := api.NewPricingService()
		if err := service.SyncOpenAIPricing(); err != nil {
			return fmt.Errorf("failed to sync OpenAI pricing: %w", err)
		}
		if err := service.SavePricingTable(); err != nil {
			return fmt.Errorf("failed to save pricing: %w", err)
		}
		fmt.Println("✅ OpenAI pricing synced and saved")
		return nil
	},
}

var pricingSyncDeepInfraCmd = &cobra.Command{
	Use:   "sync-deepinfra",
	Short: "Sync pricing from DeepInfra pricing API",
	RunE: func(cmd *cobra.Command, args []string) error {
		service := api.NewPricingService()
		if err := service.SyncDeepInfraPricing(); err != nil {
			return fmt.Errorf("failed to sync DeepInfra pricing: %w", err)
		}
		if err := service.SavePricingTable(); err != nil {
			return fmt.Errorf("failed to save pricing: %w", err)
		}
		fmt.Println("✅ DeepInfra pricing synced and saved")
		return nil
	},
}

var pricingSyncOpenRouterCmd = &cobra.Command{
	Use:   "sync-openrouter", 
	Short: "Sync pricing from OpenRouter models API",
	RunE: func(cmd *cobra.Command, args []string) error {
		service := api.NewPricingService()
		if err := service.SyncOpenRouterPricing(); err != nil {
			return fmt.Errorf("failed to sync OpenRouter pricing: %w", err)
		}
		if err := service.SavePricingTable(); err != nil {
			return fmt.Errorf("failed to save pricing: %w", err)
		}
		fmt.Println("✅ OpenRouter pricing synced and saved")
		return nil
	},
}

var pricingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all models with their pricing information",
	RunE: func(cmd *cobra.Command, args []string) error {
		service := api.NewPricingService()
		models := service.ListModels()
		
		if len(models) == 0 {
			fmt.Println("No pricing data available. Run 'ledit pricing sync-all' to fetch pricing.")
			return nil
		}
		
		// Create sorted list for consistent output
		type modelInfo struct {
			name   string
			pricing types.ModelPricing
		}
		
		var modelList []modelInfo
		for name, pricing := range models {
			modelList = append(modelList, modelInfo{name: name, pricing: pricing})
		}
		
		sort.Slice(modelList, func(i, j int) bool {
			return modelList[i].name < modelList[j].name
		})
		
		// Print table
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MODEL\tINPUT ($/1K)\tOUTPUT ($/1K)\tPROVIDER")
		fmt.Fprintln(w, "-----\t----------\t-----------\t--------")
		
		for _, model := range modelList {
			provider := "unknown"
			if strings.Contains(model.name, ":") {
				provider = strings.Split(model.name, ":")[0]
			} else if strings.Contains(model.name, "/") {
				provider = strings.Split(model.name, "/")[0]
			}
			
			inputCost := "unknown"
			outputCost := "unknown"
			
			if model.pricing.InputCostPer1K >= 0 {
				inputCost = fmt.Sprintf("$%.6f", model.pricing.InputCostPer1K)
			}
			if model.pricing.OutputCostPer1K >= 0 {
				outputCost = fmt.Sprintf("$%.6f", model.pricing.OutputCostPer1K)
			}
			
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", 
				model.name, 
				inputCost,
				outputCost,
				provider)
		}
		w.Flush()
		
		fmt.Printf("\nTotal models: %d\n", len(models))
		return nil
	},
}

var pricingCostCmd = &cobra.Command{
	Use:   "cost <model> <input-tokens> <output-tokens>",
	Short: "Calculate cost for a specific model and token usage",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelName := args[0]
		
		var inputTokens, outputTokens int
		if _, err := fmt.Sscanf(args[1], "%d", &inputTokens); err != nil {
			return fmt.Errorf("invalid input tokens: %s", args[1])
		}
		if _, err := fmt.Sscanf(args[2], "%d", &outputTokens); err != nil {
			return fmt.Errorf("invalid output tokens: %s", args[2])
		}
		
		service := api.NewPricingService()
		pricing := service.GetModelPricing(modelName)
		
		fmt.Printf("💰 Cost calculation for %s:\n", modelName)
		
		if pricing.InputCostPer1K < 0 || pricing.OutputCostPer1K < 0 {
			fmt.Printf("   Pricing information: unknown\n")
			fmt.Printf("   Run 'ledit pricing sync-all' to fetch current pricing data.\n")
			return nil
		}
		
		inputCost := float64(inputTokens) * pricing.InputCostPer1K / 1000
		outputCost := float64(outputTokens) * pricing.OutputCostPer1K / 1000
		totalCost := inputCost + outputCost
		
		fmt.Printf("   Input:  %d tokens × $%.6f/1K = $%.6f\n", inputTokens, pricing.InputCostPer1K, inputCost)
		fmt.Printf("   Output: %d tokens × $%.6f/1K = $%.6f\n", outputTokens, pricing.OutputCostPer1K, outputCost)
		fmt.Printf("   Total:  $%.6f\n", totalCost)
		
		return nil
	},
}

func init() {
	pricingCmd.AddCommand(pricingSyncAllCmd)
	pricingCmd.AddCommand(pricingSyncOpenAICmd)
	pricingCmd.AddCommand(pricingSyncDeepInfraCmd)
	pricingCmd.AddCommand(pricingSyncOpenRouterCmd)
	pricingCmd.AddCommand(pricingListCmd)
	pricingCmd.AddCommand(pricingCostCmd)
}
