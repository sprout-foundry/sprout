package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectValidationContext contains project-specific validation instructions
type ProjectValidationContext struct {
	BuildCommands     []string `json:"build_commands"`
	LintCommands      []string `json:"lint_commands"`
	TypeCheckCommands []string `json:"typecheck_commands"`
	TestCommands      []string `json:"test_commands"`
	PreCommitChecks   []string `json:"precommit_checks"`
}

// generateProjectValidationContext analyzes the project and generates validation instructions
func generateProjectValidationContext() (string, error) {
	// Check if we already have cached validation context
	contextPath := filepath.Join(".ledit", "validation_context.md")
	if content, err := os.ReadFile(contextPath); err == nil {
		return string(content), nil
	}

	// Analyze the workspace to understand the project
	ctx, err := analyzeProjectForValidation()
	if err != nil {
		return "", fmt.Errorf("failed to analyze project: %w", err)
	}

	// Generate validation instructions based on project type
	instructions := buildValidationInstructions(ctx)

	// Save to cache
	os.MkdirAll(".ledit", 0755)
	os.WriteFile(contextPath, []byte(instructions), 0644)

	return instructions, nil
}

// RegenerateProjectValidationContext forces regeneration of validation context
func RegenerateProjectValidationContext() error {
	// Remove cached file
	contextPath := filepath.Join(".ledit", "validation_context.md")
	os.Remove(contextPath)

	// Regenerate
	_, err := generateProjectValidationContext()
	return err
}

// analyzeProjectForValidation performs lightweight project analysis
func analyzeProjectForValidation() (*ProjectValidationContext, error) {
	ctx := &ProjectValidationContext{
		BuildCommands:     []string{},
		LintCommands:      []string{},
		TypeCheckCommands: []string{},
		TestCommands:      []string{},
		PreCommitChecks:   []string{},
	}

	// Check for package.json (Node.js projects)
	if _, err := os.Stat("package.json"); err == nil {
		if data, err := os.ReadFile("package.json"); err == nil {
			var pkg map[string]interface{}
			if err := json.Unmarshal(data, &pkg); err == nil {
				if scripts, ok := pkg["scripts"].(map[string]interface{}); ok {
					// Check for common script names
					checkScript := func(names []string, target *[]string) {
						for _, name := range names {
							if _, exists := scripts[name]; exists {
								*target = append(*target, fmt.Sprintf("npm run %s", name))
								break
							}
						}
					}

					checkScript([]string{"build", "compile", "dist"}, &ctx.BuildCommands)
					checkScript([]string{"lint", "eslint", "tslint"}, &ctx.LintCommands)
					checkScript([]string{"typecheck", "type-check", "tsc", "types"}, &ctx.TypeCheckCommands)
					checkScript([]string{"test", "test:unit", "test:all"}, &ctx.TestCommands)
					checkScript([]string{"precommit", "pre-commit", "verify"}, &ctx.PreCommitChecks)
				}
			}
		}
	}

	// Check for go.mod (Go projects)
	if _, err := os.Stat("go.mod"); err == nil {
		ctx.BuildCommands = append(ctx.BuildCommands, "go build ./...")
		ctx.TestCommands = append(ctx.TestCommands, "go test ./...")

		// Check if golangci-lint config exists
		if _, err := os.Stat(".golangci.yml"); err == nil {
			ctx.LintCommands = append(ctx.LintCommands, "golangci-lint run")
		} else {
			ctx.LintCommands = append(ctx.LintCommands, "go vet ./...")
		}
	}

	// Check for Cargo.toml (Rust projects)
	if _, err := os.Stat("Cargo.toml"); err == nil {
		ctx.BuildCommands = append(ctx.BuildCommands, "cargo build")
		ctx.TestCommands = append(ctx.TestCommands, "cargo test")
		ctx.LintCommands = append(ctx.LintCommands, "cargo clippy")
		ctx.TypeCheckCommands = append(ctx.TypeCheckCommands, "cargo check")
	}

	// Check for requirements.txt or pyproject.toml (Python projects)
	if _, err := os.Stat("requirements.txt"); err == nil || func() bool {
		_, err := os.Stat("pyproject.toml")
		return err == nil
	}() {
		ctx.TestCommands = append(ctx.TestCommands, "pytest")

		// Check for ruff
		if _, err := os.Stat("ruff.toml"); err == nil || func() bool {
			_, err := os.Stat(".ruff.toml")
			return err == nil
		}() {
			ctx.LintCommands = append(ctx.LintCommands, "ruff check")
		}

		// Check for mypy
		if _, err := os.Stat("mypy.ini"); err == nil || func() bool {
			_, err := os.Stat(".mypy.ini")
			return err == nil
		}() {
			ctx.TypeCheckCommands = append(ctx.TypeCheckCommands, "mypy .")
		}
	}

	// Check for Makefile
	if _, err := os.Stat("Makefile"); err == nil {
		// Try to parse common targets
		if data, err := os.ReadFile("Makefile"); err == nil {
			content := string(data)
			checkTarget := func(names []string, target *[]string) {
				for _, name := range names {
					if strings.Contains(content, name+":") {
						*target = append(*target, fmt.Sprintf("make %s", name))
						break
					}
				}
			}

			checkTarget([]string{"build", "all"}, &ctx.BuildCommands)
			checkTarget([]string{"lint", "check"}, &ctx.LintCommands)
			checkTarget([]string{"test", "test-all"}, &ctx.TestCommands)
		}
	}

	// Check for Java projects (pom.xml or build.gradle)
	if _, err := os.Stat("pom.xml"); err == nil {
		ctx.BuildCommands = append(ctx.BuildCommands, "mvn clean compile")
		ctx.TestCommands = append(ctx.TestCommands, "mvn test")
		ctx.LintCommands = append(ctx.LintCommands, "mvn checkstyle:check")
	} else if _, err := os.Stat("build.gradle"); err == nil || func() bool {
		_, err := os.Stat("build.gradle.kts")
		return err == nil
	}() {
		ctx.BuildCommands = append(ctx.BuildCommands, "gradle build")
		ctx.TestCommands = append(ctx.TestCommands, "gradle test")
		ctx.LintCommands = append(ctx.LintCommands, "gradle check")
	}

	// Check for Ruby projects (Gemfile)
	if _, err := os.Stat("Gemfile"); err == nil {
		ctx.TestCommands = append(ctx.TestCommands, "bundle exec rspec")

		// Check for Rubocop
		if _, err := os.Stat(".rubocop.yml"); err == nil {
			ctx.LintCommands = append(ctx.LintCommands, "bundle exec rubocop")
		}
	}

	// Check for PHP projects (composer.json)
	if _, err := os.Stat("composer.json"); err == nil {
		if data, err := os.ReadFile("composer.json"); err == nil {
			var composer map[string]interface{}
			if err := json.Unmarshal(data, &composer); err == nil {
				// Check for PHPUnit
				if devDeps, ok := composer["require-dev"].(map[string]interface{}); ok {
					if _, hasPhpunit := devDeps["phpunit/phpunit"]; hasPhpunit {
						ctx.TestCommands = append(ctx.TestCommands, "vendor/bin/phpunit")
					}
				}
			}
		}

		// Check for PHP CS Fixer
		if _, err := os.Stat(".php-cs-fixer.php"); err == nil {
			ctx.LintCommands = append(ctx.LintCommands, "vendor/bin/php-cs-fixer fix --dry-run --diff")
		}
	}

	// Check for C/C++ projects (CMakeLists.txt)
	if _, err := os.Stat("CMakeLists.txt"); err == nil {
		ctx.BuildCommands = append(ctx.BuildCommands, "cmake --build build")
		ctx.TestCommands = append(ctx.TestCommands, "ctest --test-dir build")
	}

	// Check for .NET projects (*.csproj or *.sln)
	if files, err := filepath.Glob("*.csproj"); err == nil && len(files) > 0 {
		ctx.BuildCommands = append(ctx.BuildCommands, "dotnet build")
		ctx.TestCommands = append(ctx.TestCommands, "dotnet test")
	} else if files, err := filepath.Glob("*.sln"); err == nil && len(files) > 0 {
		ctx.BuildCommands = append(ctx.BuildCommands, "dotnet build")
		ctx.TestCommands = append(ctx.TestCommands, "dotnet test")
	}

	// Set up pre-commit checks based on what we found
	if len(ctx.LintCommands) > 0 || len(ctx.TypeCheckCommands) > 0 || len(ctx.TestCommands) > 0 {
		ctx.PreCommitChecks = []string{}
		ctx.PreCommitChecks = append(ctx.PreCommitChecks, ctx.BuildCommands...)
		ctx.PreCommitChecks = append(ctx.PreCommitChecks, ctx.LintCommands...)
		ctx.PreCommitChecks = append(ctx.PreCommitChecks, ctx.TypeCheckCommands...)
	}

	return ctx, nil
}

// buildValidationInstructions creates markdown instructions for the agent
func buildValidationInstructions(ctx *ProjectValidationContext) string {
	var sb strings.Builder

	sb.WriteString("PROJECT VALIDATION REQUIREMENTS:\n\n")
	sb.WriteString("When you complete any code changes in this project, you MUST run the following validation steps:\n\n")

	stepNum := 1

	// Build commands
	if len(ctx.BuildCommands) > 0 {
		sb.WriteString(fmt.Sprintf("%d. **Build the project**:\n", stepNum))
		for _, cmd := range ctx.BuildCommands {
			sb.WriteString(fmt.Sprintf("   - `%s`\n", cmd))
		}
		sb.WriteString("   - Fix any build errors before proceeding\n\n")
		stepNum++
	}

	// Lint commands
	if len(ctx.LintCommands) > 0 {
		sb.WriteString(fmt.Sprintf("%d. **Run linting**:\n", stepNum))
		for _, cmd := range ctx.LintCommands {
			sb.WriteString(fmt.Sprintf("   - `%s`\n", cmd))
		}
		sb.WriteString("   - Fix all linting issues\n\n")
		stepNum++
	}

	// Type check commands
	if len(ctx.TypeCheckCommands) > 0 {
		sb.WriteString(fmt.Sprintf("%d. **Run type checking**:\n", stepNum))
		for _, cmd := range ctx.TypeCheckCommands {
			sb.WriteString(fmt.Sprintf("   - `%s`\n", cmd))
		}
		sb.WriteString("   - Resolve all type errors\n\n")
		stepNum++
	}

	// Test commands
	if len(ctx.TestCommands) > 0 {
		sb.WriteString(fmt.Sprintf("%d. **Run tests**:\n", stepNum))
		for _, cmd := range ctx.TestCommands {
			sb.WriteString(fmt.Sprintf("   - `%s`\n", cmd))
		}
		sb.WriteString("   - Ensure all tests pass\n")
		sb.WriteString("   - If tests fail due to your changes, fix the implementation (not the tests)\n\n")
		stepNum++
	}

	// Summary
	sb.WriteString("**IMPORTANT**: All validation must pass before considering the task complete.\n")
	sb.WriteString("If any validation fails, fix the issues and re-run all validation steps.\n")

	// Quick validation command if available
	if len(ctx.PreCommitChecks) > 0 {
		sb.WriteString("\n**Quick validation** (run all checks):\n")
		sb.WriteString("```bash\n")
		for _, cmd := range ctx.PreCommitChecks {
			sb.WriteString(cmd + " && \\\n")
		}
		// Remove the last " && \\\n"
		content := sb.String()
		content = strings.TrimSuffix(content, " && \\\n")
		sb.Reset()
		sb.WriteString(content)
		sb.WriteString("\n```\n")
	}

	return sb.String()
}
