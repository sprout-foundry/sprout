package tools

import (
	"testing"
)

// TestValidateEditInputs_PatternMatching tests that edit operations don't block legitimate content
func TestValidateEditInputs_PatternMatching(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		oldString   string
		newString   string
		shouldError bool
	}{
		{
			name:        "legitimate path in comment",
			filePath:    "test.go",
			oldString:   "// Path: ../src/test.go\n",
			newString:   "// Path: ../src/test.go\n// Updated\n",
			shouldError: false,
		},
		{
			name:        "path in string literal",
			filePath:    "test.go",
			oldString:   `path := "../test"`,
			newString:   `path := "../new_test"`,
			shouldError: false,
		},
		{
			name:        "relative import",
			filePath:    "test.go",
			oldString:   `import "../lib/helper"`,
			newString:   `import "../lib/new_helper"`,
			shouldError: false,
		},
		{
			name:        "windows path in comment",
			filePath:    "test.go",
			oldString:   `// Windows: ..\config\app.ini`,
			newString:   `// Windows: ..\config\new_app.ini`,
			shouldError: false,
		},
		{
			name:        "null bytes should still error",
			filePath:    "test.go",
			oldString:   "test\x00content",
			newString:   "newcontent",
			shouldError: true,
		},
		{
			name:        "empty file path",
			filePath:    "",
			oldString:   "test",
			newString:   "new",
			shouldError: true,
		},
		{
			name:        "empty old string",
			filePath:    "test.go",
			oldString:   "",
			newString:   "new",
			shouldError: true,
		},
		{
			name:        "normal edit without special patterns",
			filePath:    "test.go",
			oldString:   "Hello World",
			newString:   "Hello Go",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEditInputs(tt.filePath, tt.oldString, tt.newString)

			if tt.shouldError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestValidateEditInputs_DocumentationExamples tests real-world examples from documentation
func TestValidateEditInputs_DocumentationExamples(t *testing.T) {
	tests := []struct {
		name      string
		filePath  string
		oldString string
		newString string
	}{
		{
			name:      "markdown relative link",
			filePath:  "README.md",
			oldString: "[Link](../other/page.md)",
			newString: "[Link](../other/new-page.md)",
		},
		{
			name:      "HTML relative path",
			filePath:  "index.html",
			oldString: `<script src="../js/app.js"></script>`,
			newString: `<script src="../js/new-app.js"></script>`,
		},
		{
			name:      "Python relative import",
			filePath:  "main.py",
			oldString: `from ..utils import helper`,
			newString: `from ..utils import new_helper`,
		},
		{
			name:      "C++ include directive",
			filePath:  "main.cpp",
			oldString: `#include "../headers/helper.h"`,
			newString: `#include "../headers/new_helper.h"`,
		},
		{
			name:      "Java package import",
			filePath:  "Main.java",
			oldString: `import ../utils/Helper;`,
			newString: `import ../utils/NewHelper;`,
		},
		{
			name:      "shell script relative path",
			filePath:  "script.sh",
			oldString: `source ../config/env.sh`,
			newString: `source ../config/new-env.sh`,
		},
		{
			name:      "dockerfile context",
			filePath:  "Dockerfile",
			oldString: `COPY ../app /app`,
			newString: `COPY ../new-app /app`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEditInputs(tt.filePath, tt.oldString, tt.newString)
			if err != nil {
				t.Errorf("Legitimate edit blocked: %v\n  filePath: %s\n  oldString: %s\n  newString: %s",
					err, tt.filePath, tt.oldString, tt.newString)
			}
		})
	}
}
