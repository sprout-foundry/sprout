package cmd

import (
	"os"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

func TestAvailablePersonaCompletions(t *testing.T) {
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"web_scraper": {ID: "web_scraper", Enabled: true},
			"coder":       {ID: "coder", Enabled: true},
			"debugger":    {ID: "debugger", Enabled: false},
		},
	}

	all := availablePersonaCompletions(cfg, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 enabled persona completions, got %d (%v)", len(all), all)
	}
	if all[0] != "coder" || all[1] != "web_scraper" {
		t.Fatalf("unexpected completion order/content: %v", all)
	}

	filtered := availablePersonaCompletions(cfg, "web")
	if len(filtered) != 1 || filtered[0] != "web_scraper" {
		t.Fatalf("unexpected filtered completions: %v", filtered)
	}
}

// =============================================================================
// createChatAgent
// =============================================================================

func TestCreateChatAgent_Default(t *testing.T) {
	// Save and restore original values
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	// Reset to minimal defaults
	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	// Verify base system prompt is set (default prompt should exist)
	if a.GetSystemPrompt() == "" {
		t.Error("expected non-empty system prompt")
	}
}

func TestCreateChatAgent_WithProviderAndModel(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = "openrouter"
	agentModel = "test-model"
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with provider+model unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithModelOnly(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = "claude-3-opus"
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with model only unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithProviderOnly(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = "openai"
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with provider only unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithSystemPrompt(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = "You are a test assistant."
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with system prompt unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	prompt := a.GetSystemPrompt()
	if prompt != "You are a test assistant." {
		t.Errorf("expected system prompt to be set, got %q", prompt)
	}
}

func TestCreateChatAgent_WithSystemPromptFile(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	// Create a temp file for testing
	tmpFile, err := os.CreateTemp("", "system_prompt_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.WriteString("Custom system prompt from file.")
	tmpFile.Close()

	agentSystemPromptFile = tmpFile.Name()

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with system prompt file unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	prompt := a.GetSystemPrompt()
	if prompt != "Custom system prompt from file." {
		t.Errorf("expected system prompt from file, got %q", prompt)
	}
}

func TestCreateChatAgent_WithSystemPromptFileNotFound(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = "/nonexistent/path/to/prompt.txt"

	_, err := createChatAgent()
	if err == nil {
		t.Fatal("expected error for non-existent system prompt file")
	}
}

func TestCreateChatAgent_WithMaxIterations(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 10
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with max iterations unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	if a.GetMaxIterations() != 10 {
		t.Errorf("expected max iterations 10, got %d", a.GetMaxIterations())
	}
}
