package api

// ModelSelection represents a model selection system
// This is a stub implementation for backward compatibility
// The actual model selection logic has been moved to configuration-based system
type ModelSelection struct {
	config interface{}
}

// NewModelSelection creates a new ModelSelection instance
// This is a stub for backward compatibility - the actual model selection
// is now handled through the configuration system
func NewModelSelection(config interface{}) *ModelSelection {
	return &ModelSelection{config: config}
}
