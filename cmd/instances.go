package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"time"
)

var instancesCmd = &cobra.Command{
	Use:   "instances",
	Short: "List running ledit instances",
	Long: `Lists all currently running ledit instances across different projects.
This command reads from ~/.ledit/instances.json to discover active instances.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listInstances()
	},
}

func listInstances() error {
	instances, err := loadInstances()
	if err != nil {
		return fmt.Errorf("failed to load instances: %w", err)
	}

	// Clean up stale instances
	now := time.Now()
	staleThreshold := now.Add(-5 * time.Minute)
	cleanStaleInstances(instances, staleThreshold)

	if len(instances) == 0 {
		fmt.Println("No running instances found.")
		fmt.Println("Start ledit with: ledit agent")
		return nil
	}

	// Display instances
	fmt.Printf("\n📋 Running Ledit Instances (%d)\n\n", len(instances))
	for _, info := range instances {
		uptime := now.Sub(info.StartTime)
		fmt.Printf("  🖥️  Port %d\n", info.Port)
		fmt.Printf("      PID: %d\n", info.PID)
		fmt.Printf("      ID: %s\n", info.ID)
		fmt.Printf("      Dir: %s\n", info.WorkingDir)
		fmt.Printf("      Uptime: %s\n", uptime.String())
		fmt.Printf("      Last ping: %s ago\n", now.Sub(info.LastPing).Round(time.Second))
		fmt.Println()
	}

	fmt.Println("Connect to an instance at: http://localhost:<port>")

	return nil
}

func init() {
	rootCmd.AddCommand(instancesCmd)
}
