//go:build js

package agent

// In the browser stderr lands in the console as errors, and the notices'
// remedies (config keys, slash commands) aren't available to the user.
const printsTerminalNotices = false
