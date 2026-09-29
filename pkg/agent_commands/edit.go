package commands

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
)

// EditCommand opens $EDITOR to compose or edit a query.
type EditCommand struct{}

func (c *EditCommand) Name() string        { return "edit" }
func (c *EditCommand) Description() string { return "Open $EDITOR to compose or edit a query" }

// Usage returns the detailed help text shown by `/help edit`.
func (c *EditCommand) Usage() string {
	return strings.Join([]string{
		"/edit              Open $EDITOR (or $VISUAL) to compose a query.",
		"/edit <text>       Pre-fill the editor with <text>.",
		"",
		"After saving and closing the editor, the buffer is sent as a message.",
	}, "\n")
}

func (c *EditCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return fmt.Errorf("[edit] agent not available")
	}

	editor := chooseEditor()
	if editor == "" {
		return fmt.Errorf("[edit] no $VISUAL or $EDITOR set and no fallback editor (vi) found")
	}

	// Pre-fill content from args.
	content := ""
	if len(args) > 0 {
		content = strings.Join(args, " ") + "\n"
	}

	tmpPath, err := writeEditTempFile(content)
	if err != nil {
		return fmt.Errorf("[edit] failed to create temp file: %w", err)
	}
	defer os.Remove(tmpPath)

	parts := append(editorArgv(editor), tmpPath)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Release stdin to cooked mode so the editor reads keystrokes
	// normally. No-op when no turn / steer reader is active.
	if err := clihooks.WithCookedStdin(cmd.Run); err != nil {
		return fmt.Errorf("[edit] %s exited: %w", editor, err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("[edit] failed to read back buffer: %w", err)
	}

	line := strings.TrimRight(string(data), "\r\n")
	if line == "" {
		fmt.Fprintln(os.Stderr, "[edit] empty buffer — nothing sent")
		return nil
	}

	return chatAgent.InjectInputContext(line)
}

// chooseEditor follows the readline convention: $VISUAL → $EDITOR →
// installed fallback editors (vi). Returns "" when nothing is found.
func chooseEditor() string {
	if e := strings.TrimSpace(os.Getenv("VISUAL")); e != "" {
		return e
	}
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	for _, candidate := range fallbackEditors() {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func fallbackEditors() []string {
	if runtime.GOOS == "windows" {
		return []string{"vi", "notepad"}
	}
	return []string{"vi"}
}

// editorArgv splits $EDITOR into program and arguments. Windows editors
// usually live under "C:\Program Files\...", so a value naming an existing
// file is taken whole, and a leading double-quoted program is honored
// ("\"C:\Program Files\...\code.exe\" --wait"); otherwise the value is
// split on whitespace.
func editorArgv(editor string) []string {
	if fi, err := os.Stat(editor); err == nil && !fi.IsDir() {
		return []string{editor}
	}
	if strings.HasPrefix(editor, `"`) {
		if end := strings.Index(editor[1:], `"`); end >= 0 {
			return append([]string{editor[1 : end+1]}, strings.Fields(editor[end+2:])...)
		}
	}
	return strings.Fields(editor)
}

// writeEditTempFile creates a temp .md file pre-populated with the given
// content so editors pick a pleasant syntax highlighter for prose.
func writeEditTempFile(content string) (string, error) {
	tmp, err := os.CreateTemp("", "sprout-edit-*.md")
	if err != nil {
		return "", err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}
