package ui

// ActionType identifies the type of UI action
type ActionType string

const (
	// UI action types
	ActionShowDropdown ActionType = "show_dropdown"
	ActionShowInput    ActionType = "show_input"
	ActionShowProgress ActionType = "show_progress"
	ActionUpdateStatus ActionType = "update_status"
	ActionShowMessage  ActionType = "show_message"
	ActionClearScreen  ActionType = "clear_screen"
)

// Action represents a UI state change request
type Action struct {
	Type    ActionType
	Payload interface{}
}

// DropdownPayload is the payload for showing a dropdown
type DropdownPayload struct {
	Items      []DropdownItem
	Options    DropdownOptions
	ResultChan chan<- DropdownResult
}

// DropdownResult is the result of a dropdown selection
type DropdownResult struct {
	Selected DropdownItem
	Error    error
}

// InputPayload is the payload for showing an input prompt
type InputPayload struct {
	Prompt     string
	Default    string
	Mask       bool
	ResultChan chan<- InputResult
}

// InputResult is the result of an input prompt
type InputResult struct {
	Value string
	Error error
}

// ProgressPayload is the payload for showing progress
type ProgressPayload struct {
	Message string
	Current int
	Total   int
	Done    bool
}

// MessagePayload is the payload for showing a message
type MessagePayload struct {
	Content  string
	Level    MessageLevel
	Duration int // milliseconds, 0 = permanent
}

// MessageLevel represents the severity of a message
type MessageLevel string

const (
	MessageInfo    MessageLevel = "info"
	MessageWarning MessageLevel = "warning"
	MessageError   MessageLevel = "error"
	MessageSuccess MessageLevel = "success"
)
