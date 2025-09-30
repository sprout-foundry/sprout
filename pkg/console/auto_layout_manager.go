package console

import (
	"fmt"
	"sort"
	"sync"

	"golang.org/x/term"
)

// ComponentInfo holds layout information for a component
type ComponentInfo struct {
	Name     string
	Position string // "top", "bottom", "content"
	Height   int    // Fixed height, 0 = flexible
	Priority int    // Higher priority gets preference
	Visible  bool
	ZOrder   int
}

// AutoLayoutManager automatically manages component positioning
type AutoLayoutManager struct {
	LayoutManager // Embed the existing interface

	mutex       sync.RWMutex
	termWidth   int
	termHeight  int
	terminalFd  int
	components  map[string]*ComponentInfo
	initialized bool

	// Callbacks for resize events
	resizeCallbacks []func(width, height int)
}

// NewAutoLayoutManager creates an enhanced layout manager
func NewAutoLayoutManager() *AutoLayoutManager {
	// Provide sensible defaults for non-interactive tests
	baseManager := NewLayoutManager(80, 24)

	return &AutoLayoutManager{
		LayoutManager:   baseManager,
		terminalFd:      0, // stdin
		termWidth:       80,
		termHeight:      24,
		components:      make(map[string]*ComponentInfo),
		resizeCallbacks: make([]func(width, height int), 0),
	}
}

// Initialize sets up the auto layout manager
func (alm *AutoLayoutManager) Initialize() error {
	alm.mutex.Lock()
	defer alm.mutex.Unlock()

	var width, height int
	var err error
	if term.IsTerminal(alm.terminalFd) {
		width, height, err = term.GetSize(alm.terminalFd)
		if err != nil {
			return fmt.Errorf("failed to get terminal size: %w", err)
		}
	} else {
		// Headless fallback for tests and non-interactive environments
		width, height = 80, 24
	}

	alm.termWidth = width
	alm.termHeight = height
	alm.initialized = true

	// Initialize the base layout manager
	if err := alm.LayoutManager.CalculateLayout(width, height); err != nil {
		return err
	}

	// Calculate initial auto layout
	alm.calculateAutoLayout()

	return nil
}

// InitializeForTest sets up the auto layout manager without requiring a terminal
func (alm *AutoLayoutManager) InitializeForTest(width, height int) {
	alm.mutex.Lock()
	defer alm.mutex.Unlock()

	alm.termWidth = width
	alm.termHeight = height
	alm.initialized = true

	// Initialize the base layout manager
	alm.LayoutManager.CalculateLayout(width, height)

	// Calculate initial auto layout
	alm.calculateAutoLayout()
}

// RegisterComponent registers a component for automatic layout
func (alm *AutoLayoutManager) RegisterComponent(name string, info *ComponentInfo) {
	alm.mutex.Lock()
	defer alm.mutex.Unlock()

	alm.components[name] = info
	// Calculate layout even before initialization, using default/test dimensions
	alm.calculateAutoLayout()
}

// UpdateComponentInfo updates component layout information
func (alm *AutoLayoutManager) UpdateComponentInfo(name string, updater func(*ComponentInfo)) {
	alm.mutex.Lock()
	defer alm.mutex.Unlock()

	if info, exists := alm.components[name]; exists {
		updater(info)
		if alm.initialized {
			alm.calculateAutoLayout()
		}
	}
}

// SetComponentHeight sets the height for a component
func (alm *AutoLayoutManager) SetComponentHeight(name string, height int) {
	alm.UpdateComponentInfo(name, func(info *ComponentInfo) {
		info.Height = height
	})
}

// SetComponentVisible sets visibility for a component
func (alm *AutoLayoutManager) SetComponentVisible(name string, visible bool) {
	alm.UpdateComponentInfo(name, func(info *ComponentInfo) {
		info.Visible = visible
	})
}

// GetContentRegion returns the region allocated for scrollable content
func (alm *AutoLayoutManager) GetContentRegion() (Region, error) {
	alm.mutex.RLock()
	defer alm.mutex.RUnlock()

	return alm.LayoutManager.GetRegion("content")
}

// GetScrollRegion returns the scroll region boundaries for terminal setup (1-based)
func (alm *AutoLayoutManager) GetScrollRegion() (top, bottom int) {
	contentRegion, err := alm.GetContentRegion()
	if err != nil {
		// Fallback: use full terminal height if content region is not available
		return 1, alm.termHeight
	}

	// Convert 0-based regions to 1-based terminal coordinates
	top = contentRegion.Y + 1
	bottom = contentRegion.Y + contentRegion.Height

	return top, bottom
}

// OnTerminalResize handles terminal resize events
func (alm *AutoLayoutManager) OnTerminalResize(width, height int) {
	alm.mutex.Lock()
	defer alm.mutex.Unlock()

	alm.termWidth = width
	alm.termHeight = height

	// Update base layout manager
	alm.LayoutManager.CalculateLayout(width, height)

	// Recalculate auto layout
	alm.calculateAutoLayout()

	// Notify registered callbacks
	for _, callback := range alm.resizeCallbacks {
		go callback(width, height)
	}
}

// AddResizeCallback adds a callback for resize events
func (alm *AutoLayoutManager) AddResizeCallback(callback func(width, height int)) {
	alm.mutex.Lock()
	defer alm.mutex.Unlock()
	alm.resizeCallbacks = append(alm.resizeCallbacks, callback)
}

// GetTerminalSize returns current terminal dimensions
func (alm *AutoLayoutManager) GetTerminalSize() (int, int) {
	alm.mutex.RLock()
	defer alm.mutex.RUnlock()
	return alm.termWidth, alm.termHeight
}

// calculateAutoLayout computes automatic layout for all components
func (alm *AutoLayoutManager) calculateAutoLayout() {
	if alm.termWidth <= 0 || alm.termHeight <= 0 {
		return
	}

	// Separate components by position
	topComponents := alm.getComponentsByPosition("top")
	bottomComponents := alm.getComponentsByPosition("bottom")

	// Sort top components by priority (higher = closer to top)
	sort.Slice(topComponents, func(i, j int) bool {
		return topComponents[i].Priority > topComponents[j].Priority
	})

	// Sort bottom components by priority (higher = closer to content)
	// We want higher priority components ABOVE lower priority ones
	sort.Slice(bottomComponents, func(i, j int) bool {
		return bottomComponents[i].Priority > bottomComponents[j].Priority
	})

	// Allocate top components (growing downward from 0)
	currentY := 0
	for _, info := range topComponents {
		region := Region{
			X:       0,
			Y:       currentY,
			Width:   alm.termWidth,
			Height:  info.Height,
			ZOrder:  info.ZOrder,
			Visible: info.Visible,
		}
		if err := alm.LayoutManager.DefineRegion(info.Name, region); err != nil {
			fmt.Printf("Error defining top region %s: %v\n", info.Name, err)
		}
		currentY += info.Height
	}

	// Calculate total height needed for bottom components
	totalBottomHeight := 0
	for _, info := range bottomComponents {
		totalBottomHeight += info.Height
	}

	// Allocate content region (what's left in the middle)
	contentY := currentY
	contentHeight := alm.termHeight - currentY - totalBottomHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	contentRegion := Region{
		X:       0,
		Y:       contentY,
		Width:   alm.termWidth,
		Height:  contentHeight,
		ZOrder:  0, // Content is background
		Visible: true,
	}
	if err := alm.LayoutManager.DefineRegion("content", contentRegion); err != nil {
		fmt.Printf("Error defining content region: %v\n", err)
	}

	// Allocate bottom components (from bottom of content area downward)
	// Higher priority (closer to content) goes first
	bottomY := contentY + contentHeight
	for _, info := range bottomComponents {
		region := Region{
			X:       0,
			Y:       bottomY,
			Width:   alm.termWidth,
			Height:  info.Height,
			ZOrder:  info.ZOrder,
			Visible: info.Visible,
		}
		if err := alm.LayoutManager.DefineRegion(info.Name, region); err != nil {
			fmt.Printf("Error defining bottom region %s: %v\n", info.Name, err)
		}
		bottomY += info.Height
	}
}

// getComponentsByPosition returns components with the specified position
func (alm *AutoLayoutManager) getComponentsByPosition(position string) []*ComponentInfo {
	components := make([]*ComponentInfo, 0)
	for _, info := range alm.components {
		if info.Visible && info.Position == position {
			components = append(components, info)
		}
	}
	return components
}

// PrintDebugLayout prints current layout for debugging
func (alm *AutoLayoutManager) PrintDebugLayout() {
	alm.mutex.RLock()
	defer alm.mutex.RUnlock()

	fmt.Printf("=== Auto Layout Debug ===\n")
	fmt.Printf("Terminal: %dx%d\n", alm.termWidth, alm.termHeight)
	fmt.Printf("\nComponents:\n")
	for name, info := range alm.components {
		visible := "hidden"
		if info.Visible {
			visible = "visible"
		}
		fmt.Printf("  %s: pos=%s, height=%d, priority=%d, z=%d (%s)\n",
			name, info.Position, info.Height, info.Priority, info.ZOrder, visible)
	}

	// Show manual layout calculation
	fmt.Printf("\nManual Layout Calculation:\n")
	topComponents := alm.getComponentsByPosition("top")
	bottomComponents := alm.getComponentsByPosition("bottom")

	sort.Slice(bottomComponents, func(i, j int) bool {
		return bottomComponents[i].Priority > bottomComponents[j].Priority
	})

	// Top components
	currentY := 0
	fmt.Printf("  Top components start at y=0:\n")
	for _, info := range topComponents {
		fmt.Printf("    %s: y=%d, height=%d\n", info.Name, currentY, info.Height)
		currentY += info.Height
	}

	// Bottom components
	totalBottomHeight := 0
	for _, info := range bottomComponents {
		totalBottomHeight += info.Height
	}
	fmt.Printf("  Total bottom height: %d\n", totalBottomHeight)

	// Content
	contentY := currentY
	contentHeight := alm.termHeight - currentY - totalBottomHeight
	fmt.Printf("  Content: y=%d, height=%d\n", contentY, contentHeight)

	// Bottom components (from content area down)
	bottomY := contentY + contentHeight
	fmt.Printf("  Bottom components (priority order, higher=closer to content):\n")
	for _, info := range bottomComponents {
		fmt.Printf("    %s: y=%d, height=%d, priority=%d\n", info.Name, bottomY, info.Height, info.Priority)
		bottomY += info.Height
	}

	fmt.Printf("\nActual Regions:\n")
	regionNames := alm.LayoutManager.ListRegions()
	if len(regionNames) == 0 {
		fmt.Printf("  (no regions defined - this is the problem!)\n")
	}
	for _, name := range regionNames {
		if region, err := alm.LayoutManager.GetRegion(name); err == nil {
			fmt.Printf("  %s: y=%d-%d (height=%d), x=%d, w=%d, z=%d, visible=%t\n",
				name, region.Y, region.Y+region.Height-1, region.Height,
				region.X, region.Width, region.ZOrder, region.Visible)
		}
	}

	top, bottom := alm.GetScrollRegion()
	fmt.Printf("\nScroll region: lines %d-%d\n", top, bottom)
	fmt.Printf("=========================\n")
}

// TestLayout manually sets up layout for testing without requiring terminal
func (alm *AutoLayoutManager) TestLayout(width, height int) {
	alm.mutex.Lock()
	defer alm.mutex.Unlock()

	alm.termWidth = width
	alm.termHeight = height
	alm.calculateAutoLayout()
}
