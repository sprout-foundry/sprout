package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/injection"
)

// SP-169 phasing step 3: frame third-party code when it is read.
//
// Which content is third-party is a user decision, not something to infer — a
// cloned repository may be the user's own work, and mislabelling our own code
// as untrusted makes the model distrust its own output. So this file decides
// the class for one read and, in the ambiguous case only, asks once and
// remembers the answer through the existing config store
// (Config.TrustedContentRoots, keyed by absolute workspace root).

// The two answers the trust question offers. "third-party" is the default and
// the value an unrecognised answer falls back to.
const trustThirdPartyOption = "third-party"
const trustOursOption = "ours"

// resolveReadTrust returns how content at absPath should be treated.
//
// Rules, in order:
//
//  1. A known dependency marker, or a decision recorded for a prefix of the
//     path, decides without asking (ClassifyPath).
//  2. A path outside the workspace root, or under a nested repository (a
//     clone inside our tree), is "pulled in" code whose status is ambiguous:
//     consult a recorded decision, else ask once and persist the answer.
//     With no interactive channel (autonomous run) the conservative default
//     applies — framed, since framing labels content as data and withholds
//     nothing — and nothing is persisted.
//  3. Otherwise the path is ours.
func resolveReadTrust(env ToolEnv, absPath string) injection.TrustClass {
	// Resolve symlinks before classifying: the read follows the link, so the
	// bytes come from the target. Classifying the link path would call a
	// symlink that points into a vendored tree "ours" and deliver its target
	// unframed. Best-effort — an unresolvable path keeps its literal spelling
	// and is classified as written.
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}

	workspaceRoot := env.WorkspaceRoot
	// Paths are compared in one canonical space: the read follows symlinks, so
	// both the target and the root are resolved before they are related to
	// each other. On macOS this is also what makes a temp dir under /var
	// comparable with its /private/var spelling. The unresolved root stays the
	// key for stored decisions, so the store keeps the spelling the CLI wrote.
	classRoot := workspaceRoot
	if resolved, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		classRoot = resolved
	}
	relPath := relToWorkspace(classRoot, absPath)

	// Rule 1: markers and recorded decisions. A marker (node_modules, vendor,
	// .git, ...) is structural and outranks anything persisted.
	if class := injection.ClassifyPath(relPath, trustDecisions(env, workspaceRoot)); class == injection.TrustThirdParty {
		return injection.TrustThirdParty
	}

	// Rule 2: pulled-in code — outside the workspace, or inside a nested repo.
	if !isOutsideWorkspace(classRoot, absPath) && !isNestedRepo(classRoot, absPath) {
		return injection.TrustFirstParty
	}

	// The ambiguous case. Ask about the directory the content lives in, not
	// the file: the answer is about a tree, and keying it on the file would
	// ask again for the next file in the same clone.
	askRel := trustAskRoot(classRoot, absPath)
	if class := injection.ClassifyPath(askRel, trustDecisions(env, workspaceRoot)); class != injection.TrustUnknown {
		return class
	}

	if env.AskUser == nil {
		// No surface to ask. Frame it: the label is not a restriction, so
		// the conservative choice costs nothing and cannot block work.
		return injection.TrustThirdParty
	}

	answer, err := env.AskUser.Ask(context.Background(), AskUserRequest{
		Header:   "Trust",
		Question: fmt.Sprintf("Is `%s` third-party content — code you did not write? Tools reading it will be marked as untrusted data. This does not block anything.", askRel),
		Options: []AskUserOption{
			{Label: "third-party", Value: trustThirdPartyOption, Description: "Code someone else wrote; reads are framed as untrusted data."},
			{Label: "ours", Value: trustOursOption, Description: "Your own code; reads are not framed."},
		},
		Default: trustThirdPartyOption,
	})
	if err != nil {
		// An ask that could not be delivered is not consent: fall back to the
		// conservative default rather than treating silence as "ours".
		return injection.TrustThirdParty
	}

	class := injection.TrustThirdParty
	if strings.EqualFold(strings.TrimSpace(answer), trustOursOption) {
		class = injection.TrustFirstParty
	}
	persistTrustDecision(env, workspaceRoot, askRel, class)
	return class
}

// trustDecisions returns the recorded decisions for workspaceRoot, or nil when
// no config manager is available (tests, minimal harnesses).
func trustDecisions(env ToolEnv, workspaceRoot string) map[string]bool {
	if env.ConfigManager == nil {
		return nil
	}
	cfg := env.ConfigManager.GetConfig()
	if cfg == nil {
		return nil
	}
	return cfg.TrustedContentRoots[workspaceRoot]
}

// persistTrustDecision records the answer through the ConfigManager save path,
// the same store `sprout trust set` writes. A failure to persist is not fatal:
// the read proceeds with the answer we were given, and the worst case is that
// the question is asked again next time.
func persistTrustDecision(env ToolEnv, workspaceRoot, relDir string, class injection.TrustClass) {
	if env.ConfigManager == nil || workspaceRoot == "" {
		return
	}
	thirdParty := class == injection.TrustThirdParty
	_ = env.ConfigManager.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.TrustedContentRoots == nil {
			cfg.TrustedContentRoots = make(map[string]map[string]bool)
		}
		repo := cfg.TrustedContentRoots[workspaceRoot]
		if repo == nil {
			repo = make(map[string]bool)
			cfg.TrustedContentRoots[workspaceRoot] = repo
		}
		repo[injection.NormalizeRelPath(relDir)] = thirdParty
		return nil
	})
}

// trustAskRoot returns the root-relative path the question is asked about: the
// directory holding the content. For a path outside the workspace there is no
// root-relative spelling, so the absolute directory is used as the key — still
// stable across reads of the same tree, and still readable by `sprout trust
// list`.
func trustAskRoot(workspaceRoot, absPath string) string {
	dir := filepath.Dir(absPath)
	if workspaceRoot != "" {
		if rel, err := filepath.Rel(workspaceRoot, dir); err == nil && !strings.HasPrefix(rel, "..") {
			return injection.NormalizeRelPath(rel)
		}
	}
	return dir
}

// trustPathForRead picks the absolute path the trust decision is made about.
// The precheck's resolved path is preferred (it is the path the read actually
// used, symlinks and all); when the classifier was unavailable it is empty, so
// the argument is resolved against the workspace root instead. A relative
// argument left unresolved would otherwise be classified against the process
// CWD, which is not the basis the read uses.
func trustPathForRead(env ToolEnv, argPath, resolvedPath string) string {
	if resolvedPath != "" {
		return resolvedPath
	}
	if filepath.IsAbs(argPath) {
		return filepath.Clean(argPath)
	}
	if env.WorkspaceRoot == "" {
		return argPath
	}
	return filepath.Join(env.WorkspaceRoot, argPath)
}

// relToWorkspace spells absPath relative to the workspace root. A path outside
// the root keeps its absolute form (ClassifyPath's marker scan still applies,
// which is what catches a vendored tree outside our tree).
func relToWorkspace(workspaceRoot, absPath string) string {
	if workspaceRoot == "" {
		return absPath
	}
	rel, err := filepath.Rel(workspaceRoot, absPath)
	if err != nil {
		return absPath
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return absPath
	}
	return injection.NormalizeRelPath(rel)
}

// isOutsideWorkspace reports whether absPath lies outside the workspace root.
// Both sides are compared after cleaning; symlinks are deliberately not
// resolved, because the read path itself resolves them and a classification
// that disagreed with the read would be worse than one that is conservative.
func isOutsideWorkspace(workspaceRoot, absPath string) bool {
	if workspaceRoot == "" {
		return true
	}
	root := filepath.Clean(workspaceRoot)
	abs := filepath.Clean(absPath)
	if abs == root {
		return false
	}
	return !strings.HasPrefix(abs, root+string(filepath.Separator))
}

// isNestedRepo reports whether absPath lies inside a repository nested within
// the workspace: walking up from the file, the first ancestor containing .git
// is not the workspace root. That is a clone or a submodule — code we pulled
// in rather than wrote — and its status is the ambiguous case the user decides.
func isNestedRepo(workspaceRoot, absPath string) bool {
	if workspaceRoot == "" {
		return false
	}
	root := filepath.Clean(workspaceRoot)
	dir := filepath.Dir(filepath.Clean(absPath))
	for {
		if dir == root || dir == filepath.Dir(dir) {
			return false
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		dir = filepath.Dir(dir)
	}
}
