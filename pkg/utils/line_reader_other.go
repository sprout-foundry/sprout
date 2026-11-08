//go:build !unix

package utils

import (
	"errors"
	"time"
)

var (
	errEAGAIN      = errors.New("eagain")
	errEWOULDBLOCK = errors.New("ewouldblock")
)

// waitForReadable has no poll primitive on this platform; report ready
// so the caller's Read runs. pollGatedReads is false so LineReader takes
// its goroutine+watchdog path and the deadline still fires.
func waitForReadable(fd int, timeout time.Duration) bool { return true }

// pollGatedReads reports whether waitForReadable really gates reads.
var pollGatedReads = false
