package webcontent

import "strings"

// transientNavigationMarkers are the Chromium net errors that describe a
// momentary condition. They are matched on the error text because rod
// surfaces navigation failures as *proto.Error carrying the raw net error
// string, with no typed equivalent to switch on.
var transientNavigationMarkers = []string{
	"ERR_CONNECTION_RESET",
	"ERR_CONNECTION_CLOSED",
	"ERR_CONNECTION_ABORTED",
	"ERR_TIMEOUT",
	"ERR_EMPTY_RESPONSE",
	"ERR_NETWORK_CHANGED",
	"ERR_HTTP2_PROTOCOL_ERROR",
	"ERR_QUIC_PROTOCOL_ERROR",
}

// IsTransientNavigationError reports whether a browse failure describes a
// momentary network condition rather than a property of the URL. Callers use
// it to decide between retrying and reporting.
//
// This lives outside the !js build so the agent's browse handler can classify
// errors in every build, including the WASM shell where the rod renderer is
// compiled out.
func IsTransientNavigationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, marker := range transientNavigationMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
