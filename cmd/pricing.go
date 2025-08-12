package cmd

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/spf13/cobra"
)

var pricingCmd = &cobra.Command{
	Use:   "pricing",
	Short: "Pricing utilities",
}

var pricingSyncDeepInfraCmd = &cobra.Command{
	Use:   "sync-deepinfra [url]",
	Short: "Sync pricing from a DeepInfra pricing JSON URL (or auto-discover if omitted)",
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := ""
		if len(args) == 1 {
			url = args[0]
		}
		if err := llm.InitPricingTable(); err != nil {
			return err
		}
		if err := llm.SyncDeepInfraPricing(url); err != nil {
			return err
		}
		fmt.Println("DeepInfra pricing synced and saved to .ledit/model_pricing.json")
		return nil
	},
}

func init() {
	pricingCmd.AddCommand(pricingSyncDeepInfraCmd)
}
