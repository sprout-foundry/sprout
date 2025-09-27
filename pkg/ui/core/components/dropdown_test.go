package components

import (
	"context"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/ui/core"
)

// mockRenderer captures rendering operations for testing
type mockRenderer struct {
	operations []string
	cleared    bool
}

func (m *mockRenderer) Clear() error {
	m.cleared = true
	m.operations = append(m.operations, "CLEAR")
	return nil
}

func (m *mockRenderer) DrawText(x, y int, text string) error {
	m.operations = append(m.operations, "TEXT", text)
	return nil
}

func (m *mockRenderer) DrawBox(x, y, width, height int) error {
	m.operations = append(m.operations, "BOX")
	return nil
}

func (m *mockRenderer) Flush() error {
	m.operations = append(m.operations, "FLUSH")
	return nil
}

func (m *mockRenderer) GetSize() (width, height int) {
	return 80, 24
}

func TestDropdownComponent_Render(t *testing.T) {
	// Create store with initial state
	reducer := core.CombineReducers(map[string]core.Reducer{
		"ui":       core.UIReducer,
		"focus":    core.FocusReducer,
		"terminal": core.TerminalReducer,
	})
	store := core.NewStore(reducer, nil)

	// Set terminal size
	store.Dispatch(core.Action{
		Type: "RESIZE",
		Payload: map[string]interface{}{
			"width":  80,
			"height": 24,
		},
	})

	// Create renderer
	renderer := &mockRenderer{}

	// Create dropdown
	dropdown := NewDropdownComponent("test-dropdown", store, renderer)

	// Show dropdown with items
	items := []interface{}{
		"Option 1",
		"Option 2",
		"Option 3",
	}

	store.Dispatch(core.ShowDropdownAction("test-dropdown", items, map[string]interface{}{
		"prompt":       "Select an option:",
		"searchPrompt": "Search: ",
	}))

	// Render
	err := dropdown.Render(context.Background())
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify operations
	if !renderer.cleared {
		t.Error("Expected renderer to be cleared")
	}

	// Check that we have the expected operations
	hasBox := false
	hasPrompt := false
	hasSearch := false
	hasOptions := false

	for _, op := range renderer.operations {
		if op == "BOX" {
			hasBox = true
		}
		if strings.Contains(op, "Select an option:") {
			hasPrompt = true
		}
		if strings.Contains(op, "Search:") {
			hasSearch = true
		}
		if strings.Contains(op, "Option") {
			hasOptions = true
		}
	}

	if !hasBox {
		t.Error("Expected box to be drawn")
	}
	if !hasPrompt {
		t.Error("Expected prompt to be shown")
	}
	if !hasSearch {
		t.Error("Expected search box to be shown")
	}
	if !hasOptions {
		t.Error("Expected options to be displayed")
	}
}

func TestDropdownComponent_Search(t *testing.T) {
	// Create store
	reducer := core.CombineReducers(map[string]core.Reducer{
		"ui":    core.UIReducer,
		"focus": core.FocusReducer,
	})
	store := core.NewStore(reducer, nil)

	// Create dropdown
	renderer := &mockRenderer{}
	dropdown := NewDropdownComponent("test-dropdown", store, renderer)

	// Show dropdown
	items := []interface{}{
		"Apple",
		"Banana",
		"Cherry",
		"Date",
	}

	store.Dispatch(core.ShowDropdownAction("test-dropdown", items, nil))

	// Type 'c' to search
	dropdown.HandleInput([]byte{'c'})

	// Get state and check filtered items
	state := store.GetState()
	ui := state["ui"].(core.State)
	dropdowns := ui["dropdowns"].(map[string]interface{})
	dropdownState := dropdowns["test-dropdown"].(map[string]interface{})

	searchText := dropdownState["searchText"].(string)
	if searchText != "c" {
		t.Errorf("Expected search text 'c', got '%s'", searchText)
	}

	filteredItems := dropdownState["filteredItems"].([]interface{})
	if len(filteredItems) != 1 {
		t.Errorf("Expected 1 filtered item (Cherry), got %d", len(filteredItems))
	}

	if len(filteredItems) > 0 && filteredItems[0] != "Cherry" {
		t.Errorf("Expected filtered item to be 'Cherry', got %v", filteredItems[0])
	}
}

func TestDropdownComponent_Backspace(t *testing.T) {
    // Create store
    reducer := core.CombineReducers(map[string]core.Reducer{
        "ui":    core.UIReducer,
        "focus": core.FocusReducer,
    })
    store := core.NewStore(reducer, nil)

    // Create dropdown
    renderer := &mockRenderer{}
    dropdown := NewDropdownComponent("test-dropdown", store, renderer)

    // Show dropdown
    items := []interface{}{"Alpha", "Beta"}
    store.Dispatch(core.ShowDropdownAction("test-dropdown", items, nil))

    // Type 'a' and then backspace
    dropdown.HandleInput([]byte{'a'})
    dropdown.HandleInput([]byte{127}) // Backspace

    // Check state
    state := store.GetState()
    ui := state["ui"].(core.State)
    dropdowns := ui["dropdowns"].(map[string]interface{})
    dropdownState := dropdowns["test-dropdown"].(map[string]interface{})

    searchText := dropdownState["searchText"].(string)
    if searchText != "" {
        t.Errorf("Expected empty search after backspace, got %q", searchText)
    }

    filtered := dropdownState["filteredItems"].([]interface{})
    if len(filtered) != 2 {
        t.Errorf("Expected filtered list reset to all items, got %d", len(filtered))
    }
}

func TestDropdownComponent_Navigation(t *testing.T) {
	// Create store
	reducer := core.CombineReducers(map[string]core.Reducer{
		"ui":    core.UIReducer,
		"focus": core.FocusReducer,
	})
	store := core.NewStore(reducer, nil)

	// Create dropdown
	renderer := &mockRenderer{}
	dropdown := NewDropdownComponent("test-dropdown", store, renderer)

	// Show dropdown
	items := []interface{}{"Item 1", "Item 2", "Item 3"}
	store.Dispatch(core.ShowDropdownAction("test-dropdown", items, nil))

	// Navigate down
	dropdown.HandleInput([]byte{27, '[', 'B'}) // Down arrow

	// Check selected index
	state := store.GetState()
	ui := state["ui"].(core.State)
	dropdowns := ui["dropdowns"].(map[string]interface{})
	dropdownState := dropdowns["test-dropdown"].(map[string]interface{})
	selectedIndex := dropdownState["selectedIndex"].(int)

	if selectedIndex != 1 {
		t.Errorf("Expected selected index 1 after down arrow, got %d", selectedIndex)
	}

	// Navigate up
	dropdown.HandleInput([]byte{27, '[', 'A'}) // Up arrow

	// Re-check
	state = store.GetState()
	ui = state["ui"].(core.State)
	dropdowns = ui["dropdowns"].(map[string]interface{})
	dropdownState = dropdowns["test-dropdown"].(map[string]interface{})
	selectedIndex = dropdownState["selectedIndex"].(int)

	if selectedIndex != 0 {
		t.Errorf("Expected selected index 0 after up arrow, got %d", selectedIndex)
	}
}
