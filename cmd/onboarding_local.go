//go:build !js && (!darwin || !arm64 || !cgo)

package cmd

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// localAIAvailable reports whether this build can run the local provider.
func localAIAvailable() bool { return false }

// onboardingLocal handles the sprout-local provider onboarding flow.
// On builds without MLX support, this explains that local AI is not available.
func onboardingLocal() (string, bool) {
	fmt.Println()
	fmt.Println("═ Local AI Setup ═")
	fmt.Println()
	console.GlyphWarning.Printf("Local AI is not available in this build.")
	fmt.Println()
	fmt.Println("Local AI requires a Mac with Apple Silicon (M1 or later).")
	fmt.Println()
	fmt.Println("You can still use cloud providers — try one of those instead.")
	return "", false
}
