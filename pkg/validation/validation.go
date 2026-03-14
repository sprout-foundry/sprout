// Package validation provides syntax validation using gofmt/goimports
package validation

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/events"
)

// Validator provides syntax and import validation
type Validator struct {
	eventBus *events.EventBus
}

// Diagnostic represents a validation issue
type Diagnostic struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Source    string `json:"source"`
}

// ValidationResult holds validation results
type ValidationResult struct {
	Path        string      `json:"path"`
	Valid       bool        `json:"valid"`
	Errors      []Diagnostic `json:"errors,omitempty"`
	Warnings    []Diagnostic `json:"warnings,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// NewValidator creates a new Validator
func NewValidator(eventBus *events.EventBus) *Validator {
	return &Validator{
		eventBus: eventBus,
	}
}

// ValidateSyntax checks if Go code has valid syntax using gofmt
func (v *Validator) ValidateSyntax(ctx context.Context, path, content string) error {
	cmd := exec.CommandContext(ctx, "gofmt", "-e", "-l", "-")
	cmd.Dir = "."

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(content)

	err := cmd.Run()
	if err == nil {
		return nil // No syntax errors
	}

	output := stderr.String()
	if output == "" {
		output = stdout.String()
	}

	return fmt.Errorf("syntax error: %s", strings.TrimSpace(output))
}

// RunValidation performs a full validation check
func (v *Validator) RunValidation(ctx context.Context, path, content string) ValidationResult {
	result := ValidationResult{
		Path: path,
	}

	// Always check syntax first
	if err := v.ValidateSyntax(ctx, path, content); err != nil {
		result.Errors = append(result.Errors, Diagnostic{
			Path:      path,
			Line:      1,
			Column:    1,
			Severity:  "error",
			Message:   err.Error(),
			Source:    "gofmt",
		})
		return result
	}

	result.Valid = true

	// Check imports if present
	if strings.Contains(content, "import") {
		importResult := v.ValidateImports(ctx, path, content)
		if len(importResult) > 0 {
			result.Warnings = importResult
		}
	}

	result.Diagnostics = append(result.Diagnostics, result.Errors...)
	result.Diagnostics = append(result.Diagnostics, result.Warnings...)

	// Publish to event bus
	if v.eventBus != nil {
		v.eventBus.Publish(events.EventTypeValidation, map[string]interface{}{
			"file_path":   path,
			"diagnostics": toDiagnosticsMap(result.Diagnostics),
		})
	}

	return result
}

// ValidateImports checks for import issues using goimports
func (v *Validator) ValidateImports(ctx context.Context, path, content string) []Diagnostic {
	cmd := exec.CommandContext(ctx, "goimports", "-l", "-")
	cmd.Dir = "."

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(content)

	err := cmd.Run()
	if err != nil {
		return nil // Skip import validation if goimports fails
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return nil
	}

	// goimports -l lists files with import issues
	var diagnostics []Diagnostic
	for _, line := range strings.Split(output, "\n") {
		if line != "" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:      path,
				Line:      1,
				Column:    1,
				Severity:  "warning",
				Message:   "import issue detected",
				Source:    "goimports",
			})
		}
	}

	return diagnostics
}

// toDiagnosticsMap converts diagnostics to map format for events
func toDiagnosticsMap(diagnostics []Diagnostic) []map[string]interface{} {
	result := make([]map[string]interface{}, len(diagnostics))
	for i, d := range diagnostics {
		result[i] = map[string]interface{}{
			"path":     d.Path,
			"line":     d.Line,
			"column":   d.Column,
			"severity": d.Severity,
			"message":  d.Message,
			"source":   d.Source,
		}
	}
	return result
}

// RunAsyncValidation performs async validation without blocking
func (v *Validator) RunAsyncValidation(ctx context.Context, path, content string) {
	go func() {
		// Run validation in background - fire-and-forget
		_ = v.RunValidation(ctx, path, content)
	}()
}
