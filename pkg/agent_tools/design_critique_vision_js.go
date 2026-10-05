//go:build js

package tools

import (
	"context"
	"errors"
)

// errCritiqueByPrimary tells design_critique that this build has no separate
// vision tier: the rendered image rides the tool result with the rubric, and
// the primary model writes the critique from the pixels it was given.
var errCritiqueByPrimary = errors.New("no separate vision tier in the browser build; the rendered image is attached with the rubric — critique it directly")

func runCritiqueVisionPass(_ context.Context, _ ToolEnv, _, _ string) (string, error) {
	return "", errCritiqueByPrimary
}
