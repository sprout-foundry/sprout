# Console Component Architecture

## Overview

Console components are the building blocks of the terminal UI. Each component:
- Manages a specific UI concern (input, display, interaction)
- Integrates with the console framework
- Communicates via events
- Respects layout constraints

## Component Lifecycle

```
Init → Start → Active (Render/HandleInput) → Stop → Cleanup
```

## Creating a Component

### 1. Embed BaseComponent

```go
type MyComponent struct {
    *console.BaseComponent
    // Component-specific fields
}
```

### 2. Implement Required Methods

```go
// Lifecycle
func (c *MyComponent) Init(ctx context.Context, deps Dependencies) error
func (c *MyComponent) Start() error
func (c *MyComponent) Stop() error
func (c *MyComponent) Cleanup() error

// Rendering
func (c *MyComponent) Render() error
func (c *MyComponent) NeedsRedraw() bool

// Input (if interactive)
func (c *MyComponent) HandleInput(input []byte) (handled bool, err error)
func (c *MyComponent) CanHandleInput() bool
```

### 3. Use Framework Services

```go
// Terminal operations
c.deps.Terminal.MoveCursor(x, y)
c.deps.Terminal.WriteText("Hello")

// Layout management
c.deps.Layout.DefineRegion("myregion", region)

// Event communication
c.deps.Events.Publish(Event{Type: "my.event"})

// State management
c.deps.State.Set("key", value)
```

## Input Handling

### Regular Input
Components receive input when `CanHandleInput()` returns true.

### Exclusive Input
For modal components (like dropdowns):

```go
// Request exclusive input
c.deps.Events.Publish(Event{
    Type: "input.request_exclusive",
    Source: c.ID(),
})

// Release exclusive input
c.deps.Events.Publish(Event{
    Type: "input.release_exclusive",
    Source: c.ID(),
})
```

## Layout Integration

### Define a Region
```go
region := console.Region{
    X: 0, Y: 10,
    Width: 80, Height: 20,
    ZOrder: 1,
    Visible: true,
}
c.deps.Layout.DefineRegion("mycomponent", region)
```

### Handle Resize
```go
// Subscribe to resize events
c.deps.Events.Subscribe("terminal.resized", func(e Event) error {
    // Update component layout
    return c.handleResize()
})
```

## Event Communication

### Publishing Events
```go
c.deps.Events.Publish(Event{
    Type: "component.action",
    Source: c.ID(),
    Data: map[string]interface{}{
        "value": selectedValue,
    },
})
```

### Subscribing to Events
```go
c.deps.Events.Subscribe("other.component.event", func(e Event) error {
    // Handle event
    return nil
})
```

## Component Patterns

### 1. Display Components
- Render content in their region
- Update on state changes
- Example: FooterComponent, StatusBar

### 2. Input Components
- Handle keyboard input
- Manage focus state
- Example: InputManager, CommandLine

### 3. Modal Components
- Request exclusive input
- Overlay other content
- Example: DropdownComponent, Dialog

### 4. Container Components
- Manage child components
- Coordinate layout
- Example: AgentConsole, SplitPane

## Best Practices

1. **State Management**: Keep component state minimal and focused
2. **Event Names**: Use namespaced events (e.g., "dropdown.selected")
3. **Error Handling**: Always handle and propagate errors appropriately
4. **Cleanup**: Release resources in Cleanup() method
5. **Thread Safety**: Use mutexes for concurrent access to state

## Example: Simple Counter Component

```go
type CounterComponent struct {
    *console.BaseComponent
    count int
    mu    sync.Mutex
}

func NewCounterComponent() *CounterComponent {
    return &CounterComponent{
        BaseComponent: console.NewBaseComponent("counter", "counter"),
    }
}

func (c *CounterComponent) Init(ctx context.Context, deps Dependencies) error {
    if err := c.BaseComponent.Init(ctx, deps); err != nil {
        return err
    }
    
    // Define our display region
    c.deps.Layout.DefineRegion("counter", console.Region{
        X: 0, Y: 0, Width: 20, Height: 1,
    })
    
    // Subscribe to increment events
    c.deps.Events.Subscribe("counter.increment", func(e Event) error {
        c.mu.Lock()
        c.count++
        c.mu.Unlock()
        c.SetNeedsRedraw(true)
        return nil
    })
    
    return nil
}

func (c *CounterComponent) Render() error {
    c.mu.Lock()
    count := c.count
    c.mu.Unlock()
    
    // Clear our region and display count
    region, _ := c.deps.Layout.GetRegion("counter")
    c.deps.Terminal.MoveCursor(region.X, region.Y)
    c.deps.Terminal.ClearToEndOfLine()
    c.deps.Terminal.WriteText(fmt.Sprintf("Count: %d", count))
    
    c.SetNeedsRedraw(false)
    return nil
}

func (c *CounterComponent) HandleInput(input []byte) (bool, error) {
    if input[0] == '+' {
        c.deps.Events.Publish(Event{
            Type: "counter.increment",
            Source: c.ID(),
        })
        return true, nil
    }
    return false, nil
}
```