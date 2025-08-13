package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// prepareStagedWorkspace ensures the staged workspace exists and writes a README with instructions
func prepareStagedWorkspace(stageRoot string) error {
	if err := os.MkdirAll(stageRoot, os.ModePerm); err != nil {
		return err
	}
	readme := filepath.Join(stageRoot, "README.json")
	info := map[string]any{"note": "This is a staged workspace. Files here will be merged into the real workspace after validation.", "generated_by": "ledit"}
	b, _ := json.MarshalIndent(info, "", "  ")
	_ = os.WriteFile(readme, b, 0644)
	return nil
}

// writeStageManifest writes a manifest of staged files
func writeStageManifest(stageRoot string, files []string) error {
	m := map[string]any{"files": files}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(filepath.Join(stageRoot, "manifest.json"), b, 0644)
}
