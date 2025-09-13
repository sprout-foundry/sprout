package tui

import (
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/ui"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
)

// AppState represents the current state of the TUI application
type AppState struct {
	// Core state
	StartTime time.Time
	Width     int
	Height    int

	// Agent and commands
	Agent           *agent.Agent
	CommandRegistry *commands.CommandRegistry

	// UI Components
	LogsViewport   viewport.Model
	PromptViewport viewport.Model
	TextInput      textinput.Model

	// Logs and output
	Logs      []string
	Streaming bool

	// Progress tracking
	Progress          ui.ProgressSnapshotEvent
	ProgressCollapsed bool

	// Model info
	BaseModel   string
	Provider    string
	TotalTokens int
	TotalCost   float64

	// Interactive mode
	InteractiveMode bool
	FocusedInput    bool

	// Command history
	CommandHistory []string
	HistoryIndex   int
	OriginalInput  string

	// Prompt state
	AwaitingPrompt bool
	PromptID       string
	PromptText     string
	PromptContext  string
	PromptYesNo    bool
	PromptDefault  bool
	PromptInput    string

	// View controls
	LogsCollapsed bool

	// Paste detection
	LastInputTime  time.Time
	PasteBuffer    string
	InPasteMode    bool
	PasteThreshold time.Duration

	// Command suggestions
	ShowCommandSuggestions bool
	CommandSuggestions     []string
	JustExecutedCommand    bool // Flag to prevent suggestion re-display

	// Channels
	InterruptChan chan string
}

// ViewType represents different view modes
type ViewType int

const (
	ViewStandard ViewType = iota
	ViewInteractive
)

// Message types for tea
type (
	TickMsg            time.Time
	SubscribeEventsMsg struct{}
)

// Layout constants
const (
	MinLogsHeight     = 5
	DefaultViewWidth  = 80
	DefaultViewHeight = 20
	MaxCommandHistory = 50
	EventPollInterval = 200 * time.Millisecond
	PasteThreshold    = 50 * time.Millisecond
)
