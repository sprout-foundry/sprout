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

// FooterConfig holds configurable values for the footer
type FooterConfig struct {
	NarrowWidthThreshold int
	DefaultHeight        int
	NarrowHeight         int
	ZOrder               int
	Colors               struct {
		BgBlueGrey    string // "\033[48;2;50;54;62m"
		TextWhite     string // "\033[37m"
		LightGrayBg   string // "\033[47m"
		BlackText     string // "\033[30m"
		DimWhite      string // "\033[2m\033[37m"
		DarkGrayBg    string // "\033[40m"
		DarkGrayText  string // "\033[38;5;243m"
		LightGrayText string // "\033[38;5;250m"
		Reset         string // "\033[0m"
	}
	Paddings struct {
		Left         string // "  "
		Right        string // "  "
		MinGap       int    // 1
		PathLeft     string // "  "
		GitLeft      string // "  "
		StatsLeftPad int    // 4 for indent in stats only
	}
	Truncation struct {
		PathEllipsisLen int // 7 for "..."
		ModelMaxLen     int // 20
		GenericTruncLen int // 17 for fallback
		FreeModelTrunc  int // 12 for :free models
		DeepseekTrunc   int // 15 for non-free deepseek
	}
}

// FooterComponent displays status information at the bottom of the terminal
type FooterComponent struct {
	*console.BaseComponent
	config            FooterConfig
	lastModel         string
	lastProvider      string
	lastTokens        int
	lastCost          float64
	lastIteration     int
	lastContextTokens int
	maxContextTokens  int
	sessionStart      time.Time
	outputMutex       *sync.Mutex
	dynamicHeight     int

	// Token rate tracking
	lastTokenUpdateTime time.Time
	previousTokens      int
	tokensPerSecond     float64

	// Git and path information
	gitBranch   string
	gitChanges  int
	gitRemote   string
	currentPath string
	isGitRepo   bool
}

// NewFooterComponent creates a new footer component
func NewFooterComponent() *FooterComponent {
	config := FooterConfig{
		NarrowWidthThreshold: 100,
		DefaultHeight:        4,
		NarrowHeight:         5,
		ZOrder:               100,
		Colors: struct {
			BgBlueGrey    string
			TextWhite     string
			LightGrayBg   string
			BlackText     string
			DimWhite      string
			DarkGrayBg    string
			DarkGrayText  string
			LightGrayText string
			Reset         string
		}{
			BgBlueGrey:    "\033[48;2;50;54;62m",
			TextWhite:     "\033[37m",
			LightGrayBg:   "\033[47m",
			BlackText:     "\033[30m",
			DimWhite:      "\033[2m\033[37m",
			DarkGrayBg:    "\033[40m",
			DarkGrayText:  "\033[38;5;243m",
			LightGrayText: "\033[38;5;250m",
			Reset:         "\033[0m",
		},
		Paddings: struct {
			Left         string
			Right        string
			MinGap       int
			PathLeft     string
			GitLeft      string
			StatsLeftPad int
		}{
			Left:         "  ",
			Right:        "  ",
			MinGap:       1,
			PathLeft:     "  ",
			GitLeft:      "  ",
			StatsLeftPad: 4,
		},
		Truncation: struct {
			PathEllipsisLen int
			ModelMaxLen     int
			GenericTruncLen int
			FreeModelTrunc  int
			DeepseekTrunc   int
		}{
			PathEllipsisLen: 7,
			ModelMaxLen:     20,
			GenericTruncLen: 17,
			FreeModelTrunc:  12,
			DeepseekTrunc:   15,
		},
	}

	return &FooterComponent{
		BaseComponent: console.NewBaseComponent("footer", "FooterComponent"),
		config:        config,
		sessionStart:  time.Now(),
		dynamicHeight: config.DefaultHeight, // Default
	}
}

// subscribeToField is a helper to simplify state subscriptions
func (fc *FooterComponent) subscribeToField(deps console.Dependencies, key string, setter func(interface{})) {
	deps.State.Subscribe(key, func(_ string, _, newVal interface{}) {
		if setter != nil {
			setter(newVal)
		}
		fc.SetNeedsRedraw(true)
	})
}

// calculateHeight determines the footer height based on terminal width
func (fc *FooterComponent) calculateHeight(width int) int {
	if width < fc.config.NarrowWidthThreshold {
		return fc.config.NarrowHeight // Split model and stats on narrow screens
	}
	return fc.config.DefaultHeight // Standard height
}

// Init initializes the footer component
func (fc *FooterComponent) Init(ctx context.Context, deps console.Dependencies) error {
	if err := fc.BaseComponent.Init(ctx, deps); err != nil {
		return err
	}

	width, height, _ := deps.Terminal.GetSize()
	fc.dynamicHeight = fc.calculateHeight(width)

	// Define footer region at bottom of terminal
	region := console.Region{
		X:       0,
		Y:       height - fc.dynamicHeight,
		Width:   width,
		Height:  fc.dynamicHeight,
		ZOrder:  fc.config.ZOrder, // High z-order to stay on top
		Visible: true,
	}

	if err := deps.Layout.DefineRegion("footer", region); err != nil {
		return err
	}
	fc.SetRegion("footer")

	// Subscribe to state changes using helper
	fc.subscribeToField(deps, "footer.model", func(v interface{}) {
		if model, ok := v.(string); ok {
			fc.lastModel = model
		}
	})
	fc.subscribeToField(deps, "footer.provider", func(v interface{}) {
		if provider, ok := v.(string); ok {
			fc.lastProvider = provider
		}
	})
	fc.subscribeToField(deps, "footer.tokens", func(v interface{}) {
		if tokens, ok := v.(int); ok {
			fc.lastTokens = tokens
		}
	})
	fc.subscribeToField(deps, "footer.cost", func(v interface{}) {
		if cost, ok := v.(float64); ok {
			fc.lastCost = cost
		}
	})
	fc.subscribeToField(deps, "footer.iteration", func(v interface{}) {
		if iteration, ok := v.(int); ok {
			fc.lastIteration = iteration
		}
	})
	fc.subscribeToField(deps, "footer.contextTokens", func(v interface{}) {
		if tokens, ok := v.(int); ok {
			fc.lastContextTokens = tokens
		}
	})
	fc.subscribeToField(deps, "footer.maxContextTokens", func(v interface{}) {
		if tokens, ok := v.(int); ok {
			fc.maxContextTokens = tokens
		}
	})

	// Subscribe to terminal resize events
	deps.Events.Subscribe("terminal.resized", func(event console.Event) error {
		if data, ok := event.Data.(map[string]int); ok {
			width := data["width"]
			height := data["height"]
			fc.updateRegionOnResize(width, height)
		}
		return nil
	})

	return nil
}

// updateRegionOnResize updates the footer region on resize
func (fc *FooterComponent) updateRegionOnResize(width, height int) {
	fc.dynamicHeight = fc.calculateHeight(width)

	region := console.Region{
		X:       0,
		Y:       height - fc.dynamicHeight,
		Width:   width,
		Height:  fc.dynamicHeight,
		ZOrder:  fc.config.ZOrder,
		Visible: true,
	}

	fc.Layout().UpdateRegion("footer", region)
	fc.SetNeedsRedraw(true)
}

// fillLineBackground ensures the entire line has the footer background color
func (fc *FooterComponent) fillLineBackground(currentLen int, totalWidth int) {
	if currentLen < totalWidth {
		// Fill the rest of the line with background color
		padding := strings.Repeat(" ", totalWidth-currentLen)
		fc.Terminal().Write([]byte(padding))
	}
}

// renderSeparator renders the blank separator line
func (fc *FooterComponent) renderSeparator(region console.Region, lineOffset int) error {
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)
	fc.Terminal().ClearLine()
	fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
	fc.Terminal().Write([]byte(strings.Repeat(" ", region.Width)))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))
	return nil
}

// renderPathLine renders the current path line
func (fc *FooterComponent) renderPathLine(region console.Region, lineOffset int) error {
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)
	fc.Terminal().ClearLine()

	// Set background color for the entire path line
	fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey + fc.config.Colors.TextWhite))

	// Replace home directory with ~
	displayPath := fc.currentPath
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(displayPath, home) {
		displayPath = "~" + strings.TrimPrefix(displayPath, home)
	}

	// Truncate path if too long
	availablePathLen := region.Width - len(fc.config.Paddings.PathLeft) - 4 // 4 for safety
	if len(displayPath) > availablePathLen {
		ellipsisLen := fc.config.Truncation.PathEllipsisLen
		startIdx := len(displayPath) - availablePathLen + ellipsisLen
		if startIdx > 0 {
			displayPath = "..." + displayPath[startIdx:]
		}
	}

	pathLine := fmt.Sprintf("%s%s", fc.config.Paddings.PathLeft, displayPath)
	fc.Terminal().Write([]byte(pathLine))

	// TODO: TPS-related padding temporarily removed
	// Calculate padding between path and end of line
	// paddingLen := region.Width - len(pathLine) - tpsLen
	// if paddingLen > 0 {
	//	fc.Terminal().Write([]byte(strings.Repeat(" ", paddingLen)))
	// }

	// TODO: TPS display temporarily disabled due to incorrect data
	// Write TPS if applicable, with dim white for subtlety
	// if tpsStr != "" {
	//	fc.Terminal().Write([]byte(fc.config.Colors.DimWhite))
	//	fc.Terminal().Write([]byte(tpsStr))
	//	fc.Terminal().Write([]byte(fc.config.Colors.Reset))
	// } else {
	// Pad the rest without TPS
	remainingPad := region.Width - len(pathLine)
	if remainingPad > 0 {
		fc.Terminal().Write([]byte(strings.Repeat(" ", remainingPad)))
	}
	// }

	fc.Terminal().Write([]byte(fc.config.Colors.Reset))
	return nil
}

// renderGitLine renders the git information line
func (fc *FooterComponent) renderGitLine(region console.Region, lineOffset int) error {
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)
	fc.Terminal().ClearLine()

	if fc.isGitRepo && fc.gitBranch != "" {
		fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))

		// Determine if we should show remote based on screen width
		showRemote := fc.gitRemote != "" && region.Width >= fc.config.NarrowWidthThreshold

		// Format git line: remote in darker text, branch in lighter
		gitLine := fc.config.Paddings.GitLeft
		if showRemote {
			gitLine += fmt.Sprintf("%s%s%s%s", fc.config.Colors.DarkGrayText, fc.gitRemote, fc.config.Colors.Reset, fc.config.Colors.BgBlueGrey)
			gitLine += ":"
		}
		gitLine += fmt.Sprintf("%s%s%s%s", fc.config.Colors.LightGrayText, fc.gitBranch, fc.config.Colors.Reset, fc.config.Colors.BgBlueGrey)

		// Changes in default white
		if fc.gitChanges > 0 {
			gitLine += fmt.Sprintf("%s (+%d)%s%s", fc.config.Colors.TextWhite, fc.gitChanges, fc.config.Colors.Reset, fc.config.Colors.BgBlueGrey)
		}

		fc.Terminal().Write([]byte(gitLine))
		// Pad the rest of the line - need to calculate visible length
		visibleLen := len(fc.config.Paddings.GitLeft)
		if showRemote {
			visibleLen += len(fc.gitRemote) + 1 // remote + colon
		}
		visibleLen += len(fc.gitBranch)
		if fc.gitChanges > 0 {
			visibleLen += len(fmt.Sprintf(" (+%d)", fc.gitChanges))
		}
		padding := region.Width - visibleLen
		if padding > 0 {
			fc.Terminal().Write([]byte(strings.Repeat(" ", padding)))
		}
		fc.Terminal().Write([]byte(fc.config.Colors.Reset))
	} else {
		// No git repo - fill with blue-grey
		fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
		fc.Terminal().Write([]byte(strings.Repeat(" ", region.Width)))
		fc.Terminal().Write([]byte(fc.config.Colors.Reset))
	}
	return nil
}

// formatCost formats the cost with appropriate precision
func (fc *FooterComponent) formatCost(cost float64) string {
	if cost >= 1.0 {
		return fmt.Sprintf("$%.2f", cost)
	} else if cost >= 0.01 {
		return fmt.Sprintf("$%.3f", cost)
	} else if cost > 0 {
		return fmt.Sprintf("$%.6f", cost)
	}
	return "$0.000"
}

// formatContextUsage formats the context usage string
func (fc *FooterComponent) formatContextUsage() string {
	if fc.maxContextTokens > 0 {
		percent := float64(fc.lastContextTokens) / float64(fc.maxContextTokens) * 100
		return fmt.Sprintf(" | %s/%s (%.0f%%)",
			fc.formatTokens(fc.lastContextTokens),
			fc.formatTokens(fc.maxContextTokens),
			percent)
	}
	return ""
}

// formatIteration formats the iteration string
func (fc *FooterComponent) formatIteration() string {
	if fc.lastIteration > 0 {
		return fmt.Sprintf(" | %d", fc.lastIteration)
	}
	return ""
}

// getStatsSection returns the formatted stats string, choosing full/minimal based on width
func (fc *FooterComponent) getStatsSection(region console.Region) (string, int) {
	costStr := fc.formatCost(fc.lastCost)
	contextStr := fc.formatContextUsage()
	iterStr := fc.formatIteration()

	rightPad := fc.config.Paddings.Right
	fullStatsContent := fmt.Sprintf(
		"%s | %s%s%s",
		fc.formatTokens(fc.lastTokens),
		costStr,
		contextStr,
		iterStr,
	)
	fullSection := fullStatsContent + rightPad
	fullLen := len(fullSection)

	minContent := fmt.Sprintf("%s | %s", fc.formatTokens(fc.lastTokens), costStr)
	minSection := minContent + rightPad
	minLen := len(minSection)

	absMin := costStr + "  "
	absMinLen := len(absMin)

	if minLen <= region.Width {
		if fullLen > region.Width {
			return minSection, minLen
		}
		return fullSection, fullLen
	}
	return absMin, absMinLen
}

// renderModelAndStats renders model and stats on a single line
func (fc *FooterComponent) renderModelAndStats(region console.Region, lineOffset int) error {
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)
	fc.Terminal().ClearLine()

	// Define edge padding widths
	leftEdgePadding := len(fc.config.Paddings.Left)   // Usually 2 spaces
	rightEdgePadding := len(fc.config.Paddings.Right) // Usually 2 spaces

	// Get stats content
	statsSection, statsLen := fc.getStatsSection(region)

	// Calculate available space for model section
	availableForModel := region.Width - leftEdgePadding - rightEdgePadding - statsLen - fc.config.Paddings.MinGap

	// Build model section
	modelName := fc.extractModelName(fc.lastModel)
	baseModelText := fmt.Sprintf("%s (%s)", fc.lastProvider, modelName)

	// Adjust model content to fit available space
	modelContent := baseModelText
	if len(modelContent) > availableForModel {
		// Truncate model section to fit
		if availableForModel > len(fc.lastProvider)+5 {
			availableForModelName := availableForModel - len(fc.lastProvider) - 3 // For " ()"
			if availableForModelName > 3 {
				truncModel := fc.truncateString(modelName, availableForModelName)
				modelContent = fmt.Sprintf("%s (%s)", fc.lastProvider, truncModel)
			} else {
				modelContent = fc.lastProvider
			}
		} else {
			modelContent = fc.truncateString(fc.lastProvider, availableForModel)
		}
	}

	// Render the line with three sections: blue-grey | light-gray model | dark-gray stats | blue-grey
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)

	// 1. Left blue-grey padding
	fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
	fc.Terminal().Write([]byte(strings.Repeat(" ", leftEdgePadding)))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	// 2. Model section with light gray background (only behind text)
	fc.Terminal().Write([]byte(fc.config.Colors.LightGrayBg + fc.config.Colors.BlackText))
	fc.Terminal().Write([]byte(modelContent))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	// 2.5. Padding after model with blue-grey background
	modelPadding := availableForModel - len(modelContent)
	if modelPadding > 0 {
		fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
		fc.Terminal().Write([]byte(strings.Repeat(" ", modelPadding)))
		fc.Terminal().Write([]byte(fc.config.Colors.Reset))
	}

	// 3. Stats section with dark gray background
	fc.Terminal().Write([]byte(fc.config.Colors.DarkGrayBg + fc.config.Colors.DimWhite))
	fc.Terminal().Write([]byte(statsSection))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	// 4. Right blue-grey padding
	fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
	fc.Terminal().Write([]byte(strings.Repeat(" ", rightEdgePadding)))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	return nil
}

// renderModelOnly renders just the model info on a line
func (fc *FooterComponent) renderModelOnly(region console.Region, lineOffset int) error {
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)
	fc.Terminal().ClearLine()

	// Define edge padding widths
	leftEdgePadding := len(fc.config.Paddings.Left)   // Usually 2 spaces
	rightEdgePadding := len(fc.config.Paddings.Right) // Usually 2 spaces

	// Calculate available space for model content
	availableForModel := region.Width - leftEdgePadding - rightEdgePadding

	// Build model content
	modelName := fc.extractModelName(fc.lastModel)
	modelContent := fmt.Sprintf("%s (%s)", fc.lastProvider, modelName)

	// Truncate if necessary
	if len(modelContent) > availableForModel {
		if availableForModel > len(fc.lastProvider)+5 {
			availableForModelName := availableForModel - len(fc.lastProvider) - 3 // For " ()"
			if availableForModelName > 3 {
				truncModel := fc.truncateString(modelName, availableForModelName)
				modelContent = fmt.Sprintf("%s (%s)", fc.lastProvider, truncModel)
			} else {
				modelContent = fc.lastProvider
			}
		} else {
			modelContent = fc.truncateString(fc.lastProvider, availableForModel)
		}
	}

	// Render the line: blue-grey | light-gray model | blue-grey
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)

	// 1. Left blue-grey padding
	fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
	fc.Terminal().Write([]byte(strings.Repeat(" ", leftEdgePadding)))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	// 2. Model section with light gray background (only behind text)
	fc.Terminal().Write([]byte(fc.config.Colors.LightGrayBg + fc.config.Colors.BlackText))
	fc.Terminal().Write([]byte(modelContent))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	// 2.5. Padding after model with blue-grey background
	modelPadding := availableForModel - len(modelContent)
	if modelPadding > 0 {
		fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
		fc.Terminal().Write([]byte(strings.Repeat(" ", modelPadding)))
		fc.Terminal().Write([]byte(fc.config.Colors.Reset))
	}

	// 3. Right blue-grey padding
	fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
	fc.Terminal().Write([]byte(strings.Repeat(" ", rightEdgePadding)))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	return nil
}

// renderStatsOnly renders just the stats on a line
func (fc *FooterComponent) renderStatsOnly(region console.Region, lineOffset int) error {
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)
	fc.Terminal().ClearLine()

	// Define edge padding widths
	leftEdgePadding := len(fc.config.Paddings.Left)   // Usually 2 spaces
	rightEdgePadding := len(fc.config.Paddings.Right) // Usually 2 spaces

	// Calculate available space for stats content (with slight indent)
	statsIndent := fc.config.Paddings.StatsLeftPad // Usually 4 spaces for indent
	availableForStats := region.Width - leftEdgePadding - rightEdgePadding - statsIndent

	// Build stats content
	costStr := fc.formatCost(fc.lastCost)
	contextStr := fc.formatContextUsage()
	iterStr := fc.formatIteration()

	statsContent := fmt.Sprintf(
		"%s | %s%s%s",
		fc.formatTokens(fc.lastTokens),
		costStr,
		contextStr,
		iterStr,
	)

	// Truncate stats if necessary (fallback to minimal)
	if len(statsContent) > availableForStats {
		minContent := fmt.Sprintf("%s | %s", fc.formatTokens(fc.lastTokens), costStr)
		if len(minContent) <= availableForStats {
			statsContent = minContent
		} else {
			statsContent = costStr // Absolute minimum
		}
	}

	// Render the line: blue-grey | dark-gray stats | blue-grey
	fc.Terminal().MoveCursor(region.X+1, region.Y+lineOffset)

	// 1. Left blue-grey padding
	fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
	fc.Terminal().Write([]byte(strings.Repeat(" ", leftEdgePadding)))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	// 2. Stats section with dark gray background (includes indent)
	fc.Terminal().Write([]byte(fc.config.Colors.DarkGrayBg + fc.config.Colors.DimWhite))
	fc.Terminal().Write([]byte(strings.Repeat(" ", statsIndent))) // Indent
	fc.Terminal().Write([]byte(statsContent))
	// Pad stats section to full available width
	statsPadding := availableForStats - len(statsContent)
	if statsPadding > 0 {
		fc.Terminal().Write([]byte(strings.Repeat(" ", statsPadding)))
	}
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	// 3. Right blue-grey padding
	fc.Terminal().Write([]byte(fc.config.Colors.BgBlueGrey))
	fc.Terminal().Write([]byte(strings.Repeat(" ", rightEdgePadding)))
	fc.Terminal().Write([]byte(fc.config.Colors.Reset))

	return nil
}

// Render renders the footer
func (fc *FooterComponent) Render() error {
	if console.DebugEnabled() {
		fmt.Fprintf(os.Stderr, "[DEBUG] FooterComponent.Render() called\n")
	}

	// Use output mutex to prevent interleaving with agent output
	if fc.outputMutex != nil {
		fc.outputMutex.Lock()
		defer fc.outputMutex.Unlock()
	}

	// Ensure we start with a clean slate - reset any existing styles
	fc.Terminal().Write([]byte("\033[0m"))

	region, err := fc.Layout().GetRegion("footer")
	if err != nil {
		return fmt.Errorf("failed to get footer region: %w", err)
	}

	// Save cursor position
	fc.Terminal().SaveCursor()
	defer fc.Terminal().RestoreCursor()

	// Clear all footer lines first
	for i := 0; i < fc.dynamicHeight; i++ {
		fc.Terminal().MoveCursor(region.X+1, region.Y+i+1)
		fc.Terminal().ClearLine()
	}

	// Render each line
	if err := fc.renderSeparator(region, 1); err != nil {
		return err
	}
	if err := fc.renderPathLine(region, 2); err != nil {
		return err
	}
	if err := fc.renderGitLine(region, 3); err != nil {
		return err
	}

	// Model and stats rendering
	isNarrow := fc.dynamicHeight == fc.config.NarrowHeight

	if !isNarrow {
		// Standard: Model and stats on fourth line
		if err := fc.renderModelAndStats(region, 4); err != nil {
			return err
		}
	} else {
		// Narrow: Model on fourth line, stats on fifth line
		if err := fc.renderModelOnly(region, 4); err != nil {
			return err
		}
		if err := fc.renderStatsOnly(region, 5); err != nil {
			return err
		}
	}

	// Mark as drawn
	fc.SetNeedsRedraw(false)

	// CRITICAL: Reset all styling to prevent bleed-through to subsequent output
	// Use explicit SGR0 reset to ensure ALL attributes are cleared
	fc.Terminal().Write([]byte("\033[0m"))

	// Also explicitly set default foreground/background to be absolutely sure
	fc.Terminal().Write([]byte("\033[39;49m"))

	return nil
}

// UpdateStats updates the footer statistics
func (fc *FooterComponent) UpdateStats(model, provider string, tokens int, cost float64, iteration, contextTokens, maxContextTokens int) {
	if tokens < 0 || contextTokens < 0 || maxContextTokens < 0 {
		// Log or handle invalid values if needed
		return
	}

	// Calculate tokens per second for real-time display
	now := time.Now()
	if !fc.lastTokenUpdateTime.IsZero() && tokens > fc.previousTokens {
		deltaTokens := tokens - fc.previousTokens
		deltaTime := now.Sub(fc.lastTokenUpdateTime).Seconds()
		if deltaTime > 0.1 { // Only update if at least 100ms have passed
			fc.tokensPerSecond = float64(deltaTokens) / deltaTime
		}
	}
	fc.lastTokenUpdateTime = now
	fc.previousTokens = tokens

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

// GetHeight returns the current dynamic height
func (fc *FooterComponent) GetHeight() int {
	return fc.dynamicHeight
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
		if len(modelName) > fc.config.Truncation.ModelMaxLen {
			// First check if it's a free model - preserve ":free" suffix
			if strings.HasSuffix(modelName, ":free") {
				// Remove ":free" temporarily for processing
				baseName := strings.TrimSuffix(modelName, ":free")

				// Apply pattern-specific truncation to base name
				var truncatedBase string
				if strings.Contains(baseName, "Qwen") && strings.Contains(baseName, "Coder") {
					// Extract: Qwen3-Coder-480B from Qwen3-Coder-480B-A35B-Instruct-Turbo
					if p := strings.Split(baseName, "-"); len(p) >= 3 {
						truncatedBase = strings.Join(p[:3], "-")
					} else {
						truncatedBase = baseName
					}
				} else if strings.Contains(baseName, "deepseek") {
					// Keep deepseek base name, truncate if too long
					if len(baseName) > fc.config.Truncation.FreeModelTrunc {
						truncatedBase = baseName[:fc.config.Truncation.FreeModelTrunc] + "..."
					} else {
						truncatedBase = baseName
					}
				} else {
					// Generic truncation for other free models
					if len(baseName) > fc.config.Truncation.FreeModelTrunc {
						truncatedBase = baseName[:fc.config.Truncation.FreeModelTrunc] + "..."
					} else {
						truncatedBase = baseName
					}
				}

				return truncatedBase + ":free"
			}

			// Non-free model patterns
			if strings.Contains(modelName, "Qwen") && strings.Contains(modelName, "Coder") {
				// Extract: Qwen3-Coder-480B
				if p := strings.Split(modelName, "-"); len(p) >= 3 {
					return strings.Join(p[:3], "-") // e.g., "Qwen3-Coder-480B"
				}
			} else if strings.Contains(modelName, "deepseek") {
				// For non-free deepseek models, truncate base name if needed
				if p := strings.Split(modelName, ":"); len(p) > 1 {
					base := p[0]
					if len(base) > fc.config.Truncation.DeepseekTrunc {
						return base[:fc.config.Truncation.DeepseekTrunc] + "..."
					}
					return base
				}
			}

			// Generic fallback: keep first N chars
			return modelName[:fc.config.Truncation.GenericTruncLen] + "..."
		}
		return modelName
	}
	// Fallback to truncating the full string if no "/" found
	return fc.truncateString(fullModel, fc.config.Truncation.ModelMaxLen)
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
	oldRegion, err := fc.Layout().GetRegion("footer")
	if err != nil {
		return // Silently fail if region not found
	}

	// Save cursor position
	fc.Terminal().SaveCursor()

	// Clear all old footer lines with bounds check
	if oldRegion.Y > 0 && oldRegion.Height > 0 {
		for i := 0; i < oldRegion.Height; i++ {
			if oldRegion.Y+i+1 <= height {
				fc.Terminal().MoveCursor(1, oldRegion.Y+i+1)
				fc.Terminal().ClearLine()
			}
		}
	}

	// Update region
	fc.updateRegionOnResize(width, height)

	// Restore cursor
	fc.Terminal().RestoreCursor()

	// Add a small delay to let terminal stabilize after resize
	time.Sleep(10 * time.Millisecond)

	// Force a fresh render
	fc.Render()
}
