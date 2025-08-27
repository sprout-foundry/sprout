# Output Routing Optimization

This document describes the context-aware output system implemented to optimize the user experience across different execution modes.

## Problem Statement

Previously, the same verbose output was shown in both UI and console modes, leading to:
- **UI Mode**: Redundant information cluttering logs (token usage, detailed summaries, file paths)
- **Console Mode**: Missing summary information that users expect from CLI tools
- **Inconsistent Experience**: UI header already shows token/cost info, but duplicate summaries were still printed

## Solution: Context-Aware Output System

### New UI Package Functions

**Detection:**
- `ui.IsUIActive()` - Detects if TUI sink is active (UI mode vs console mode)

**Context-Aware Output:**
- `ui.PrintContext(text, forceInUI)` - Prints only when appropriate for context
- `ui.PrintfContext(forceInUI, format, args...)` - Format version

**Enhanced Progress Events:**
- `ui.PublishProgressWithTokens()` - Sends token/cost data to UI header

### Optimization Rules

#### Console Mode (Direct CLI Usage)
✅ **Shows:** Detailed summaries, token usage, file paths, processing steps
✅ **Purpose:** Complete information for CLI users who expect verbose feedback

#### UI Mode (Interactive Agent)
✅ **Shows:** Essential progress in logs, token/cost in header
✅ **Suppresses:** Redundant summaries, verbose completion messages, file paths
✅ **Purpose:** Clean experience with information displayed in appropriate UI areas

## Implementation Details

### Agent Command (`cmd/agent.go`)
- **Console**: Full token usage summary at end
- **UI**: Token/cost data sent to header, no duplicate summary

### Code Command (`cmd/code.go`)  
- **Console**: Processing messages + token usage summary
- **UI**: Minimal output, token/cost in header

### Simplified Agent (`pkg/agent/agent.go`)
- **Console**: Detailed mode info + usage summary + completion status
- **UI**: Minimal logs + header updates

## Benefits

### For UI Users:
- 🚀 **Cleaner logs** - No redundant token summaries
- 📊 **Header information** - Token usage always visible in top-right
- 🎯 **Focus on results** - Less noise, more signal
- 📱 **Better UX** - Information appears in contextually appropriate places

### For Console Users:
- 📋 **Complete summaries** - All expected CLI feedback
- 💰 **Token tracking** - Detailed usage and cost information  
- 🕐 **Processing status** - Clear progress indicators
- 🛠️ **Tool-friendly** - Structured output for scripting

### For Developers:
- 🔄 **Consistent API** - Same functions work in both contexts
- 🎛️ **Easy control** - Simple boolean flags control output behavior
- 🧪 **Testable** - Clear separation of concerns
- 🔧 **Maintainable** - Centralized context detection

## Usage Examples

```go
// Only show in console mode
ui.PrintfContext(false, "Token Usage: %d total\n", tokens)

// Only show in UI mode (force to logs)
ui.PrintfContext(true, "Debug: Processing step complete")

// Show different content based on context
if ui.IsUIActive() {
    ui.Log("✅ Completed")
} else {
    ui.Out().Print("✅ Task completed successfully\n├─ Duration: 2.3s\n└─ Status: All validated\n")
}
```

## Future Enhancements

- Add context-aware formatting (colors, styles)
- Implement progress streaming for long operations
- Add user preferences for verbosity levels
- Support for custom output templates per context