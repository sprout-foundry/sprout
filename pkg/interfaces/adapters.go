package interfaces

import (
	"context"
	"io"
	"os"
	"time"
)

// FileSystem defines the interface for file system operations
type FileSystem interface {
	// ReadFile reads the contents of a file
	ReadFile(path string) ([]byte, error)

	// WriteFile writes data to a file
	WriteFile(path string, data []byte, perm os.FileMode) error

	// CreateFile creates a new file
	CreateFile(path string) (*os.File, error)

	// OpenFile opens a file with specified flags and permissions
	OpenFile(path string, flag int, perm os.FileMode) (*os.File, error)

	// Exists checks if a file or directory exists
	Exists(path string) bool

	// IsDir checks if a path is a directory
	IsDir(path string) bool

	// MkdirAll creates directory and all necessary parents
	MkdirAll(path string, perm os.FileMode) error

	// RemoveAll removes a file or directory tree
	RemoveAll(path string) error

	// Rename renames/moves a file or directory
	Rename(oldpath, newpath string) error

	// ListDir lists the contents of a directory
	ListDir(path string) ([]DirEntry, error)

	// Walk walks a directory tree
	Walk(root string, walkFn WalkFunc) error

	// Stat returns file information
	Stat(path string) (FileInfo, error)

	// Glob returns the names of all files matching a pattern
	Glob(pattern string) ([]string, error)

	// WatchFiles watches for file system changes
	WatchFiles(paths []string, callback FileWatchCallback) error

	// GetWorkingDir returns the current working directory
	GetWorkingDir() (string, error)

	// ChangeDir changes the current working directory
	ChangeDir(dir string) error

	// GetTempDir returns the system temporary directory
	GetTempDir() string
}

// DirEntry represents a directory entry
type DirEntry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
	Mode    os.FileMode
}

// FileInfo represents file information
type FileInfo struct {
	Name    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	IsDir   bool
}

// WalkFunc is the type of function called by Walk
type WalkFunc func(path string, info FileInfo, err error) error

// FileWatchCallback is called when file changes are detected
type FileWatchCallback func(path string, event FileWatchEvent)

// FileWatchEvent represents a file system event
type FileWatchEvent struct {
	Type string // "create", "modify", "delete", "rename"
	Path string
	Time time.Time
}

// GitProvider defines the interface for Git operations
type GitProvider interface {
	// InitRepository initializes a new Git repository
	InitRepository(path string) error

	// CloneRepository clones a remote repository
	CloneRepository(url, path string) error

	// GetStatus returns the status of the repository
	GetStatus() (*GitStatus, error)

	// AddFiles adds files to the staging area
	AddFiles(patterns []string) error

	// CommitChanges creates a commit with staged changes
	CommitChanges(message string) error

	// CreateBranch creates a new branch
	CreateBranch(name string) error

	// CheckoutBranch switches to a branch
	CheckoutBranch(name string) error

	// GetBranches returns a list of branches
	GetBranches() ([]string, error)

	// GetCurrentBranch returns the current branch name
	GetCurrentBranch() (string, error)

	// GetCommitHistory returns commit history
	GetCommitHistory(limit int) ([]GitCommit, error)

	// GetDiff returns the diff between commits or working tree
	GetDiff(from, to string) (string, error)

	// Push pushes changes to remote repository
	Push(remote, branch string) error

	// Pull pulls changes from remote repository
	Pull(remote, branch string) error

	// IsRepository checks if the path is a Git repository
	IsRepository(path string) bool

	// GetRemotes returns configured remotes
	GetRemotes() (map[string]string, error)
}

// GitStatus represents the status of a Git repository
type GitStatus struct {
	Branch     string
	Ahead      int
	Behind     int
	Staged     []string
	Unstaged   []string
	Untracked  []string
	Conflicted []string
	Clean      bool
}

// GitCommit represents a Git commit
type GitCommit struct {
	Hash      string
	Author    string
	Email     string
	Date      time.Time
	Message   string
	ShortHash string
}

// UIProvider defines the interface for user interface operations
type UIProvider interface {
	// Print outputs text without formatting
	Print(text string)

	// Printf outputs formatted text
	Printf(format string, args ...interface{})

	// PrintLine outputs text with a newline
	PrintLine(text string)

	// PrintError outputs error text
	PrintError(text string)

	// PrintWarning outputs warning text
	PrintWarning(text string)

	// PrintSuccess outputs success text
	PrintSuccess(text string)

	// PrintDebug outputs debug text
	PrintDebug(text string)

	// PromptInput prompts for user input
	PromptInput(prompt string) (string, error)

	// PromptConfirm prompts for yes/no confirmation
	PromptConfirm(prompt string) (bool, error)

	// PromptSelect prompts for selection from options
	PromptSelect(prompt string, options []string) (string, error)

	// PromptPassword prompts for password input (hidden)
	PromptPassword(prompt string) (string, error)

	// ShowProgress shows a progress indicator
	ShowProgress(message string, current, total int)

	// HideProgress hides the progress indicator
	HideProgress()

	// StartSpinner starts a spinning progress indicator
	StartSpinner(message string)

	// StopSpinner stops the spinning progress indicator
	StopSpinner()

	// CreateTable creates a formatted table output
	CreateTable(headers []string, rows [][]string) string

	// SetColorOutput enables or disables colored output
	SetColorOutput(enabled bool)

	// IsInteractive returns true if running in interactive mode
	IsInteractive() bool

	// GetTerminalWidth returns the width of the terminal
	GetTerminalWidth() int
}

// ProcessRunner defines the interface for running external processes
type ProcessRunner interface {
	// Run runs a command and waits for completion
	Run(ctx context.Context, cmd string, args []string, options *RunOptions) (*ProcessResult, error)

	// Start starts a command without waiting for completion
	Start(ctx context.Context, cmd string, args []string, options *RunOptions) (*Process, error)

	// RunWithInput runs a command with input data
	RunWithInput(ctx context.Context, cmd string, args []string, input []byte, options *RunOptions) (*ProcessResult, error)

	// RunScript runs a shell script
	RunScript(ctx context.Context, script string, options *RunOptions) (*ProcessResult, error)
}

// RunOptions represents options for running processes
type RunOptions struct {
	Dir           string            // working directory
	Env           map[string]string // environment variables
	Stdin         io.Reader         // standard input
	Stdout        io.Writer         // standard output
	Stderr        io.Writer         // standard error
	Timeout       time.Duration     // execution timeout
	KillOnTimeout bool              // kill process on timeout
}

// ProcessResult represents the result of a process execution
type ProcessResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	Duration time.Duration
	Error    error
}

// Process represents a running process
type Process struct {
	PID    int
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
}

// Wait waits for the process to complete
func (p *Process) Wait() (*ProcessResult, error) {
	// Implementation would be provided by concrete type
	return nil, nil
}

// Kill terminates the process
func (p *Process) Kill() error {
	// Implementation would be provided by concrete type
	return nil
}
