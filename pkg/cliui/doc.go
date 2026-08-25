// Package cliui hosts the CLI terminal display helpers extracted from cmd/.
// These files provide the terminal subscriber, tool display, subagent display,
// and turn statistics functionality used by the agent command modes.
//
// All .go files in this package (other than this doc.go) carry a
// //go:build !js build constraint and are excluded from the JS/WASM build —
// the WebUI uses a different rendering path that lives outside this package.
package cliui
