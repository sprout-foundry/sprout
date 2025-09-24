package cmd

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long: `Print detailed version information including:
• Application version
• Go runtime version
• Build information
• Git commit hash (if available)

This command supports both --version and -v flags as well as the standalone version command.`,
	Run: func(cmd *cobra.Command, args []string) {
		printVersionInfo()
	},
}

// versionInfo holds the build-time version information
var (
	// These variables are set at build time using -ldflags
	version     = "dev"     // Semantic version (e.g., "v1.0.0")
	buildDate   = "unknown" // Build timestamp
	gitCommit   = ""        // Git commit hash
	gitTag      = ""        // Git tag (if building from tag)
	goVersion   = runtime.Version()
)

// init adds flags and sets up the version command
func init() {
	// Add version command to root
	rootCmd.AddCommand(versionCmd)
	
	// Add --version and -v flags to root command
	rootCmd.Flags().BoolP("version", "v", false, "Print version information and exit")
	
	// Hook into the root command's pre-run to handle version flags
	originalPreRun := rootCmd.PersistentPreRun
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Check if version flag is set
		if versionFlag, _ := cmd.Flags().GetBool("version"); versionFlag {
			printVersionInfo()
			os.Exit(0)
		}
		
		// Call original pre-run if it exists
		if originalPreRun != nil {
			originalPreRun(cmd, args)
		}
	}
}

// printVersionInfo prints comprehensive version information
func printVersionInfo() {
	fmt.Printf("ledit version %s\n", version)
	
	// Add build information if available
	if buildDate != "unknown" {
		fmt.Printf("Build date: %s\n", buildDate)
	}
	
	// Add git information if available
	if gitCommit != "" {
		fmt.Printf("Git commit: %s\n", gitCommit)
		if gitTag != "" && gitTag != version {
			fmt.Printf("Git tag: %s\n", gitTag)
		}
	}
	
	fmt.Printf("Go version: %s\n", goVersion)
	
	// Add module information from build info
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Printf("Module: %s\n", info.Main.Path)
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			fmt.Printf("Module version: %s\n", info.Main.Version)
		}
	}
	
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}