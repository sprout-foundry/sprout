package console

import (
	"fmt"
	"sort"
	"sync"
)

// layoutManager implements LayoutManager interface
type layoutManager struct {
	mu              sync.RWMutex
	regions         map[string]*Region
	termWidth       int
	termHeight      int
	redrawQueue     map[string]bool
	batchMode       bool
	forceRedrawFlag bool
}

// NewLayoutManager creates a new layout manager
func NewLayoutManager(termWidth, termHeight int) LayoutManager {
	return &layoutManager{
		regions:     make(map[string]*Region),
		redrawQueue: make(map[string]bool),
		termWidth:   termWidth,
		termHeight:  termHeight,
	}
}

// DefineRegion defines a new region
func (lm *layoutManager) DefineRegion(name string, region Region) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	// Validate region
	if err := lm.validateRegion(&region); err != nil {
		return fmt.Errorf("invalid region %s: %w", name, err)
	}

	// Store copy to prevent external modification
	regionCopy := region
	lm.regions[name] = &regionCopy

	// Mark for redraw
	lm.redrawQueue[name] = true

	return nil
}

// UpdateRegion updates an existing region
func (lm *layoutManager) UpdateRegion(name string, region Region) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if _, exists := lm.regions[name]; !exists {
		return fmt.Errorf("region %s not found", name)
	}

	// Validate region
	if err := lm.validateRegion(&region); err != nil {
		return fmt.Errorf("invalid region %s: %w", name, err)
	}

	// Update with copy
	regionCopy := region
	lm.regions[name] = &regionCopy

	// Mark for redraw
	lm.redrawQueue[name] = true

	return nil
}

// GetRegion returns a copy of the region
func (lm *layoutManager) GetRegion(name string) (Region, error) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	region, exists := lm.regions[name]
	if !exists {
		return Region{}, fmt.Errorf("region %s not found", name)
	}

	// Return copy to prevent external modification
	return *region, nil
}

// RemoveRegion removes a region
func (lm *layoutManager) RemoveRegion(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if _, exists := lm.regions[name]; !exists {
		return fmt.Errorf("region %s not found", name)
	}

	delete(lm.regions, name)
	delete(lm.redrawQueue, name)

	return nil
}

// ListRegions returns all region names
func (lm *layoutManager) ListRegions() []string {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	names := make([]string, 0, len(lm.regions))
	for name := range lm.regions {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// CalculateLayout recalculates layout for new terminal size
func (lm *layoutManager) CalculateLayout(termWidth, termHeight int) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.termWidth = termWidth
	lm.termHeight = termHeight

	// Mark all regions for redraw
	for name := range lm.regions {
		lm.redrawQueue[name] = true
	}

	// Here you could implement automatic layout adjustments
	// For now, regions maintain their absolute positions

	return nil
}

// GetAvailableSpace returns the largest available rectangular space
func (lm *layoutManager) GetAvailableSpace() Region {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	// Simple implementation: return full terminal space
	// A more sophisticated version would calculate actual free space
	return Region{
		X:       0,
		Y:       0,
		Width:   lm.termWidth,
		Height:  lm.termHeight,
		Visible: true,
	}
}

// BeginBatch starts batch mode for rendering
func (lm *layoutManager) BeginBatch() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.batchMode = true
}

// EndBatch ends batch mode and returns regions needing redraw
func (lm *layoutManager) EndBatch() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.batchMode = false

	// In a real implementation, this would trigger actual rendering
	// For now, we just clear the queue
	if len(lm.redrawQueue) > 0 || lm.forceRedrawFlag {
		// Would trigger redraw here
		lm.redrawQueue = make(map[string]bool)
		lm.forceRedrawFlag = false
	}

	return nil
}

// RequestRedraw marks a region for redraw
func (lm *layoutManager) RequestRedraw(regionName string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if _, exists := lm.regions[regionName]; exists {
		lm.redrawQueue[regionName] = true

		// If not in batch mode, could trigger immediate redraw
		if !lm.batchMode {
			// Would trigger redraw here
		}
	}
}

// ForceRedraw marks all regions for redraw
func (lm *layoutManager) ForceRedraw() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.forceRedrawFlag = true
	for name := range lm.regions {
		lm.redrawQueue[name] = true
	}

	// If not in batch mode, could trigger immediate redraw
	if !lm.batchMode {
		// Would trigger redraw here
	}
}

// SetZOrder sets the z-order for a region
func (lm *layoutManager) SetZOrder(regionName string, zOrder int) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	region, exists := lm.regions[regionName]
	if !exists {
		return fmt.Errorf("region %s not found", regionName)
	}

	region.ZOrder = zOrder
	lm.redrawQueue[regionName] = true

	return nil
}

// GetRenderOrder returns region names sorted by z-order
func (lm *layoutManager) GetRenderOrder() []string {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	// Create slice of region info for sorting
	type regionInfo struct {
		name   string
		zOrder int
	}

	regions := make([]regionInfo, 0, len(lm.regions))
	for name, region := range lm.regions {
		if region.Visible {
			regions = append(regions, regionInfo{
				name:   name,
				zOrder: region.ZOrder,
			})
		}
	}

	// Sort by z-order (lower values rendered first)
	sort.Slice(regions, func(i, j int) bool {
		if regions[i].zOrder == regions[j].zOrder {
			// If same z-order, sort by name for consistency
			return regions[i].name < regions[j].name
		}
		return regions[i].zOrder < regions[j].zOrder
	})

	// Extract names
	names := make([]string, len(regions))
	for i, info := range regions {
		names[i] = info.name
	}

	return names
}

// validateRegion validates region boundaries
func (lm *layoutManager) validateRegion(region *Region) error {
	if region.X < 0 || region.Y < 0 {
		return fmt.Errorf("negative coordinates: x=%d, y=%d", region.X, region.Y)
	}

	if region.Width <= 0 || region.Height <= 0 {
		return fmt.Errorf("invalid dimensions: width=%d, height=%d", region.Width, region.Height)
	}

	// Check if region fits within terminal
	if region.X+region.Width > lm.termWidth {
		return fmt.Errorf("region exceeds terminal width: %d > %d", region.X+region.Width, lm.termWidth)
	}

	if region.Y+region.Height > lm.termHeight {
		return fmt.Errorf("region exceeds terminal height: %d > %d", region.Y+region.Height, lm.termHeight)
	}

	return nil
}
