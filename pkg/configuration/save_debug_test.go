package configuration

import (
	"encoding/json"
	"testing"
)

// TestSaveConfigDebug debugs the save config flow
func TestSaveConfigDebug(t *testing.T) {
	lastSaved := map[string]interface{}{
		"resource_directory": "resources-a",
		"provider_priority":  []string{"openrouter", "deepinfra"},
	}
	
	current := map[string]interface{}{
		"resource_directory": "resources-b",
		"provider_priority":  nil,
	}
	
	latest := map[string]interface{}{
		"resource_directory": "resources-a",
		"provider_priority":  []string{"openrouter", "deepinfra"},
	}
	
	// Simulate mergeConfigChanges
	baseMap := lastSaved
	currentMap := current
	targetMap := latest
	
	applyMapDiff(baseMap, currentMap, targetMap)
	
	t.Logf("target after merge: %+v", targetMap)
	
	// Check if provider_priority is nil or a slice
	if targetMap["provider_priority"] == nil {
		t.Log("provider_priority is nil - this is correct")
	}
	if sp, ok := targetMap["provider_priority"].([]string); ok {
		t.Logf("provider_priority is []string with length %d", len(sp))
	} else {
		t.Logf("provider_priority type: %T, value: %v", targetMap["provider_priority"], targetMap["provider_priority"])
	}
	
	// Try to marshal to see what happens
	data, err := json.MarshalIndent(targetMap, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	t.Logf("JSON: %s", string(data))
}
