package training

import (
	"os"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/secretdetect"
)

// PIIRedactionConfig controls what personally identifiable information is
// scrubbed from training data exports.
type PIIRedactionConfig struct {
	// HomeDir is the user's home directory to redact (e.g. /Users/alan).
	// When set, all occurrences are replaced with the placeholder.
	HomeDir string

	// Username is the OS username to redact.
	Username string

	// Hostname is the machine hostname to redact.
	Hostname string
}

// DefaultPIIConfig builds a PII config from the current environment.
func DefaultPIIConfig() PIIRedactionConfig {
	cfg := PIIRedactionConfig{}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cfg.HomeDir = home
	}

	// Derive username from home dir (works on macOS and Linux).
	if cfg.HomeDir != "" {
		parts := strings.Split(cfg.HomeDir, string(os.PathSeparator))
		if len(parts) > 0 {
			cfg.Username = parts[len(parts)-1]
		}
	}

	if host, err := os.Hostname(); err == nil && host != "" {
		cfg.Hostname = host
	}

	return cfg
}

// homeDirRe matches absolute paths starting with a home directory prefix.
var homeDirRe = regexp.MustCompile(``) // unused, kept for API compat

// usernameRe matches standalone username references.
var usernameRe *regexp.Regexp

// emailRe matches email addresses.
var emailRe = regexp.MustCompile(`[\w.+-]+@[\w.-]+\.\w+`)

// anyHomeDirRe matches any home directory path on macOS or Linux.
// Captures /Users/<name> or /home/<name> for redaction.
var anyHomeDirRe = regexp.MustCompile(`(?:/Users|/home)/[\w.-]+`)

// anyHomeDirUnderscoreRe matches underscore-sanitized home directory paths
// found in .sprout/changes/ filenames (e.g. _home_aprice_...).
var anyHomeDirUnderscoreRe = regexp.MustCompile(`(?:_Users_|_home_)[\w.-]+?_`)

// compilePIIRegexps builds regexps for the given config.
// before redaction. Returns clean patterns that won't panic.
func compilePIIRegexps(cfg PIIRedactionConfig) {
	if cfg.HomeDir != "" {
		// Replace home dir as a literal string (handles paths robustly).
		// No regex needed — strings.ReplaceAll is sufficient and safe.
	}
	if cfg.Username != "" {
		// Match the username as a standalone word, not as a substring.
		// Avoids false positives like "alan" in "balance".
		escaped := regexp.QuoteMeta(cfg.Username)
		usernameRe = regexp.MustCompile(`\b` + escaped + `\b`)
	}
}

// redactPII replaces personally identifiable information in the given string.
// It uses DefaultPIIConfig if the config is empty.
func redactPII(s string, cfg PIIRedactionConfig) string {
	if cfg.HomeDir == "" && cfg.Username == "" && cfg.Hostname == "" {
		cfg = DefaultPIIConfig()
	}

	compilePIIRegexps(cfg)

	// Replace home directory paths first (most specific).
	if homeDirRe != nil {
		s = homeDirRe.ReplaceAllString(s, "$HOME")
	}

	// Replace any home directory paths (e.g. /home/aprice, /Users/alanp)
	// so data from remote machines is redacted even when the local username
	// differs. Extract remote usernames first (before replacing paths)
	// so they can also be redacted as standalone words.
	for _, path := range anyHomeDirRe.FindAllString(s, -1) {
		parts := strings.Split(path, string(os.PathSeparator))
		if len(parts) < 3 {
			continue
		}
		remoteUser := parts[2] // username is always 3rd component
		if remoteUser != "" && remoteUser != cfg.Username {
			escaped := regexp.QuoteMeta(remoteUser)
			re := regexp.MustCompile(`\b` + escaped + `\b`)
			s = re.ReplaceAllString(s, "$USER")
		}
	}
	s = anyHomeDirRe.ReplaceAllString(s, "$HOME")
	// Also replace underscore-sanitized home dir paths from .sprout/changes/
	s = anyHomeDirUnderscoreRe.ReplaceAllString(s, "$HOME")

	// Replace the bare home dir (for cases without a trailing path).
	if cfg.HomeDir != "" {
		s = strings.ReplaceAll(s, cfg.HomeDir, "$HOME")
		// Also replace underscore-sanitized paths (slashes → underscores)
		// used in .sprout/changes/ filenames.
		underscored := strings.ReplaceAll(cfg.HomeDir, string(os.PathSeparator), "_")
		s = strings.ReplaceAll(s, underscored, "$HOME")
	}

	// Replace username as a standalone word.
	if usernameRe != nil {
		s = usernameRe.ReplaceAllString(s, "$USER")
	}

	// Replace hostname.
	if cfg.Hostname != "" {
		s = strings.ReplaceAll(s, cfg.Hostname, "$HOST")
	}

	// Redact email addresses.
	s = emailRe.ReplaceAllString(s, "$EMAIL")

	// Redact git author tags like "(by alanprice)" or "Author: alanprice".
	// Matches the username as a prefix of longer words (alanprice, alan228)
	// when preceded by common git/author contexts.
	if cfg.Username != "" {
		escaped := regexp.QuoteMeta(cfg.Username)
		authorRe := regexp.MustCompile(`(?i)(by |author:*\s*)` + escaped + `\w*`)
		s = authorRe.ReplaceAllString(s, "${1}$USER")
		// Also redact VCS mentions: @username in chat contexts
		atRe := regexp.MustCompile(`@` + escaped + `\w*\b`)
		s = atRe.ReplaceAllString(s, "@$USER")
		// Redact github.com/username in module paths
		vcsRe := regexp.MustCompile(`(github\.com|gitlab\.com|bitbucket\.org)/` + escaped + `\w*`)
		s = vcsRe.ReplaceAllString(s, "${1}/$USER")
		// Redact bare username-prefixed words that look like git authors.
		// Matches lines where the username is a prefix of a longer word
		// (eg. "alanprice" from git log --format='%an').
		bareAuthorRe := regexp.MustCompile(`(?m)^` + escaped + `\w+$`)
		s = bareAuthorRe.ReplaceAllString(s, "$USER")
	}

	return s
}

// RedactContent applies both PII and secret redaction to a content string.
// This is the single entry point for all content sanitization in exports.
// Call SetRemoteUsernames before calling this to enable redaction of
// usernames from remote machines discovered during a pre-scan.
func RedactContent(s string) string {
	s = redactPII(s, PIIRedactionConfig{})
	if len(remoteUsernamesForRedaction) > 0 {
		s = redactAdditionalUsernames(s, remoteUsernamesForRedaction)
	}
	s = redactSecrets(s)
	return s
}

// remoteUsernamesForRedaction holds additional usernames to redact
// (set during export pre-scan). Thread-safe only because exports
// are single-threaded.
var remoteUsernamesForRedaction []string

// SetRemoteUsernames sets the additional usernames that RedactContent
// will redact as $USER in addition to the local username.
func SetRemoteUsernames(users []string) {
	remoteUsernamesForRedaction = append([]string{}, users...)
}

// mergeUsernames combines two username slices, deduplicating.
func mergeUsernames(a, b []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, u := range a {
		if !seen[u] && u != "" {
			seen[u] = true
			result = append(result, u)
		}
	}
	for _, u := range b {
		if !seen[u] && u != "" {
			seen[u] = true
			result = append(result, u)
		}
	}
	return result
}

// remoteUsernameRe captures usernames from any home directory path.
// Used by CollectRemoteUsernames for pre-scanning before redaction.
var remoteUsernameRe = regexp.MustCompile(`(?:/Users/|/home/)([\w.-]+)`)

// cmdOutputUsernamePatterns matches usernames that appear in common
// command output patterns without a full home directory path.
var cmdOutputUsernamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[sudo\] password for ([\w.-]+):`),
	regexp.MustCompile(`local-([\w.-]+)\.(?:dev|com|local|internal)`),
	regexp.MustCompile(`(?:api|dev)-([\w.-]+)\.(?:dev|com|local)`),
}

// CollectRemoteUsernames scans strings for home directory paths and returns
// any usernames that differ from the local username. This enables redaction
// of usernames from remote machines where /home/<user> doesn't match the
// local /Users/<name> or /home/<name> pattern.
func CollectRemoteUsernames(strings []string) []string {
	cfg := DefaultPIIConfig()
	seen := make(map[string]bool)
	var result []string
	for _, s := range strings {
		for _, match := range remoteUsernameRe.FindAllStringSubmatch(s, -1) {
			user := match[1]
			if user == "" || user == cfg.Username || seen[user] {
				continue
			}
			seen[user] = true
			result = append(result, user)
		}
	}
	// Also scan for underscored variants (_Users_alanp → "alanp")
	underscoredRe := regexp.MustCompile(`(?:_Users_|_home_)([\w.-]+?)(?:_|$)`)
	for _, s := range strings {
		for _, match := range underscoredRe.FindAllStringSubmatch(s, -1) {
			user := match[1]
			if user == "" || user == cfg.Username || seen[user] {
				continue
			}
			seen[user] = true
			result = append(result, user)
		}
	}
	// Also scan for usernames in command output patterns (sudo prompts,
	// custom dev domains) that appear without a full home dir path.
	for _, s := range strings {
		for _, pat := range cmdOutputUsernamePatterns {
			for _, match := range pat.FindAllStringSubmatch(s, -1) {
				user := match[1]
				if user == "" || user == cfg.Username || seen[user] {
					continue
				}
				seen[user] = true
				result = append(result, user)
			}
		}
	}
	return result
}

// scanWorkingDirsForUsernames extracts usernames from working directory
// paths. This catches remote usernames even when no individual message
// contains a full home directory path.
func scanWorkingDirsForUsernames(workingDirs []string) []string {
	cfg := DefaultPIIConfig()
	seen := make(map[string]bool)
	var result []string
	for _, wd := range workingDirs {
		for _, match := range remoteUsernameRe.FindAllStringSubmatch(wd, -1) {
			user := match[1]
			if user == "" || user == cfg.Username || seen[user] {
				continue
			}
			seen[user] = true
			result = append(result, user)
		}
	}
	return result
}
// These are typically usernames from remote machines discovered during
// a pre-scan with CollectRemoteUsernames.
func redactAdditionalUsernames(s string, users []string) string {
	for _, user := range users {
		if user == "" {
			continue
		}
		escaped := regexp.QuoteMeta(user)
		// Standard word-boundary match (catches most cases).
		re := regexp.MustCompile(`\b` + escaped + `\b`)
		s = re.ReplaceAllString(s, "$USER")
		// Also catch usernames preceded by literal \n (backslash-n)
		// which occurs when command output is double-escaped in
		// session storage. Without this, \naprice has no word boundary
		// between the 'n' and 'a' characters.
		re2 := regexp.MustCompile(`\\n` + escaped + `\b`)
		s = re2.ReplaceAllString(s, "\\n$USER")
	}
	return s
}

// redactSecrets wraps secretdetect.RedactOpaque for testability.
func redactSecrets(s string) string {
	return secretdetect.RedactOpaque(s)
}
