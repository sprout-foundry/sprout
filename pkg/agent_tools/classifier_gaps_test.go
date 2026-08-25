package tools

import (
	"testing"
)

// TestClassifierGaps_Audit covers three verified classification gaps that
// were fixed in SP-124b:
//  1. find with destructive flags (-delete, -exec rm/chmod/chown)
//  2. interpreter command escapes (bash -c, python -c, etc.)
//  3. newline as a chain separator in SplitChainedCommand
func TestClassifierGaps_Audit(t *testing.T) {
	t.Run("DANGEROUS_find_-delete", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "find . -delete",
		})
		if result.Risk != SecurityDangerous {
			t.Errorf("find . -delete: want DANGEROUS, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("DANGEROUS_find_-exec_rm_-rf", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "find . -exec rm -rf {} \\;",
		})
		if result.Risk != SecurityDangerous {
			t.Errorf("find . -exec rm -rf {} \\;: want DANGEROUS, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("DANGEROUS_find_-exec_chmod", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "find /tmp -exec chmod 777 {} \\;",
		})
		if result.Risk != SecurityDangerous {
			t.Errorf("find /tmp -exec chmod 777 {} \\;: want DANGEROUS, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("DANGEROUS_newline_split_rm_-rf", func(t *testing.T) {
		// The newline splits into two subcommands; the rm -rf half should
		// be classified CAUTION (or higher) which elevates the max risk.
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "echo hello\nrm -rf /tmp/x",
		})
		if result.Risk != SecurityCaution {
			t.Errorf("echo hello\\nrm -rf /tmp/x: want CAUTION, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("CAUTION_bash_-c", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "bash -c 'rm -rf /tmp/x'",
		})
		if result.Risk != SecurityCaution {
			t.Errorf("bash -c 'rm -rf /tmp/x': want CAUTION, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("CAUTION_sh_-c", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "sh -c 'rm -rf /tmp/x'",
		})
		if result.Risk != SecurityCaution {
			t.Errorf("sh -c 'rm -rf /tmp/x': want CAUTION, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("CAUTION_python_-c", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "python -c 'import os'",
		})
		if result.Risk != SecurityCaution {
			t.Errorf("python -c 'import os': want CAUTION, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("CAUTION_node_-e", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "node -e 'console.log(1)'",
		})
		if result.Risk != SecurityCaution {
			t.Errorf("node -e 'console.log(1)': want CAUTION, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("CAUTION_perl_-e", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "perl -e 'print 1'",
		})
		if result.Risk != SecurityCaution {
			t.Errorf("perl -e 'print 1': want CAUTION, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	// Regression: safe find commands must NOT regress to non-SAFE
	t.Run("SAFE_find_-name", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "find . -name '*.go'",
		})
		if result.Risk != SecuritySafe {
			t.Errorf("find . -name '*.go': want SAFE, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("SAFE_find_/-tmp", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "find /tmp -type f",
		})
		if result.Risk != SecuritySafe {
			t.Errorf("find /tmp -type f: want SAFE, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	// Regression: script file execution must NOT regress to non-SAFE
	t.Run("SAFE_bash_script.sh", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "bash script.sh",
		})
		if result.Risk != SecuritySafe {
			t.Errorf("bash script.sh: want SAFE, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("SAFE_python_script.py", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "python script.py",
		})
		if result.Risk != SecuritySafe {
			t.Errorf("python script.py: want SAFE, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("SAFE_node_server.js", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "node server.js",
		})
		if result.Risk != SecuritySafe {
			t.Errorf("node server.js: want SAFE, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	t.Run("SAFE_echo", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "echo hello",
		})
		if result.Risk != SecuritySafe {
			t.Errorf("echo hello: want SAFE, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	// A quoted newline must NOT split the command — it is data, not a
	// separator. The whole string is one safe echo command.
	t.Run("SAFE_quoted_newline_no_split", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": "echo \"line1\nline2\"",
		})
		if result.Risk != SecuritySafe {
			t.Errorf("echo with quoted newline: want SAFE, got %s (%s)", result.Risk, result.Reasoning)
		}
	})

	// find -exec with an unrelated destructive-looking token appearing
	// BEFORE -exec must not false-positive. The "rm" here is part of a
	// -name pattern, not the inner -exec command, so this is a read-only
	// find (exec echo).
	t.Run("SAFE_find_exec_echo_with_rm_in_name_pattern", func(t *testing.T) {
		result := ClassifyToolCall("shell_command", map[string]interface{}{
			"command": `find . -name "*rm*" -exec echo {} \;`,
		})
		if result.Risk == SecurityDangerous {
			t.Errorf("find -exec echo with rm only in -name pattern: want not-DANGEROUS, got %s (%s)", result.Risk, result.Reasoning)
		}
	})
}
