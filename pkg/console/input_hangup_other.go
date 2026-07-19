//go:build !unix || js

package console

// nonblockingEOFIsTransient is not implemented on non-Unix platforms
// (Windows, js/wasm) because O_NONBLOCK semantics and the POSIX poll
// hangup discriminator aren't available.
//
// Returning false here means EOF remains fatal on platforms where
// POSIX poll-based hangup detection is unavailable. This is deliberate:
// signal_compat_windows.go cannot actually enable O_NONBLOCK even though
// its compatibility stub returns nil, so Windows EOF is authoritative.
// The stub also keeps generic console code buildable for js/wasm.

func nonblockingEOFIsTransient(fd int) bool {
	_ = fd
	return false
}
