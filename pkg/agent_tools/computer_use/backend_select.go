package computer_use

import (
	"os"
	"runtime"
)

// NewPlatformBackend returns the best available real backend for the current
// host, or a descriptive error when the platform/toolchain can't support
// computer use. Callers that get an error should keep the default MockBackend
// and surface the message to the user (e.g. "install cliclick").
func NewPlatformBackend() (ComputerBackend, error) {
	capTool, cliTool, err := detectTools(runtime.GOOS)
	if err != nil {
		return nil, err
	}
	return &subprocessBackend{
		os:      runtime.GOOS,
		tmpDir:  os.TempDir(),
		capTool: capTool,
		cliTool: cliTool,
	}, nil
}

// PlatformSupport describes whether the current host can run computer use and,
// if not, why. Used by the "Test connection" diagnostic and the persona
// activation check.
type PlatformSupport struct {
	Supported bool   `json:"supported"`
	OS        string `json:"os"`
	Reason    string `json:"reason,omitempty"` // populated when Supported is false
}

// CheckPlatformSupport reports whether a real backend can be constructed
// without actually taking control of the desktop.
func CheckPlatformSupport() PlatformSupport {
	if _, _, err := detectTools(runtime.GOOS); err != nil {
		return PlatformSupport{Supported: false, OS: runtime.GOOS, Reason: err.Error()}
	}
	return PlatformSupport{Supported: true, OS: runtime.GOOS}
}
