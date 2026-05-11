package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandItemDisplay(t *testing.T) {
	tests := []struct {
		name string
		item CommandItem
		want string
	}{
		{
			name: "command without aliases",
			item: CommandItem{
				Name:        "help",
				Description: "Show help information",
				Aliases:     nil,
			},
			want: "/help - Show help information",
		},
		{
			name: "command with one alias",
			item: CommandItem{
				Name:        "shell",
				Description: "Execute shell commands",
				Aliases:     []string{"sh"},
			},
			want: "/shell (/sh) - Execute shell commands",
		},
		{
			name: "command with multiple aliases",
			item: CommandItem{
				Name:        "exec",
				Description: "Execute a command",
				Aliases:     []string{"run", "cmd"},
			},
			want: "/exec (/run, /cmd) - Execute a command",
		},
		{
			name: "command with empty alias list",
			item: CommandItem{
				Name:        "status",
				Description: "Show status",
				Aliases:     []string{},
			},
			want: "/status - Show status",
		},
		{
			name: "command with empty name and description",
			item: CommandItem{
				Name:        "",
				Description: "",
				Aliases:     nil,
			},
			want: "/ - ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.Display()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCommandItemSearchText(t *testing.T) {
	tests := []struct {
		name string
		item CommandItem
		want string
	}{
		{
			name: "command without aliases",
			item: CommandItem{
				Name:        "help",
				Description: "Show help information",
				Aliases:     nil,
			},
			want: "help show help information",
		},
		{
			name: "command with aliases",
			item: CommandItem{
				Name:        "shell",
				Description: "Execute shell commands",
				Aliases:     []string{"sh", "cmd"},
			},
			want: "shell sh cmd execute shell commands",
		},
		{
			name: "command with mixed case name and description",
			item: CommandItem{
				Name:        "GitStatus",
				Description: "Show Git Repository Status",
				Aliases:     []string{"gs"},
			},
			want: "GitStatus gs show git repository status",
		},
		{
			name: "command with empty fields",
			item: CommandItem{
				Name:        "",
				Description: "",
				Aliases:     nil,
			},
			want: " ", // Just one trailing space
		},
		{
			name: "command with only name",
			item: CommandItem{
				Name:        "test",
				Description: "",
				Aliases:     nil,
			},
			want: "test ", // One trailing space from empty description
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.SearchText()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCommandItemValue(t *testing.T) {
	tests := []struct {
		name string
		item CommandItem
		want string
	}{
		{
			name: "simple command name",
			item: CommandItem{
				Name: "help",
			},
			want: "/help",
		},
		{
			name: "command with underscores",
			item: CommandItem{
				Name: "git_commit",
			},
			want: "/git_commit",
		},
		{
			name: "command with hyphens",
			item: CommandItem{
				Name: "sub-agent",
			},
			want: "/sub-agent",
		},
		{
			name: "empty command name",
			item: CommandItem{
				Name: "",
			},
			want: "/",
		},
		{
			name: "command with numbers",
			item: CommandItem{
				Name: "model3",
			},
			want: "/model3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.Value()
			assert.Equal(t, tt.want, got)
		})
	}
}
