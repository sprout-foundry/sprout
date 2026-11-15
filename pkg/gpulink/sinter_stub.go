//go:build !darwin || !arm64 || !cgo

package gpulink

import (
	"errors"
	"net/http"
)

// SinterAvailable reports whether this build runs sinter models in-process.
const SinterAvailable = false

// errNoSinter is a build without the in-process engine.
var errNoSinter = errors.New("this sprout build has no in-process sinter engine (it ships for Apple Silicon Macs); use --engine host:port with a local server instead")

// SinterHandler is unavailable in this build.
func SinterHandler(string) (http.Handler, string, func(), error) {
	return nil, "", nil, errNoSinter
}
