package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// dropdownItem implements list.Item interface
type dropdownItem struct {
	title       string
	description string
	value       interface{}
}

func (i dropdownItem) FilterValue() string { return i.title }
func (i dropdownItem) Title() string       { return i.title }
func (i dropdownItem) Description() string { return i.description }

// dropdownModel is a temporary model for command selection
type dropdownModel struct {
	list     list.Model
	choice   string
	quitting bool
}

func newDropdownModel(items []list.Item, title string) dropdownModel {
	const listHeight = 20
	const listWidth = 80

	l := list.New(items, list.NewDefaultDelegate(), listWidth, listHeight)
	l.Title = title
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)
	l.DisableQuitKeybindings() // We'll handle quit ourselves

	// Customize styles
	l.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		MarginLeft(2)

	return dropdownModel{
		list: l,
	}
}

func (m dropdownModel) Init() tea.Cmd {
	return nil
}

func (m dropdownModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if selected, ok := m.list.SelectedItem().(dropdownItem); ok {
				m.choice = selected.value.(string)
			}
			m.quitting = true
			return m, tea.Quit
		case "q", "esc", "ctrl+c":
			m.quitting = true
			m.choice = "" // Empty means cancelled
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 2) // Leave room for borders
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m dropdownModel) View() string {
	if m.quitting {
		return ""
	}
	return m.list.View()
}

// ShowCommandDropdown displays a command selector in the TUI and returns the selected command
func ShowCommandDropdown(commands []Command) (string, error) {
	// Convert commands to list items
	items := make([]list.Item, 0, len(commands))

	for _, cmd := range commands {
		displayName := fmt.Sprintf("/%s", cmd.Name())

		// Add aliases if any
		aliases := getCommandAliases(cmd.Name())
		if len(aliases) > 0 {
			displayName += fmt.Sprintf(" (/%s)", strings.Join(aliases, ", /"))
		}

		item := dropdownItem{
			title:       displayName,
			description: cmd.Description(),
			value:       "/" + cmd.Name(),
		}
		items = append(items, item)
	}

	// Create and run the dropdown model
	m := newDropdownModel(items, "Select a command:")
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	dropdown := finalModel.(dropdownModel)
	if dropdown.choice == "" {
		return "", fmt.Errorf("cancelled")
	}

	return dropdown.choice, nil
}

// getCommandAliases returns common aliases for a command
func getCommandAliases(name string) []string {
	aliases := map[string][]string{
		"help":     {"h", "?"},
		"exit":     {"quit", "q"},
		"models":   {"model"},
		"provider": {"providers"},
		"changes":  {"diff"},
		"status":   {"st"},
		"exec":     {"run", "e"},
		"shell":    {"sh", "bash"},
	}
	return aliases[name]
}

// Command interface (duplicated here to avoid circular import)
type Command interface {
	Name() string
	Description() string
}

// ShowModelDropdown displays a model selector in the TUI and returns the selected model ID
func ShowModelDropdown(items []ModelItem) (string, error) {
	// Convert to list items
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = dropdownItem{
			title:       item.Display,
			description: item.Description,
			value:       item.ID,
		}
	}

	// Create and run the dropdown model
	m := newDropdownModel(listItems, "Select a model:")
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	dropdown := finalModel.(dropdownModel)
	if dropdown.choice == "" {
		return "", fmt.Errorf("cancelled")
	}

	return dropdown.choice, nil
}

// ModelItem represents a model for selection
type ModelItem struct {
	ID          string
	Display     string
	Description string
}
