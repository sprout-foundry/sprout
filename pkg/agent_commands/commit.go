// Package commands provides slash commands for the ledit agent.
//
// This file (commit.go) serves as a backward compatibility wrapper.
// The commit functionality has been refactored into multiple files:
//
//   - commit_types.go    : Types (CommitCommand, CommitJSONResult) and constants
//   - commit_git.go     : Git utility functions
//   - commit_review.go  : Commit review functions
//   - commit_command.go : Main command implementation
//
// All symbols are exported from this package directly since they share the same package name.

package commands

// Keep this file to maintain backward compatibility imports.
// The actual implementation is in the other commit_*.go files.
