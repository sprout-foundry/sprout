package ui

// QuickOption represents a simple choice in a quick prompt
type QuickOption struct {
    Label  string // display label
    Value  string // returned value
    Hotkey rune   // optional explicit hotkey (lowercase), 0 for auto
}

