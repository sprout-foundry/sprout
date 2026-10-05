package txn

import "strings"

// daemonOnlyEnv names credentials the daemon authenticates with. No build,
// test or tool needs them, and a command that printed them would hand the
// daemon's API to whoever reads the output. Git credentials are deliberately
// not here: the credential helper reads them, and any command allowed to
// push can reach them through git anyway.
var daemonOnlyEnv = map[string]bool{
	"SPROUT_AUTH_TOKEN":           true,
	"SPROUT_WORKSPACE_TXN_SECRET": true,
	"CODING_ENV_JWT_SECRET":       true,
}

func runEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !daemonOnlyEnv[name] {
			out = append(out, kv)
		}
	}
	return out
}
