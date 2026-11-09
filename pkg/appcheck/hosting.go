//go:build !js

package appcheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// compatibilityDate is the platform's compatibility date: hosting runs the
// worker on it with no flags, so the check does too.
const compatibilityDate = "2025-01-01"

// wranglerEnv runs wrangler without telemetry or prompts: the machine the
// check runs on reaches only what it must.
func wranglerEnv() []string {
	env := os.Environ()
	return append(env, "CI=1", "WRANGLER_SEND_METRICS=false")
}

var (
	tomlMainRe       = regexp.MustCompile(`(?m)^main\s*=.*$`)
	tomlCompatRe     = regexp.MustCompile(`(?m)^compatibility_date\s*=.*$`)
	tomlFlagsRe      = regexp.MustCompile(`(?m)^compatibility_flags\s*=.*$`)
	tomlDirectoryRe  = regexp.MustCompile(`(?m)^directory\s*=.*$`)
	tomlMigrationsRe = regexp.MustCompile(`(?m)^migrations_dir\s*=.*$`)
	tomlHasMainRe    = regexp.MustCompile(`(?m)^main\s*=`)
	tomlDatabaseRe   = regexp.MustCompile(`(?m)^database_name\s*=\s*"([^"]+)"`)
	tomlD1Re         = regexp.MustCompile(`(?m)^\[\[d1_databases\]\]`)
	tomlNotFoundRe   = regexp.MustCompile(`(?m)^not_found_handling\s*=\s*"([^"]+)"`)
)

// hostedToml rewrites wrangler.toml into the configuration hosting uses: the
// bundled server from the build output, the platform's compatibility date,
// no flags, and assets from the site directory the check copies.
func hostedToml(text string) string {
	out := tomlMainRe.ReplaceAllString(text, `main = "worker.js"`)
	out = tomlCompatRe.ReplaceAllString(out, fmt.Sprintf(`compatibility_date = %q`, compatibilityDate))
	out = tomlFlagsRe.ReplaceAllString(out, "")
	out = tomlDirectoryRe.ReplaceAllString(out, `directory = "./site"`)
	out = tomlMigrationsRe.ReplaceAllString(out, `migrations_dir = "migrations"`)
	if !regexp.MustCompile(`(?m)^compatibility_date\s*=`).MatchString(out) {
		out = fmt.Sprintf("compatibility_date = %q\n", compatibilityDate) + out
	}
	return out
}

// copySite copies dist/ into site, minus the files hosting does not serve:
// worker.js, _migrations, .assetsignore itself, and whatever .assetsignore
// lists.
func copySite(dist, site string) error {
	if err := os.CopyFS(site, os.DirFS(dist)); err != nil { //nolint:gosec // G305: dist is the project's own build output
		return fmt.Errorf("copy build output: %w", err)
	}
	names := []string{"worker.js", "_migrations", ".assetsignore"}
	ignorePath := filepath.Join(site, ".assetsignore")
	if data, err := os.ReadFile(ignorePath); err == nil { //nolint:gosec // G304: inside the scratch copy
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			names = append(names, line)
		}
	}
	for _, name := range names {
		if err := os.RemoveAll(filepath.Join(site, name)); err != nil {
			return fmt.Errorf("remove %s from the served copy: %w", name, err)
		}
	}
	return nil
}

// notFoundHandling reproduces hosting's choice: the setting if given, else a
// 404 page when the site has one, else a single-page app when there is
// exactly one HTML file, else none.
func notFoundHandling(toml, site string) string {
	if m := tomlNotFoundRe.FindStringSubmatch(toml); m != nil {
		return m[1]
	}
	if _, err := os.Stat(filepath.Join(site, "404.html")); err == nil {
		return "404-page"
	}
	htmlFiles := 0
	_ = filepath.WalkDir(site, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".html") {
			htmlFiles++
		}
		return nil
	})
	if htmlFiles == 1 {
		return "single-page-application"
	}
	return "none"
}

// hasMigrations reports whether the build output ships the database schema
// migrations hosting applies (dist/_migrations/*.sql).
func hasMigrations(dist string) bool {
	matches, _ := filepath.Glob(filepath.Join(dist, "_migrations", "*.sql"))
	return len(matches) > 0
}

// build runs the project's build. It returns the combined trailing output on
// failure, for the report.
func build(root string) error {
	cmd := exec.Command("npm", "run", "build") //nolint:gosec // G204: the project's own build script
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := string(out)
		if len(text) > 1500 {
			text = text[len(text)-1500:]
		}
		return fmt.Errorf("%s", text)
	}
	return nil
}
