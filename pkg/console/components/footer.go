package components

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/console"
)

// FooterComponent displays status information at the bottom of the terminal
type FooterComponent struct {
	*console.BaseComponent
	lastModel         string
	lastProvider      string
	lastTokens        int
	lastCost          float64
	lastIteration     int
	lastContextTokens int
	maxContextTokens  int
	sessionStart      time.Time
	outputMutex       *sync.Mutex

	// Git and path information
	gitBranch   string
	gitChanges  int
	gitRemote   string
	currentPath string
	isGitRepo   bool
}

// NewFooterComponent creates a new footer component
func NewFooterComponent() *FooterComponent {
	return &FooterComponent{
		BaseComponent: console.NewBaseComponent("footer", "FooterComponent"),
		sessionStart:  time.Now(),
	}
}

// Init initializes the footer component
func (fc *FooterComponent) Init(ctx context.Context, deps console.Dependencies) error {
	if err := fc.BaseComponent.Init(ctx, deps); err != nil {
		return err
	}

	// Define footer region at bottom of terminal
	width, height, _ := deps.Terminal.GetSize()
	region := console.Region{
		X:       0,
		Y:       height - 4, // 4 lines for footer (blank separator, git, path, stats)
		Width:   width,
		Height:  4,
		ZOrder:  100, // High z-order to stay on top
		Visible: true,
	}

	if err := deps.Layout.DefineRegion("footer", region); err != nil {
		return err
	}
	fc.SetRegion("footer")

	// Subscribe to state changes
	deps.State.Subscribe("footer.model", func(key string, oldValue, newValue interface{}) {
		if model, ok := newValue.(string); ok {
			fc.lastModel = model
			fc.SetNeedsRedraw(true)
		}
	})

	deps.State.Subscribe("footer.provider", func(key string, oldValue, newValue interface{}) {
		if provider, ok := newValue.(string); ok {
			fc.lastProvider = provider
			fc.SetNeedsRedraw(true)
		}
	})

	deps.State.Subscribe("footer.tokens", func(key string, oldValue, newValue interface{}) {
		if tokens, ok := newValue.(int); ok {
			fc.lastTokens = tokens
			fc.SetNeedsRedraw(true)
		}
	})

	deps.State.Subscribe("footer.cost", func(key string, oldValue, newValue interface{}) {
		if cost, ok := newValue.(float64); ok {
			fc.lastCost = cost
			fc.SetNeedsRedraw(true)
		}
	})

	deps.State.Subscribe("footer.iteration", func(key string, oldValue, newValue interface{}) {
		if iteration, ok := newValue.(int); ok {
			fc.lastIteration = iteration
			fc.SetNeedsRedraw(true)
		}
	})

	deps.State.Subscribe("footer.contextTokens", func(key string, oldValue, newValue interface{}) {
		if tokens, ok := newValue.(int); ok {
			fc.lastContextTokens = tokens
			fc.SetNeedsRedraw(true)
		}
	})

	deps.State.Subscribe("footer.maxContextTokens", func(key string, oldValue, newValue interface{}) {
		if tokens, ok := newValue.(int); ok {
			fc.maxContextTokens = tokens
			fc.SetNeedsRedraw(true)
		}
	})

	// Subscribe to terminal resize events
	deps.Events.Subscribe("terminal.resized", func(event console.Event) error {
		// Update footer position
		if data, ok := event.Data.(map[string]int); ok {
			width := data["width"]
			height := data["height"]

			region := console.Region{
				X:       0,
				Y:       height - 4,
				Width:   width,
				Height:  4,
				ZOrder:  100,
				Visible: true,
			}

			deps.Layout.UpdateRegion("footer", region)
			fc.SetNeedsRedraw(true)
		}
		return nil
	})

	return nil
}

// Render renders the footer
func (fc *FooterComponent) Render() error {
	// Use output mutex to prevent interleaving with agent output
	if fc.outputMutex != nil {
		fc.outputMutex.Lock()
		defer fc.outputMutex.Unlock()
	}

	region, err := fc.Layout().GetRegion("footer")
	if err != nil {
		return err
	}

	// Save cursor position
	fc.Terminal().SaveCursor()
	defer fc.Terminal().RestoreCursor()

	// First line: Blank separator with blue-grey background
	fc.Terminal().MoveCursor(region.X+1, region.Y+1) // 1-based coordinates
	fc.Terminal().ClearLine()
	// Using ANSI 256 color 67 - a more blue-toned grey
	fc.Terminal().Write([]byte("\033[48;2;50;54;62m"))
	fc.Terminal().Write([]byte(strings.Repeat(" ", region.Width)))
	fc.Terminal().Write([]byte("\033[0m"))

	// Second line: Current path
	fc.Terminal().MoveCursor(region.X+1, region.Y+2)
	fc.Terminal().ClearLine()

	// Replace home directory with ~
	displayPath := fc.currentPath
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(displayPath, home) {
		displayPath = "~" + strings.TrimPrefix(displayPath, home)
	}

	fc.Terminal().Write([]byte("\033[48;2;50;54;62m\033[37m")) // Blue-grey bg, normal white text
	pathLine := fmt.Sprintf("  %s", displayPath)          // No icon
	// Truncate path if too long
	if len(pathLine) > region.Width-2 {
		pathLine = "  ..." + pathLine[len(pathLine)-(region.Width-7):]
	}
	fc.Terminal().Write([]byte(pathLine))
	// Pad the rest
	padding := region.Width - len(pathLine)
	if padding > 0 {
		fc.Terminal().Write([]byte(strings.Repeat(" ", padding)))
	}
	fc.Terminal().Write([]byte("\033[0m"))

	// Third line: Git information (if in git repo)
	fc.Terminal().MoveCursor(region.X+1, region.Y+3)
	fc.Terminal().ClearLine()

	// Blue-grey background continues
	if fc.isGitRepo && fc.gitBranch != "" {
		fc.Terminal().Write([]byte("\033[48;2;50;54;62m")) // Blue-grey bg

		// Format git line: remote in darker text, branch in lighter
		gitLine := "  "
		if fc.gitRemote != "" {
			// Remote in darker gray text (color 243)
			gitLine += fmt.Sprintf("\033[38;5;243m%s\033[0m\033[48;2;50;54;62m", fc.gitRemote)
			gitLine += ":"
		}
		// Branch name in lighter gray (color 250)
		gitLine += fmt.Sprintf("\033[38;5;250m%s\033[0m\033[48;2;50;54;62m", fc.gitBranch)

		// Changes in default white
		if fc.gitChanges > 0 {
			gitLine += fmt.Sprintf("\033[37m (+%d)\033[0m\033[48;2;50;54;62m", fc.gitChanges)
		}

		fc.Terminal().Write([]byte(gitLine))
		// Pad the rest of the line - need to calculate visible length
		visibleLen := 2 + len(fc.gitRemote) + 1 + len(fc.gitBranch) // 2 for spaces, 1 for colon
		if fc.gitChanges > 0 {
			visibleLen += len(fmt.Sprintf(" (+%d)", fc.gitChanges))
		}
		padding := region.Width - visibleLen
		if padding > 0 {
			fc.Terminal().Write([]byte(strings.Repeat(" ", padding)))
		}
		fc.Terminal().Write([]byte("\033[0m"))
	} else {
		// No git repo - fill with blue-grey
		fc.Terminal().Write([]byte("\033[48;2;50;54;62m"))
		fc.Terminal().Write([]byte(strings.Repeat(" ", region.Width)))
		fc.Terminal().Write([]byte("\033[0m"))
	}

	// Fourth line: Model and stats (existing footer)
	fc.Terminal().MoveCursor(region.X+1, region.Y+4)
	fc.Terminal().ClearLine()

	// Format cost with appropriate precision
	var costStr string
	if fc.lastCost >= 1.0 {
		costStr = fmt.Sprintf("$%.2f", fc.lastCost)
	} else if fc.lastCost >= 0.01 {
		costStr = fmt.Sprintf("$%.3f", fc.lastCost)
	} else if fc.lastCost > 0 {
		costStr = fmt.Sprintf("$%.6f", fc.lastCost)
	} else {
		costStr = "$0.000"
	}

	// Format context usage (simplified - no "Context:" prefix)
	contextStr := ""
	if fc.maxContextTokens > 0 {
		contextPercent := float64(fc.lastContextTokens) / float64(fc.maxContextTokens) * 100
		contextStr = fmt.Sprintf(" | %s/%s (%.0f%%)",
			fc.formatTokens(fc.lastContextTokens),
			fc.formatTokens(fc.maxContextTokens),
			contextPercent)
	}

	// Format iteration
	iterStr := ""
	if fc.lastIteration > 0 {
		iterStr = fmt.Sprintf(" | Iter: %d", fc.lastIteration)
	}

	// Model info for left side (light background)
	leftPad := "  "       // 2 spaces for left padding
	rightModelPad := "  " // 2 spaces for right padding after model text

	// Format provider (model) - provider name first, then model
	modelName := fc.extractModelName(fc.lastModel)
	modelText := fmt.Sprintf("%s (%s)", fc.lastProvider, modelName)

	// If model section would be too long, truncate the model name
	maxModelSectionWidth := region.Width / 2 // Use max half the screen for model info
	modelSection := leftPad + modelText + rightModelPad

	if len(modelSection) > maxModelSectionWidth {
		// Calculate how much space we have for the model name after provider
		availableForModel := maxModelSectionWidth - len(leftPad) - len(rightModelPad) - len(fc.lastProvider) - 3 // 3 for " ()"
		if availableForModel > 3 {
			truncatedModel := modelName
			if len(modelName) > availableForModel {
				truncatedModel = modelName[:availableForModel-3] + "..."
			}
			modelText = fmt.Sprintf("%s (%s)", fc.lastProvider, truncatedModel)
			modelSection = leftPad + modelText + rightModelPad
		} else {
			// Just show provider if no room for model
			modelText = fc.lastProvider
			modelSection = leftPad + modelText + rightModelPad
		}
	}

	modelSectionLen := len(modelSection)

	// Stats content for right side (simplified - no "tokens" word)
	rightPad := "  " // 2 spaces for right padding
	statsContent := fmt.Sprintf(
		"%s | %s%s%s",
		fc.formatTokens(fc.lastTokens),
		costStr,
		contextStr,
		iterStr,
	)

	statsSection := statsContent + rightPad
	statsSectionLen := len(statsSection)

	// Check if everything fits with at least some gap
	minGap := 2 // Minimum gap between sections
	if modelSectionLen+statsSectionLen+minGap <= region.Width {
		// Light gray background wrapped exactly to model text
		fc.Terminal().Write([]byte("\033[47m\033[30m")) // Light gray background, black text
		fc.Terminal().Write([]byte(modelSection))
		fc.Terminal().Write([]byte("\033[0m")) // Reset colors immediately after text

		// Fill the rest of the line with dark background
		remainingSpace := region.Width - modelSectionLen

		// Dark background for the entire remaining space
		fc.Terminal().Write([]byte("\033[2m\033[37m\033[40m")) // Dim white text with dark gray background

		// Add spaces to position stats at the right
		paddingBeforeStats := remainingSpace - statsSectionLen
		if paddingBeforeStats > 0 {
			fc.Terminal().Write([]byte(strings.Repeat(" ", paddingBeforeStats)))
		}

		// Write stats content
		fc.Terminal().Write([]byte(statsSection))

		// Fill any remaining space to the end of line with dark background
		fc.Terminal().ClearToEndOfLine()

		// Reset colors
		fc.Terminal().Write([]byte("\033[0m"))
	} else {
		// If it doesn't fit, just show model info with dark background for the rest
		fc.Terminal().Write([]byte("\033[47m\033[30m")) // Light gray background, black text
		fc.Terminal().Write([]byte(modelSection))
		fc.Terminal().Write([]byte("\033[0m")) // Reset colors

		// Fill the rest with dark background
		fc.Terminal().Write([]byte("\033[2m\033[37m\033[40m")) // Dim white text with dark gray background
		fc.Terminal().ClearToEndOfLine()
		fc.Terminal().Write([]byte("\033[0m")) // Reset colors
	}

	// Mark as drawn
	fc.SetNeedsRedraw(false)

	return nil
}

// UpdateStats updates the footer statistics
func (fc *FooterComponent) UpdateStats(model, provider string, tokens int, cost float64, iteration, contextTokens, maxContextTokens int) {
	fc.State().Set("footer.model", model)
	fc.State().Set("footer.provider", provider)
	fc.State().Set("footer.tokens", tokens)
	fc.State().Set("footer.cost", cost)
	fc.State().Set("footer.iteration", iteration)
	fc.State().Set("footer.contextTokens", contextTokens)
	fc.State().Set("footer.maxContextTokens", maxContextTokens)
}

// UpdateGitInfo updates git information
func (fc *FooterComponent) UpdateGitInfo(branch string, changes int, isRepo bool) {
	fc.gitBranch = branch
	fc.gitChanges = changes
	fc.isGitRepo = isRepo
	fc.SetNeedsRedraw(true)
}

// UpdateGitRemote updates git remote information
func (fc *FooterComponent) UpdateGitRemote(remote string) {
	fc.gitRemote = remote
	fc.SetNeedsRedraw(true)
}

// UpdatePath updates the current path
func (fc *FooterComponent) UpdatePath(path string) {
	fc.currentPath = path
	fc.SetNeedsRedraw(true)
}

// SetOutputMutex sets the output mutex for synchronized output
func (fc *FooterComponent) SetOutputMutex(mu *sync.Mutex) {
	fc.outputMutex = mu
}

// formatTokens formats token count for display
func (fc *FooterComponent) formatTokens(tokens int) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
	} else if tokens >= 1000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

// formatDuration formats a duration for display
func (fc *FooterComponent) formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// extractModelName extracts the model name from a provider/model string
func (fc *FooterComponent) extractModelName(fullModel string) string {
	// Split on "/" and take the last part (actual model name)
	parts := strings.Split(fullModel, "/")
	if len(parts) >= 2 {
		modelName := parts[len(parts)-1]

		// Smart truncation for common model patterns
		if len(modelName) > 20 {
			// First check if it's a free model - preserve ":free" suffix
			if strings.HasSuffix(modelName, ":free") {
				// Remove ":free" temporarily for processing
				baseName := strings.TrimSuffix(modelName, ":free")

				// Apply pattern-specific truncation to base name
				var truncatedBase string
				if strings.Contains(baseName, "Qwen") && strings.Contains(baseName, "Coder") {
					// Extract: Qwen3-Coder-480B from Qwen3-Coder-480B-A35B-Instruct-Turbo
					if parts := strings.Split(baseName, "-"); len(parts) >= 3 {
						truncatedBase = strings.Join(parts[:3], "-")
					} else {
						truncatedBase = baseName
					}
				} else if strings.Contains(baseName, "deepseek") {
					// Keep deepseek base name, truncate if too long
					if len(baseName) > 12 { // Leave room for ":free"
						truncatedBase = baseName[:12] + "..."
					} else {
						truncatedBase = baseName
					}
				} else {
					// Generic truncation for other free models
					if len(baseName) > 12 { // Leave room for ":free"
						truncatedBase = baseName[:12] + "..."
					} else {
						truncatedBase = baseName
					}
				}

				return truncatedBase + ":free"
			}

			// Non-free model patterns
			if strings.Contains(modelName, "Qwen") && strings.Contains(modelName, "Coder") {
				// Extract: Qwen3-Coder-480B
				if parts := strings.Split(modelName, "-"); len(parts) >= 3 {
					return strings.Join(parts[:3], "-") // e.g., "Qwen3-Coder-480B"
				}
			} else if strings.Contains(modelName, "deepseek") {
				// For non-free deepseek models, truncate base name if needed
				if parts := strings.Split(modelName, ":"); len(parts) > 1 {
					base := parts[0]
					if len(base) > 15 {
						return base[:15] + "..."
					}
					return base
				}
			}

			// Generic fallback: keep first 17 chars
			return modelName[:17] + "..."
		}
		return modelName
	}
	// Fallback to truncating the full string if no "/" found
	return fc.truncateString(fullModel, 20)
}

// truncateString truncates a string to maxLen
func (fc *FooterComponent) truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// OnResize handles terminal resize events
func (fc *FooterComponent) OnResize(width, height int) {
	// Get the old region to clear it
	oldRegion, _ := fc.Layout().GetRegion("footer")

	// Save cursor position
	fc.Terminal().SaveCursor()

	// Clear all old footer lines (we now have 3 lines)
	if oldRegion.Y > 0 && oldRegion.Height > 0 {
		for i := 0; i < oldRegion.Height; i++ {
			if oldRegion.Y+i+1 <= height {
				fc.Terminal().MoveCursor(1, oldRegion.Y+i+1)
				fc.Terminal().ClearLine()
			}
		}
	}

	// Update footer position
	region := console.Region{
		X:       0,
		Y:       height - 4,
		Width:   width,
		Height:  4,
		ZOrder:  100,
		Visible: true,
	}

	// Clear all 4 lines at the new footer position
	for i := 0; i < 4; i++ {
		if region.Y+i+1 <= height {
			fc.Terminal().MoveCursor(1, region.Y+i+1)
			fc.Terminal().ClearLine()
		}
	}

	// Restore cursor
	fc.Terminal().RestoreCursor()

	// Update the layout
	fc.Layout().UpdateRegion("footer", region)
	fc.SetNeedsRedraw(true)

	// Add a small delay to let terminal stabilize after resize
	time.Sleep(10 * time.Millisecond)

	// Force a fresh render
	fc.Render()
}