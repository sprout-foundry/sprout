package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ValidateBuild runs project-specific build validation commands
func ValidateBuild() (string, error) {
	// Check what type of project this is
	projectType, buildCommands := detectProjectType()

	if len(buildCommands) == 0 {
		return "No build validation commands found for this project type", nil
	}

	var results []string
	var hasErrors bool

	results = append(results, fmt.Sprintf("🔧 Running build validation for %s project...", projectType))

	for _, cmd := range buildCommands {
		results = append(results, fmt.Sprintf("\n📋 Executing: %s", cmd))

		// Split command into parts
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			continue
		}

		// Execute the command
		command := exec.Command(parts[0], parts[1:]...)
		output, err := command.CombinedOutput()

		if err != nil {
			hasErrors = true
			results = append(results, fmt.Sprintf("❌ Build failed: %v", err))
			results = append(results, fmt.Sprintf("Output:\n%s", string(output)))
		} else {
			results = append(results, fmt.Sprintf("✅ Build successful"))
			if strings.TrimSpace(string(output)) != "" {
				results = append(results, fmt.Sprintf("Output:\n%s", string(output)))
			}
		}
	}

	if hasErrors {
		return strings.Join(results, "\n"), fmt.Errorf("build validation failed")
	}

	return strings.Join(results, "\n"), nil
}

// detectProjectType detects the project type and returns appropriate build commands
func detectProjectType() (string, []string) {
	// Check for Go project
	if _, err := os.Stat("go.mod"); err == nil {
		return "Go", []string{"go build ./...", "go vet ./..."}
	}

	// Check for Node.js project
	if _, err := os.Stat("package.json"); err == nil {
		// Try to read package.json to find build script
		if data, err := os.ReadFile("package.json"); err == nil {
			var pkg map[string]interface{}
			if err := json.Unmarshal(data, &pkg); err == nil {
				if scripts, ok := pkg["scripts"].(map[string]interface{}); ok {
					if _, ok := scripts["build"].(string); ok {
						return "Node.js", []string{"npm run build", "npm test"}
					}
				}
			}
		}
		return "Node.js", []string{"npm test"}
	}

	// Check for Python project
	if _, err := os.Stat("requirements.txt"); err == nil {
		return "Python", []string{"python -m pytest"}
	}
	if _, err := os.Stat("pyproject.toml"); err == nil {
		return "Python", []string{"python -m pytest"}
	}

	// Check for Rust project
	if _, err := os.Stat("Cargo.toml"); err == nil {
		return "Rust", []string{"cargo build", "cargo test"}
	}

	// Check for Java project
	if _, err := os.Stat("pom.xml"); err == nil {
		return "Java", []string{"mvn compile", "mvn test"}
	}
	if _, err := os.Stat("build.gradle"); err == nil {
		return "Java", []string{"gradle build", "gradle test"}
	}

	// Default: no specific project type detected
	return "Unknown", []string{}
}
