package ui

import (
    "fmt"
    "strings"
)

// StringItem is a simple adapter for string selections
type StringItem struct {
	value string
}

func NewStringItem(value string) *StringItem {
	return &StringItem{value: value}
}

func (s *StringItem) Display() string    { return s.value }
func (s *StringItem) SearchText() string { return s.value }
func (s *StringItem) Value() interface{} { return s.value }

// ModelItem adapts model information for dropdown display
type ModelItem struct {
	Provider      string
	Model         string
	DisplayName   string
	Tags          []string
	InputCost     float64
	OutputCost    float64
	LegacyCost    float64
	ContextLength int
}

func (m *ModelItem) Display() string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	// Just return the model ID, not provider/model
	return m.Model
}

// DisplayCompact returns a compact display format prioritizing pricing and context info
func (m *ModelItem) DisplayCompact(maxWidth int) string {
	baseName := m.Model
	if m.DisplayName != "" {
		baseName = m.DisplayName
	}

	// Build info components using actual data from the struct
	var pricingInfo, contextInfo string

	// Use actual pricing data
	if m.InputCost > 0 && m.OutputCost > 0 {
		// Format input/output pricing compactly
		pricingInfo = fmt.Sprintf("$%.3f/$%.3f/M", m.InputCost, m.OutputCost)
	} else if m.LegacyCost > 0 {
		pricingInfo = fmt.Sprintf("$%.3f/M", m.LegacyCost)
	} else if strings.Contains(m.Provider, "Ollama") {
		pricingInfo = "FREE"
	}

	// Use actual context length data
	if m.ContextLength > 0 {
		contextInfo = fmt.Sprintf("%dK", m.ContextLength/1000)
	}

	// If no additional info, use just the model name (not provider-prefixed)
	if pricingInfo == "" && contextInfo == "" {
		return truncateString(m.Model, maxWidth)
	}

	// Build compact display
	compactDisplay := baseName

	// Add pricing info if available
	if pricingInfo != "" {
		compactDisplay += " " + pricingInfo
	}

	// Add context info if available
	if contextInfo != "" {
		compactDisplay += " " + contextInfo
	}

	return truncateString(compactDisplay, maxWidth)
}

func (m *ModelItem) SearchText() string {
	// Include tags in search
	parts := []string{m.Provider, m.Model}
	parts = append(parts, m.Tags...)
	return strings.Join(parts, " ")
}

func (m *ModelItem) Value() interface{} {
	return m.Model
}

// ProviderItem adapts provider information for dropdown display
type ProviderItem struct {
	Name        string
	DisplayName string
	Available   bool
}

func (p *ProviderItem) Display() string {
	display := p.Name
	if p.DisplayName != "" {
		display = p.DisplayName
	}
	if !p.Available {
		display += " (API key required)"
	}
	return display
}

func (p *ProviderItem) SearchText() string {
	return p.Name + " " + p.DisplayName
}

func (p *ProviderItem) Value() interface{} {
    return p.Name
}

// KeyValueItem is a generic key-value adapter
type KeyValueItem struct {
	Key         string
	DisplayText string
	val         interface{}
}

func NewKeyValueItem(key string, display string, value interface{}) *KeyValueItem {
	return &KeyValueItem{
		Key:         key,
		DisplayText: display,
		val:         value,
	}
}

func (k *KeyValueItem) Display() string    { return k.DisplayText }
func (k *KeyValueItem) SearchText() string { return k.Key + " " + k.DisplayText }
func (k *KeyValueItem) Value() interface{} { return k.val }

// truncateString safely truncates a string to maxLen with ellipsis when possible
func truncateString(s string, maxLen int) string {
    if maxLen <= 0 {
        return ""
    }
    if len(s) <= maxLen {
        return s
    }
    if maxLen <= 3 {
        return s[:maxLen]
    }
    return s[:maxLen-3] + "..."
}
