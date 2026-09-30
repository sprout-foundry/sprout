//go:build !windows

package search

func isTransientShareError(error) bool { return false }
