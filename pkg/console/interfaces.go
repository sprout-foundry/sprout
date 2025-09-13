package console

import (
	"context"
)

// Component is the base interface for all UI components
type Component interface {
	// Lifecycle methods
	Init(ctx context.Context, deps Dependencies) error
	Start() error
	Stop() error
	Cleanup() error

	// Component identity
	ID() string
	Type() string

	// Rendering
	Render() error
	NeedsRedraw() bool

	// Input handling
	HandleInput(input []byte) (handled bool, err error)
	CanHandleInput() bool

	// Region management
	GetRegion() string
	SetRegion(region string)
}

// Dependencies provides access to core services
type Dependencies struct {
	Terminal TerminalManager
	Layout   LayoutManager
	State    StateManager
	Events   EventBus
}

// TerminalManager handles all terminal operations
type TerminalManager interface {
	// Initialization
	Init() error
	Cleanup() error

	// Size detection
	GetSize() (width, height int, err error)
	OnResize(callback func(width, height int))

	// Terminal modes
	SetRawMode(enabled bool) error
	IsRawMode() bool

	// Cursor control
	MoveCursor(x, y int) error
	SaveCursor() error
	RestoreCursor() error
	HideCursor() error
	ShowCursor() error

	// Screen operations
	ClearScreen() error
	ClearLine() error
	ClearToEndOfLine() error
	ClearToEndOfScreen() error

	// Output
	Write(data []byte) (int, error)
	WriteAt(x, y int, data []byte) error
	Flush() error
}

// Region represents a rectangular area on the terminal
type Region struct {
	X, Y          int // Top-left corner (0-based)
	Width, Height int
	ZOrder        int // Higher values are on top
	Visible       bool
}

// LayoutManager manages screen regions and rendering order
type LayoutManager interface {
	// Region management
	DefineRegion(name string, region Region) error
	UpdateRegion(name string, region Region) error
	GetRegion(name string) (Region, error)
	RemoveRegion(name string) error
	ListRegions() []string

	// Layout calculations
	CalculateLayout(termWidth, termHeight int) error
	GetAvailableSpace() Region

	// Rendering control
	BeginBatch()
	EndBatch() error
	RequestRedraw(regionName string)
	ForceRedraw()

	// Z-order management
	SetZOrder(regionName string, zOrder int) error
	GetRenderOrder() []string
}

// StateManager provides centralized state management
type StateManager interface {
	// State operations
	Get(key string) (interface{}, bool)
	Set(key string, value interface{})
	Delete(key string)
	Clear()

	// Transactions
	BeginTransaction()
	Commit()
	Rollback()

	// Subscriptions
	Subscribe(pattern string, callback StateCallback) string
	Unsubscribe(subscriptionID string)

	// Persistence
	Save(path string) error
	Load(path string) error
}

// StateCallback is called when state changes
type StateCallback func(key string, oldValue, newValue interface{})

// Event represents a system event
type Event struct {
	ID        string
	Type      string
	Source    string
	Target    string // Optional specific target
	Data      interface{}
	Timestamp int64
}

// EventBus provides component communication
type EventBus interface {
	// Publishing
	Publish(event Event) error
	PublishAsync(event Event)

	// Subscriptions
	Subscribe(eventType string, handler EventHandler) string
	SubscribeToSource(source string, handler EventHandler) string
	Unsubscribe(subscriptionID string)

	// Event filtering
	SetFilter(filter EventFilter)

	// Control
	Start() error
	Stop() error
}

// EventHandler processes events
type EventHandler func(event Event) error

// EventFilter determines if an event should be processed
type EventFilter func(event Event) bool

// ConsoleApp is the main application controller
type ConsoleApp interface {
	// Lifecycle
	Init(config *Config) error
	Start() error
	Stop() error
	Run() error

	// Component management
	AddComponent(component Component) error
	RemoveComponent(componentID string) error
	GetComponent(componentID string) (Component, bool)
	ListComponents() []string

	// Service access
	Terminal() TerminalManager
	Layout() LayoutManager
	State() StateManager
	Events() EventBus

	// Configuration
	GetConfig() *Config
	UpdateConfig(config *Config) error
}

// Config holds application configuration
type Config struct {
	// Terminal settings
	RawMode      bool
	MouseEnabled bool
	AltScreen    bool

	// Layout settings
	MinWidth  int
	MinHeight int

	// Component settings
	Components []ComponentConfig

	// Event settings
	EventQueueSize int

	// Debug settings
	Debug   bool
	LogFile string
}

// ComponentConfig configures a component
type ComponentConfig struct {
	ID      string
	Type    string
	Region  string
	Config  map[string]interface{}
	Enabled bool
}
