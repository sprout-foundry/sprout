//go:build !js && !mlx

package cmd

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// onboardingLocal handles the sprout-local provider onboarding flow.
// On builds without MLX support, this explains that local AI is not available.
func onboardingLocal() (string, bool) {
	fmt.Println()
	fmt.Println("═ Local AI Setup ═")
	fmt.Println()
	console.GlyphWarning.Printf("Local AI is not available in this build.")
	fmt.Println()
	fmt.Println("Local AI requires Apple Silicon (M1/M2/M3/M4) with the MLX")
	fmt.Println("framework installed. On this platform or build, it's not available.")
	fmt.Println()
	fmt.Println("You can still use cloud providers — try one of those instead.")
	return "", false
}
