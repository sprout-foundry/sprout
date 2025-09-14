package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/console"
)

// FooterComponent displays status information at the bottom of the terminal
type FooterComponent struct {
	*console.BaseComponent
	lastModel    string
	lastProvider string
	lastTokens   int
	lastCost     float64
	sessionStart time.Time
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
		Y:       height - 2, // 2 lines for footer (top border + stats)
		Width:   width,
		Height:  2,
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

	// Subscribe to terminal resize events
	deps.Events.Subscribe("terminal.resized", func(event console.Event) error {
		// Update footer position
		if data, ok := event.Data.(map[string]int); ok {
			width := data["width"]
			height := data["height"]

			region := console.Region{
				X:       0,
				Y:       height - 2,
				Width:   width,
				Height:  2,
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
	region, err := fc.Layout().GetRegion("footer")
	if err != nil {
		return err
	}

	// Save cursor position
	fc.Terminal().SaveCursor()
	defer fc.Terminal().RestoreCursor()

	// Move to footer position
	fc.Terminal().MoveCursor(region.X+1, region.Y+1) // 1-based coordinates

	// Move to stats line
	fc.Terminal().MoveCursor(region.X+1, region.Y+2)

	// Format stats
	sessionDuration := fc.formatDuration(time.Since(fc.sessionStart))
	statsLine := fmt.Sprintf(
		" %s (%s) | %s tokens | $%.3f | %s",
		fc.extractModelName(fc.lastModel),
		fc.lastProvider,
		fc.formatTokens(fc.lastTokens),
		fc.lastCost,
		sessionDuration,
	)

	// Truncate if too long
	if len(statsLine) > region.Width {
		statsLine = statsLine[:region.Width-3] + "..."
	}

	// Draw stats with dark gray background (#444) and light text
	// Using dim white text (color code 37) with a dark gray background (color code 40 for black, closest to #444)
	fc.Terminal().Write([]byte("\033[2m\033[37m\033[40m")) // Dim white text with dark gray background
	fc.Terminal().Write([]byte(statsLine))
	fc.Terminal().ClearToEndOfLine()
	fc.Terminal().Write([]byte("\033[0m"))

	// Mark as drawn
	fc.SetNeedsRedraw(false)

	return nil
}

// UpdateStats updates the footer statistics
func (fc *FooterComponent) UpdateStats(model, provider string, tokens int, cost float64) {
	fc.State().Set("footer.model", model)
	fc.State().Set("footer.provider", provider)
	fc.State().Set("footer.tokens", tokens)
	fc.State().Set("footer.cost", cost)
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
	// Update footer position
	region := console.Region{
		X:       0,
		Y:       height - 2,
		Width:   width,
		Height:  2,
		ZOrder:  100,
		Visible: true,
	}

	fc.Layout().UpdateRegion("footer", region)
	fc.SetNeedsRedraw(true)
	fc.Render()
}
