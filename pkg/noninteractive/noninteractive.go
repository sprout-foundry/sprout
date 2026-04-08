package noninteractive

// HelpHint is the canonical guidance shown when a provider is not configured
// in non-interactive environments (daemons, CI, piped stdin).
const HelpHint = "Set LEDIT_PROVIDER / configure ~/.ledit/config.json, or run `ledit agent` interactively"
