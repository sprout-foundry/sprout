package tools

import (
	"fmt"
	"slices"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// Patch operation parsing (used by patch_structured_file). Applying a patch
// happens against a yaml.Node in structured_patches_yaml.go, which preserves
// key order; the generic interface{} appliers that used to live here had no
// callers and lost that order.
// ---------------------------------------------------------------------------

// stringField returns obj[key] as a string, or "" when the key is absent or
// holds a non-string. It exists because fmt.Sprint(nil) is "<nil>": a missing
// key would otherwise read as a present, non-empty value.
func stringField(obj map[string]interface{}, key string) string {
	s, _ := obj[key].(string)
	return s
}

func parsePatchOperations(v interface{}) ([]jsonPatchOperation, error) {
	rawOps, ok := v.([]interface{})
	if !ok {
		return nil, agenterrors.NewInvalidInputError("parameter 'patch_ops' must be an array", nil)
	}

	ops := make([]jsonPatchOperation, 0, len(rawOps))
	for i, raw := range rawOps {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch_ops[%d] must be an object", i), nil)
		}
		// Read the string fields as strings: fmt.Sprint(nil) yields "<nil>",
		// which is non-empty and would slip past the empty-path check below.
		op := strings.ToLower(strings.TrimSpace(stringField(obj, "op")))
		path := stringField(obj, "path")
		from := stringField(obj, "from")

		if op == "" || path == "" {
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch_ops[%d] requires non-empty op and path", i), nil)
		}
		if !slices.Contains([]string{"add", "replace", "remove", "test"}, op) {
			return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch_ops[%d] has unsupported op '%s'", i, op), nil)
		}
		if op == "add" || op == "replace" || op == "test" {
			if _, exists := obj["value"]; !exists {
				return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("patch_ops[%d] requires value for op '%s'", i, op), nil)
			}
		}
		ops = append(ops, jsonPatchOperation{
			Op:    op,
			Path:  path,
			From:  from,
			Value: obj["value"],
		})
	}

	return ops, nil
}

// ---------------------------------------------------------------------------
// Patch operations on generic interface{} (loses YAML key order)
// ---------------------------------------------------------------------------

func parseJSONPointer(path string) ([]string, error) {
	if path == "" {
		return nil, agenterrors.NewInvalidInputError("patch path cannot be empty", nil)
	}
	if path == "/" {
		return []string{""}, nil
	}
	if path[0] != '/' {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("invalid patch path '%s': must start with '/'", path), nil)
	}

	raw := strings.Split(path[1:], "/")
	segments := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		segments = append(segments, part)
	}
	return segments, nil
}
