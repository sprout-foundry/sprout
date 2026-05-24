//go:build js && wasm

package main

import (
	"context"
	"os"
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// workspaceJSFuncs returns the workspace entries that main.go merges
// into the SproutWasm global. Exposes directory walk functions so the
// browser can discover indexable files on the MEMFS.
func workspaceJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"walkCodeFiles":         js.FuncOf(walkFilesFunc(embedding.WalkCodeFiles)),
		"walkAllIndexableFiles": js.FuncOf(walkFilesFunc(embedding.WalkAllIndexableFiles)),
	}
}

// walkFilesFunc builds a js.FuncOf handler from an embedding walk function.
// Centralizes the result shape {files, count, root} and the optional root
// resolution so each walk variant is a single registration line.
func walkFilesFunc(walkFn func(context.Context, string) ([]string, error)) func(js.Value, []js.Value) interface{} {
	return func(this js.Value, args []js.Value) interface{} {
		return asPromise(func(ctx context.Context) (interface{}, error) {
			root := argString(args, 0, "")
			if root == "" {
				var err error
				root, err = os.Getwd()
				if err != nil {
					return nil, err
				}
			}
			files, err := walkFn(ctx, root)
			if err != nil {
				return nil, err
			}
			if files == nil {
				files = []string{}
			}
			return map[string]interface{}{
				"files": files,
				"count": len(files),
				"root":  root,
			}, nil
		})
	}
}
