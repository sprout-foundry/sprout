package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripQuotedSections(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no quotes", "echo hello", "echo hello"},
		{"single quoted", `echo 'hello world'`, `echo '           '`},
		{"double quoted", `echo "hello world"`, `echo "           "`},
		{"pipe in quotes", `grep 'rgba|gradient|shadow'`, `grep '                    '`},
		{"mixed", `echo "hello" 'world'`, `echo "     " '     '`},
		{"empty", "", ""},
		{"unclosed quote", `echo "hello`, `echo "     `},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripQuotedSections(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaxRisk(t *testing.T) {
	assert.Equal(t, SecuritySafe, maxRisk([]SecurityRisk{SecuritySafe}))
	assert.Equal(t, SecurityDangerous, maxRisk([]SecurityRisk{SecuritySafe, SecurityCaution, SecurityDangerous}))
	assert.Equal(t, SecurityCaution, maxRisk([]SecurityRisk{SecuritySafe, SecurityCaution}))
	assert.Equal(t, SecuritySafe, maxRisk([]SecurityRisk{}))
	assert.Equal(t, SecuritySafe, maxRisk(nil))
}

func TestExtractCommandSubstitutions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"none", "echo hello", nil},
		{"simple", "echo $(date)", []string{"date"}},
		{"nested", "echo $(hostname)-$(date +%s)", []string{"hostname", "date +%s"}},
		{"empty substitution", "echo $()", nil},
		{"unclosed", "echo $(date", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCommandSubstitutions(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsRedirection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"none", "echo hello", false},
		{"redirect out", "echo hello > file.txt", true},
		{"append", "echo hello >> file.txt", true},
		{"redirect in single", "grep foo < input.txt", false},
		{"fd dup", "make 2>&1", false},
		{"in redirect pattern", "grep pattern file", false},
		{"quoted redirect", "echo '> not redirect'", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsRedirection(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractRedirectionTarget(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedPath string
		expectedOK   bool
	}{
		{"redirect to file", "echo hi > out.txt", "out.txt", true},
		{"append to file", "echo hi >> log.txt", "log.txt", true},
		{"redirect to /tmp", "echo hi > /tmp/test.txt", "/tmp/test.txt", true},
		{"no redirect", "echo hi", "", false},
		{"fd dup", "make 2>&1", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := extractRedirectionTarget(strings.ToLower(tt.input))
			assert.Equal(t, tt.expectedOK, ok)
			if ok {
				assert.Equal(t, tt.expectedPath, path)
			}
		})
	}
}

func TestIsBenignRedirection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"dev null", "echo hi > /dev/null", true},
		{"dev null append", "echo hi >> /dev/null", true},
		{"dev null no space", "echo hi>/dev/null", true},
		{"tmp file", "echo hi > /tmp/test.txt", true},
		{"system dir", "echo hi > /etc/passwd", false},
		{"no redirect", "echo hi", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBenignRedirection(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasRedirectionTraversalToSystemDir(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"normal tmp", "echo hi > /tmp/test.txt", false},
		{"traverse to etc", "echo hi > /tmp/../etc/passwd", true},
		{"traverse to usr", "echo hi > /tmp/../usr/bin/evil", true},
		{"direct /etc", "echo hi > /etc/hosts", true},
		{"no traversal", "echo hi > output.txt", false},
		{"no redirect", "echo hi", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasRedirectionTraversalToSystemDir(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetShellCommandReasoning(t *testing.T) {
	assert.Contains(t, getShellCommandReasoning("ls", SecuritySafe), "safe")
	assert.Contains(t, getShellCommandReasoning("rm", SecurityCaution), "Review")
	assert.Contains(t, getShellCommandReasoning("rm -rf /", SecurityDangerous), "destroy")
}

func TestGetShellCommandRiskType(t *testing.T) {
	// Not dangerous - returns empty
	assert.Equal(t, "", getShellCommandRiskType("ls", SecuritySafe, false))
	assert.Equal(t, "", getShellCommandRiskType("ls", SecurityCaution, false))

	// Dangerous operations
	assert.Equal(t, "mass_deletion", getShellCommandRiskType("rm -rf .", SecurityDangerous, false))
	assert.Equal(t, "mass_deletion", getShellCommandRiskType("rm -rf /", SecurityDangerous, false))
	assert.Equal(t, "privilege_escalation", getShellCommandRiskType("sudo rm -rf /", SecurityDangerous, false))
	assert.Equal(t, "directory_deletion", getShellCommandRiskType("rm -rf mydir", SecurityDangerous, false))
	assert.Equal(t, "destructive_git_operation", getShellCommandRiskType("git push --force origin main", SecurityDangerous, false))
	assert.Equal(t, "destructive_git_operation", getShellCommandRiskType("git branch -D feature", SecurityDangerous, false))
	assert.Equal(t, "source_code_destruction", getShellCommandRiskType("rm -rf src/", SecurityDangerous, false))
	assert.Equal(t, "insecure_permissions", getShellCommandRiskType("chmod 777 /tmp/dir", SecurityDangerous, false))
	assert.Equal(t, "disk_destruction", getShellCommandRiskType("mkfs.ext4 /dev/sda", SecurityDangerous, false))
	assert.Equal(t, "system_instability", getShellCommandRiskType("killall -9 chrome", SecurityDangerous, false))
}
