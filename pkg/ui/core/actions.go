package core

// Common action types
const (
	// UI State Actions
	ActionShowDropdown   = "UI/SHOW_DROPDOWN"
	ActionHideDropdown   = "UI/HIDE_DROPDOWN"
	ActionUpdateDropdown = "UI/UPDATE_DROPDOWN"

	// Input Actions
	ActionSetInput     = "INPUT/SET"
	ActionClearInput   = "INPUT/CLEAR"
	ActionUpdateCursor = "INPUT/UPDATE_CURSOR"

	// Navigation Actions
	ActionNavigateUp    = "NAV/UP"
	ActionNavigateDown  = "NAV/DOWN"
	ActionNavigateLeft  = "NAV/LEFT"
	ActionNavigateRight = "NAV/RIGHT"
	ActionSelect        = "NAV/SELECT"
	ActionCancel        = "NAV/CANCEL"

	// Terminal Actions
	ActionResize     = "TERMINAL/RESIZE"
	ActionSetRawMode = "TERMINAL/SET_RAW_MODE"

	// Focus Actions
	ActionFocusComponent = "FOCUS/SET"
	ActionBlurComponent  = "FOCUS/BLUR"

	// Modal Actions
	ActionOpenModal  = "MODAL/OPEN"
	ActionCloseModal = "MODAL/CLOSE"

	// Command Actions
	ActionExecuteCommand = "COMMAND/EXECUTE"
	ActionCommandResult  = "COMMAND/RESULT"
	ActionCommandError   = "COMMAND/ERROR"
)

// Action creators

// ShowDropdownAction creates an action to show a dropdown
func ShowDropdownAction(id string, items []interface{}, options map[string]interface{}) Action {
	return Action{
		Type: ActionShowDropdown,
		Payload: map[string]interface{}{
			"id":      id,
			"items":   items,
			"options": options,
		},
	}
}

// HideDropdownAction creates an action to hide a dropdown
func HideDropdownAction(id string) Action {
	return Action{
		Type: ActionHideDropdown,
		Payload: map[string]interface{}{
			"id": id,
		},
	}
}

// UpdateDropdownAction creates an action to update dropdown state
func UpdateDropdownAction(id string, updates map[string]interface{}) Action {
	return Action{
		Type: ActionUpdateDropdown,
		Payload: map[string]interface{}{
			"id":      id,
			"updates": updates,
		},
	}
}

// NavigateAction creates a navigation action
func NavigateAction(direction string) Action {
	switch direction {
	case "up":
		return Action{Type: ActionNavigateUp}
	case "down":
		return Action{Type: ActionNavigateDown}
	case "left":
		return Action{Type: ActionNavigateLeft}
	case "right":
		return Action{Type: ActionNavigateRight}
	default:
		return Action{}
	}
}

// SelectAction creates a select action
func SelectAction() Action {
	return Action{Type: ActionSelect}
}

// CancelAction creates a cancel action
func CancelAction() Action {
	return Action{Type: ActionCancel}
}

// ResizeAction creates a terminal resize action
func ResizeAction(width, height int) Action {
	return Action{
		Type: ActionResize,
		Payload: map[string]interface{}{
			"width":  width,
			"height": height,
		},
	}
}

// FocusComponentAction creates an action to focus a component
func FocusComponentAction(componentID string) Action {
	return Action{
		Type: ActionFocusComponent,
		Payload: map[string]interface{}{
			"componentID": componentID,
		},
	}
}

// ExecuteCommandAction creates an action to execute a command
func ExecuteCommandAction(command string, args []string) Action {
	return Action{
		Type: ActionExecuteCommand,
		Payload: map[string]interface{}{
			"command": command,
			"args":    args,
		},
	}
}
