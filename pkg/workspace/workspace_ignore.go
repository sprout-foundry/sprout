package workspace

import (
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

func GetIgnoreRules(rootDir string) *ignore.GitIgnore {
	var allLines []string

	// Always include essential ledit patterns first (these should never be overridden)
	allLines = append(allLines, getEssentialLeditPatterns()...)

	// Load .gitignore rules
	gitIgnorePath := filepath.Join(rootDir, ".gitignore")
	if gitIgnoreContent, err := os.ReadFile(gitIgnorePath); err == nil {
		allLines = append(allLines, strings.Split(string(gitIgnoreContent), "\n")...)
	}

	// Load .ledit/leditignore rules
	leditIgnorePath := filepath.Join(rootDir, ".ledit", "leditignore")
	if leditIgnoreContent, err := os.ReadFile(leditIgnorePath); err == nil {
		allLines = append(allLines, strings.Split(string(leditIgnoreContent), "\n")...)
	}

	// Always include fallback patterns for common files that should be ignored
	allLines = append(allLines, getFallbackIgnorePatterns()...)

	// Filter out empty lines and comments
	var filteredLines []string
	for _, line := range allLines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			filteredLines = append(filteredLines, line)
		}
	}

	return ignore.CompileIgnoreLines(filteredLines...)
}

func AddToLeditIgnore(ignoreFilePath, path string) error {
	// Open file in append mode, create if not exists
	f, err := os.OpenFile(ignoreFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Add the path followed by newline
	if _, err := f.WriteString(path + "\n"); err != nil {
		return err
	}

	return nil
}

// getEssentialLeditPatterns returns patterns that should always be ignored by ledit
// These patterns ensure workspace isolation and prevent ledit from analyzing its own files
func getEssentialLeditPatterns() []string {
	return []string{
		".ledit/",          // Ledit workspace directory (always ignore)
		".ledit/*",         // All contents of ledit directory
		"ledit",            // Ledit binary (if present in workspace)
		"testing/",         // Testing directory (if present)
		"test_results.txt", // Test results file
		"e2e_results.csv",  // E2E test results
	}
}

// getFallbackIgnorePatterns returns common patterns that should be ignored
// These are the same as before but separated for clarity
func getFallbackIgnorePatterns() []string {
	return []string{
		// Operating System Files and Thumbnails
		".DS_Store",        // macOS directory metadata
		"Thumbs.db",        // Windows thumbnail cache
		".ehthumbs.db",     // Windows thumbnail cache
		"Desktop.ini",      // Windows folder customization
		"$RECYCLE.BIN/",    // Windows Recycle Bin
		"*.lnk",            // Windows shortcut files
		"._*",              // macOS AppleDouble files
		".Spotlight-V100/", // macOS Spotlight index
		".Trashes/",        // macOS trash directory
		"*.swp",            // Vim swap files
		"*.swo",            // Vim swap files
		"*.swn",            // Vim swap files
		"~*",               // Emacs/Vim temporary files
		"#*#",              // Emacs temporary files
		".#*",              // Emacs lock files
		"*.bak",            // Backup files
		"*.tmp",            // Temporary files
		"*.temp",           // Temporary files
		"*.dump",           // Dump files
		"*.log",            // Log files
		"*.out",            // Output files
		"*.err",            // Error files
		"Session.vim",      // Vim session file
		".netrwhist",       // Vim netrw history

		// IDE and Editor Files
		".idea/",          // IntelliJ IDEA project files
		"*.iml",           // IntelliJ IDEA module files (sometimes committed, but often local)
		"*.iws",           // IntelliJ IDEA workspace files (old format)
		"*.ipr",           // IntelliJ IDEA project files (old format)
		".vscode/",        // VS Code workspace settings and extensions
		".vs/",            // Visual Studio solution files (temp/cache)
		"*.suo",           // Visual Studio solution user options
		"*.user",          // Visual Studio project user options
		"*.ncb",           // Visual C++ IntelliSense database
		"*.sdf",           // Visual C++ IntelliSense database
		"*.opensdf",       // Visual C++ IntelliSense database
		"*.vcxproj.user",  // Visual Studio C++ project user options
		"*.filters",       // Visual Studio C++ filters (often committed, but ignore if auto-generated)
		"*.sln.docstates", // Visual Studio document states
		"*.vsp",           // Visual Studio performance report
		"*.dsk",           // Visual Studio desktop file
		"*.layout",        // Visual Studio layout file
		"*.vcproj.*.user", // Visual C++ user project settings
		"*.opt",           // Various tool options files
		"*.errlog",        // Error log files

		// Eclipse
		".project",   // Eclipse project file (often committed, but can be ignored for simple projects)
		".classpath", // Eclipse classpath file (often committed)
		".settings/", // Eclipse project settings (often committed)
		".metadata/", // Eclipse workspace metadata (user-specific)

		// Sublime Text
		"*.sublime-workspace", // Sublime Text workspace file

		// Atom
		".atom/", // Atom editor files

		// Visual Studio Code specific patterns (beyond .vscode/)
		"*.code-workspace", // VS Code workspace file (often committed, but ignore if personal)

		// Build and Output Files
		"build/",        // General build output directory
		"dist/",         // Distribution directory
		"bin/",          // Binary output directory (Go, C/C++, etc.)
		"obj/",          // Object files directory (C/C++, .NET)
		"target/",       // Maven/Gradle build output directory
		"out/",          // General output directory
		"classes/",      // Java compiled classes
		"test-classes/", // Java compiled test classes
		"gen/",          // Generated source files

		// Compiled and Executable Files
		"*.class", // Java compiled classes
		"*.jar",   // Java Archive
		"*.war",   // Web Application Archive
		"*.ear",   // Enterprise Application Archive
		"*.exe",   // Executable files
		"*.dll",   // Dynamic Link Libraries
		"*.so",    // Shared Objects
		"*.dylib", // macOS Dynamic Libraries
		"*.o",     // Object files (C/C++)
		"*.obj",   // Object files (C/C++)
		"*.a",     // Static Libraries (Unix)
		"*.lib",   // Static Libraries (Windows)
		"*.pdb",   // Program Debug Database
		"*.ilk",   // Incremental Linker File

		// Language Specific Ignored Files
		// Python
		"__pycache__/",        // Python 3 cache directory
		"*.pyc",               // Python compiled files
		"*.pyd",               // Python dynamic library
		"*.pyo",               // Python optimized compiled files
		"venv/",               // Python virtual environment
		".venv/",              // Python virtual environment (common alternative name)
		"env/",                // Python virtual environment (common alternative name)
		".Python",             // Python virtual environment (legacy)
		"pip-log.txt",         // Pip log file
		".pytest_cache/",      // Pytest cache
		".mypy_cache/",        // Mypy cache
		"htmlcov/",            // Coverage.py HTML report
		".coverage",           // Coverage.py data file
		"*.egg",               // Python eggs
		"*.egg-info/",         // Python egg info directory
		".ipynb_checkpoints/", // Jupyter Notebook checkpoints

		// Go
		"*.test", // Go test binaries
		"*.out",  // Go output files (e.g., pprof)

		// Node.js / JavaScript
		"node_modules/",  // Node.js dependencies
		"npm-debug.log",  // npm debug log
		"yarn-debug.log", // Yarn debug log
		"yarn-error.log", // Yarn error log
		".npm/",          // npm cache
		".cache/",        // General cache directory (also used by Node.js tools)
		"coverage/",      // Jest/Istanbul test coverage reports
		".parcel-cache/", // Parcel bundler cache
		".next/",         // Next.js build output
		".nuxt/",         // Nuxt.js build output
		".vuepress/",     // VuePress build output
		".cache/",        // General cache directory

		// Ruby
		"vendor/bundle/", // Bundler installed gems
		".bundle/",       // Bundler configuration
		"Rakefile.lock",  // Rakefile lock (less common than Gemfile.lock)

		// PHP
		"vendor/", // Composer installed dependencies

		// Rust
		"target/", // Cargo build output directory

		// Dart / Flutter
		".dart_tool/",                   // Dart tool cache
		".flutter-plugins",              // Flutter plugin config
		".flutter-plugins-dependencies", // Flutter plugin config dependencies

		// Other Build/Tool Specific
		"CMakeCache.txt",        // CMake cache file
		"CMakeFiles/",           // CMake build files
		"cmake_install.cmake",   // CMake install script
		"install_manifest.txt",  // CMake install manifest
		"CPPLint/",              // CppLint cache
		"compile_commands.json", // Compilation database (often useful for tools, but can be ignored)
		"Makefile.Debug",        // Generated Makefiles
		"Makefile.Release",      // Generated Makefiles
		"*.userprefs",           // User preferences for various tools

		// Version Control Files
		".git/",          // Git repository metadata
		".gitignore",     // Itself (you usually commit this, but if you want to ignore modifications)
		".gitattributes", // Git attributes (usually committed)
		".gitmodules",    // Git submodules (usually committed)
		".svn/",          // SVN repository metadata
		".hg/",           // Mercurial repository metadata
		".hgignore",      // Mercurial ignore file (usually committed)
		"CVS/",           // CVS repository metadata
		".bzr/",          // Bazaar repository metadata

		// Cache, Configuration, and Miscellaneous
		".cache/",       // General cache directory
		".history",      // Editor history files (e.g., VS Code Local History extension)
		".editorconfig", // Editor configuration (often committed, but ignore if personal)
		"*.pid",         // Process ID files
		"*.pid.lock",    // Process ID lock files
		"*.lock",        // Generic lock files
		"*.orig",        // Original files from merges/patches
		"*.rej",         // Rejected patches
		"*.diff",        // Diff files
		"*.patch",       // Patch files

		// Environment Variables
		".env",           // Environment variables (often sensitive, local-only)
		"*.env",          // Specific environment variable files (e.g., .env.development)
		".flaskenv",      // Flask environment variables
		".babelrc.js",    // Babel config (if auto-generated)
		".prettierrc.js", // Prettier config (if auto-generated)

		// Documentation and Static Site Generators
		"_site/",      // Jekyll/Hugo output directory
		"public/",     // Hugo/Gatsby/Next.js output directory (if distinct from dist/build)
		"_book/",      // GitBook output
		"docs/build/", // Sphinx/ReadTheDocs build output
		"gh-pages/",   // GitHub Pages branch (if building locally)

		// Terraform
		".terraform/",      // Terraform internal state/cache (not the .tfstate files)
		"*.tfstate",        // Terraform state files (sensitive, should be in remote backend)
		"*.tfstate.backup", // Terraform state backups

		// AWS SAM / Serverless Framework
		".aws-sam/",    // AWS SAM build artifacts
		".serverless/", // Serverless Framework build artifacts

		// Vagrant
		".vagrant/", // Vagrant VM state and cache

		// Docker
		"*.dockerignore", // Docker ignore file (usually committed)
		".docker/",       // Docker build context cache
		"Dockerfile.bak", // Dockerfile backup

		// Other common ignored files
		"Icon\r",         // macOS icon file
		".bzrignore",     // Bazaar ignore file
		".cvsignore",     // CVS ignore file
		"spsv_targets/",  // Some specific build system files
		"*.pydevproject", // PyDev project files
	}
}

// getFallbackIgnore remains for backward compatibility but now just calls the new function
func getFallbackIgnore() *ignore.GitIgnore {
	return ignore.CompileIgnoreLines(getFallbackIgnorePatterns()...)
}
