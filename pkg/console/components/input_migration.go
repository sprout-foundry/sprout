package components

// InputHandlerAdapter wraps InputComponent to implement the old InputHandler interface
type InputHandlerAdapter struct {
	*InputComponent
}

// NewInputHandlerAdapter creates an adapter for legacy code
func NewInputHandlerAdapter(prompt string) *InputHandlerAdapter {
	return &InputHandlerAdapter{
		InputComponent: NewInputComponent("adapter", prompt),
	}
}

// Close implements the legacy interface
func (a *InputHandlerAdapter) Close() error {
	return a.Cleanup()
}

// Migration helper functions

// MigrateSimpleDirectInput replaces SimpleDirectInputHandler
func MigrateSimpleDirectInput(prompt string) *InputHandlerAdapter {
	adapter := NewInputHandlerAdapter(prompt)
	adapter.SetEcho(true).SetHistory(true)
	return adapter
}

// MigrateBufferedInput replaces BufferedInputHandler
func MigrateBufferedInput(prompt string) *InputHandlerAdapter {
	adapter := NewInputHandlerAdapter(prompt)
	adapter.SetEcho(true).SetHistory(true)
	return adapter
}

// MigrateDirectInput replaces DirectInputHandler
func MigrateDirectInput(prompt string) *InputHandlerAdapter {
	adapter := NewInputHandlerAdapter(prompt)
	adapter.SetEcho(true).SetHistory(true)
	return adapter
}

// MigrateRawInput replaces RawInputHandler
func MigrateRawInput(prompt string) *InputHandlerAdapter {
	adapter := NewInputHandlerAdapter(prompt)
	adapter.SetEcho(false).SetHistory(false)
	return adapter
}

// MigrateUnbufferedInput replaces UnbufferedInputHandler
func MigrateUnbufferedInput(prompt string) *InputHandlerAdapter {
	adapter := NewInputHandlerAdapter(prompt)
	adapter.SetEcho(true).SetHistory(true)
	return adapter
}

// MigrateNobufInput replaces NobufInputHandler
func MigrateNobufInput(prompt string) *InputHandlerAdapter {
	adapter := NewInputHandlerAdapter(prompt)
	adapter.SetEcho(true).SetHistory(true)
	return adapter
}

// ConfigureForPassword configures input for password entry
func ConfigureForPassword(adapter *InputHandlerAdapter) *InputHandlerAdapter {
	adapter.SetEcho(false).SetHistory(false)
	return adapter
}

// ConfigureForMultiline configures input for multiline entry
func ConfigureForMultiline(adapter *InputHandlerAdapter) *InputHandlerAdapter {
	adapter.SetMultiline(true)
	return adapter
}
