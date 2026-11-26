package envutil

import "os"

// DevSpaceID is the dev space this process runs in, as its host sets it:
// DEV_SPACE_ID, or WORKSPACE_ID from hosts that predate the name.
func DevSpaceID() string { return firstEnv("DEV_SPACE_ID", "WORKSPACE_ID") }

// DevSpaceToken is the dev space's token for calls back to its host:
// DEV_SPACE_TOKEN, or WORKSPACE_TOKEN from hosts that predate the name.
func DevSpaceToken() string { return firstEnv("DEV_SPACE_TOKEN", "WORKSPACE_TOKEN") }

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}
