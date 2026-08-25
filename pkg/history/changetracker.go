// Package history provides change tracking, revision management, and display.
//
// This package has been split from a single monolithic file (changetracker.go)
// into three focused files:
//   - changetracker_record.go  — types, revision grouping, and display
//   - changetracker_revert.go  — revert/restore logic and staleness checks
//   - changetracker_persist.go — persisted constants
//
// All exported API symbols remain unchanged.
package history
