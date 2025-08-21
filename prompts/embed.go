package promptassets

import "embed"

// Only include prompt templates in this directory
//
//go:embed *.txt
var FS embed.FS
