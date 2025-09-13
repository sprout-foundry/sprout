package main

import (
	"context"
	"fmt"
	"log"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/console/components"
)

func main() {
	// Create agent
	agent, err := agent.New(agent.Config{
		Provider: "openai",
		Model:    "gpt-4",
		Debug:    true,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create console app
	app := console.NewApp()

	// Configure app
	config := &console.Config{
		RawMode:      true,
		MouseEnabled: false,
		AltScreen:    false,
		Components: []console.ComponentConfig{
			{
				ID:      "agent-console",
				Type:    "agent",
				Region:  "main",
				Enabled: true,
			},
			{
				ID:      "footer",
				Type:    "footer",
				Region:  "footer",
				Enabled: true,
			},
		},
	}

	// Initialize app
	if err := app.Init(config); err != nil {
		log.Fatal(err)
	}

	// Create and add agent console component
	agentConsole := components.NewAgentConsole(agent, nil)
	if err := app.AddComponent(agentConsole); err != nil {
		log.Fatal(err)
	}

	// Create and add footer component
	footer := components.NewFooterComponent()
	if err := app.AddComponent(footer); err != nil {
		log.Fatal(err)
	}

	// Run the app
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// Example of using the input component standalone
func standaloneInputExample() {
	input := components.NewLegacyInputWrapper("Enter text: ")
	defer input.Close()

	line, _, err := input.ReadLine()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("You entered: %s\n", line)
}
