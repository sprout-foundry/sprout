package core

// UIReducer manages UI state like dropdowns, modals, etc.
func UIReducer(state State, action Action) State {
	if state == nil {
		state = make(State)
	}

	switch action.Type {
	case ActionShowDropdown:
		payload := action.Payload.(map[string]interface{})

		// Deep copy state first
		newState := deepCopyState(state)
		dropdowns := getOrCreateMap(newState, "dropdowns")

		dropdowns[payload["id"].(string)] = map[string]interface{}{
			"visible":       true,
			"items":         payload["items"],
			"options":       payload["options"],
			"selectedIndex": 0,
			"searchText":    "",
			"filteredItems": payload["items"],
		}

		newState["dropdowns"] = dropdowns
		return newState

	case ActionHideDropdown:
		payload := action.Payload.(map[string]interface{})

		// Deep copy state first
		newState := deepCopyState(state)
		dropdowns := getOrCreateMap(newState, "dropdowns")

		delete(dropdowns, payload["id"].(string))

		newState["dropdowns"] = dropdowns
		return newState

	case ActionUpdateDropdown:
		payload := action.Payload.(map[string]interface{})
		dropdownID := payload["id"].(string)

		// Deep copy the state first to ensure immutability
		newState := deepCopyState(state)
		dropdowns := getOrCreateMap(newState, "dropdowns")

		if dropdown, exists := dropdowns[dropdownID].(map[string]interface{}); exists {
			// Create a new dropdown object with updates
			newDropdown := make(map[string]interface{})
			for k, v := range dropdown {
				newDropdown[k] = v
			}

			updates := payload["updates"].(map[string]interface{})
			for k, v := range updates {
				newDropdown[k] = v
			}

			dropdowns[dropdownID] = newDropdown
			newState["dropdowns"] = dropdowns
			return newState
		}

		return state

	default:
		return state
	}
}

// FocusReducer manages focus state
func FocusReducer(state State, action Action) State {
	if state == nil {
		state = make(State)
	}

	switch action.Type {
	case ActionFocusComponent:
		payload := action.Payload.(map[string]interface{})
		newState := deepCopyState(state)
		newState["focusedComponent"] = payload["componentID"]
		return newState

	case ActionBlurComponent:
		newState := deepCopyState(state)
		delete(newState, "focusedComponent")
		return newState

	case ActionSelect:
		newState := deepCopyState(state)
		newState["lastAction"] = "select"
		return newState

	case ActionCancel:
		newState := deepCopyState(state)
		newState["lastAction"] = "cancel"
		return newState

	case "FOCUS/CLEAR_LAST_ACTION":
		newState := deepCopyState(state)
		delete(newState, "lastAction")
		return newState

	default:
		return state
	}
}

// InputReducer manages input state
func InputReducer(state State, action Action) State {
	if state == nil {
		state = State{
			"text":       "",
			"cursorPos":  0,
			"history":    []string{},
			"historyPos": -1,
		}
	}

	switch action.Type {
	case ActionSetInput:
		payload := action.Payload.(map[string]interface{})
		newState := deepCopyState(state)
		newState["text"] = payload["text"]
		if cursor, ok := payload["cursorPos"]; ok {
			newState["cursorPos"] = cursor
		}
		return newState

	case ActionClearInput:
		newState := deepCopyState(state)
		newState["text"] = ""
		newState["cursorPos"] = 0
		newState["historyPos"] = -1
		return newState

	case ActionUpdateCursor:
		payload := action.Payload.(map[string]interface{})
		newState := deepCopyState(state)
		newState["cursorPos"] = payload["position"]
		return newState

	default:
		return state
	}
}

// TerminalReducer manages terminal state
func TerminalReducer(state State, action Action) State {
	if state == nil {
		state = State{
			"width":   80,
			"height":  24,
			"rawMode": false,
		}
	}

	switch action.Type {
	case ActionResize:
		payload := action.Payload.(map[string]interface{})
		newState := deepCopyState(state)
		newState["width"] = payload["width"]
		newState["height"] = payload["height"]
		return newState

	case ActionSetRawMode:
		payload := action.Payload.(map[string]interface{})
		newState := deepCopyState(state)
		newState["rawMode"] = payload["enabled"]
		return newState

	default:
		return state
	}
}

// CommandReducer manages command execution state
func CommandReducer(state State, action Action) State {
	if state == nil {
		state = State{
			"executing":   false,
			"lastCommand": nil,
			"lastResult":  nil,
			"lastError":   nil,
		}
	}

	switch action.Type {
	case ActionExecuteCommand:
		payload := action.Payload.(map[string]interface{})
		newState := deepCopyState(state)
		newState["executing"] = true
		newState["lastCommand"] = payload
		newState["lastError"] = nil
		return newState

	case ActionCommandResult:
		payload := action.Payload.(map[string]interface{})
		newState := deepCopyState(state)
		newState["executing"] = false
		newState["lastResult"] = payload["result"]
		return newState

	case ActionCommandError:
		payload := action.Payload.(map[string]interface{})
		newState := deepCopyState(state)
		newState["executing"] = false
		newState["lastError"] = payload["error"]
		return newState

	default:
		return state
	}
}

// RootReducer combines all reducers
func RootReducer() Reducer {
	return CombineReducers(map[string]Reducer{
		"ui":       UIReducer,
		"focus":    FocusReducer,
		"input":    InputReducer,
		"terminal": TerminalReducer,
		"command":  CommandReducer,
	})
}

// Helper functions

func getOrCreateMap(state State, key string) map[string]interface{} {
	if val, ok := state[key].(map[string]interface{}); ok {
		// Return a copy to avoid mutations
		newMap := make(map[string]interface{})
		for k, v := range val {
			newMap[k] = v
		}
		return newMap
	}
	return make(map[string]interface{})
}
