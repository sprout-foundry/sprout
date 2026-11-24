package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// ProjectFolderPruneResult describes what PruneProjectFolderConfig removed.
type ProjectFolderPruneResult struct {
	Path    string
	Removed []string // dot paths, sorted
	Kept    []string // top-level keys left in the file, sorted
}

// PruneProjectFolderConfig removes from a project folder config file every setting
// whose value the global config already gives: the same value, or a zero the
// global config leaves unset. Such files were written by earlier versions,
// which saved the whole merged config into the project folder layer, so global
// changes stopped reaching the project folder. A setting deliberately pinned to
// the global value is removed too — it keeps working until the global value
// changes. Unless dryRun, the original is kept as <file>.bak.
func PruneProjectFolderConfig(globalDir, projectFolderFile string, dryRun bool) (ProjectFolderPruneResult, error) {
	result := ProjectFolderPruneResult{Path: projectFolderFile}
	original, err := os.ReadFile(projectFolderFile)
	if err != nil {
		return result, fmt.Errorf("read project folder config: %w", err)
	}
	projectFolder := map[string]interface{}{}
	if err := json.Unmarshal(original, &projectFolder); err != nil {
		return result, fmt.Errorf("parse project folder config %q: %w", projectFolderFile, err)
	}

	global, err := LoadConfigWithLayers(filepath.Join(globalDir, ConfigFileName), "", "", globalDir)
	if err != nil {
		return result, fmt.Errorf("load global config: %w", err)
	}
	globalMap, err := persistedJSONMap(global)
	if err != nil {
		return result, err
	}
	delete(globalMap, "version")

	pruned := cloneJSONMap(projectFolder)
	pruneRedundantJSON(pruned, globalMap, "", &result.Removed)

	// Some settings replace the global value wholesale instead of merging
	// into it (command history maps, skills), so dropping entries inside them
	// changes the result. Check the effective config and put back, whole,
	// any setting whose effective value would change.
	globalFile := filepath.Join(globalDir, ConfigFileName)
	changed, err := effectiveDifferences(globalFile, globalDir, projectFolder, pruned)
	if err != nil {
		return result, err
	}
	for _, key := range changed {
		if original, ok := projectFolder[key]; ok {
			pruned[key] = original
		}
		result.Removed = dropPathsUnder(result.Removed, key)
	}
	if len(changed) > 0 {
		if still, err := effectiveDifferences(globalFile, globalDir, projectFolder, pruned); err != nil {
			return result, err
		} else if len(still) > 0 {
			return result, fmt.Errorf("pruning %s would change %s; left unchanged", projectFolderFile, strings.Join(still, ", "))
		}
	}
	projectFolder = pruned
	sort.Strings(result.Removed)
	for key := range projectFolder {
		result.Kept = append(result.Kept, key)
	}
	sort.Strings(result.Kept)

	if dryRun || len(result.Removed) == 0 {
		return result, nil
	}
	if err := os.WriteFile(filepath.Clean(projectFolderFile+".bak"), original, 0600); err != nil { // #nosec G703 -- beside the project folder config the caller named
		return result, fmt.Errorf("back up project folder config: %w", err)
	}
	data, err := json.MarshalIndent(projectFolder, "", "  ")
	if err != nil {
		return result, fmt.Errorf("marshal project folder config: %w", err)
	}
	if err := os.WriteFile(projectFolderFile, data, 0600); err != nil {
		return result, fmt.Errorf("write project folder config: %w", err)
	}
	return result, nil
}

func pruneRedundantJSON(projectFolder, global map[string]interface{}, prefix string, removed *[]string) {
	for key, value := range projectFolder {
		if prefix == "" && key == "version" {
			continue
		}
		path := strings.TrimPrefix(prefix+"."+key, ".")
		globalValue, inGlobal := global[key]
		wsObj, wsIsObj := value.(map[string]interface{})
		globalObj, globalIsObj := globalValue.(map[string]interface{})
		switch {
		case wsIsObj && (globalIsObj || !inGlobal):
			if globalObj == nil {
				globalObj = map[string]interface{}{}
			}
			wasEmpty := len(wsObj) == 0
			pruneRedundantJSON(wsObj, globalObj, path, removed)
			if len(wsObj) == 0 {
				delete(projectFolder, key)
				if wasEmpty {
					*removed = append(*removed, path)
				}
			}
		case inGlobal && reflect.DeepEqual(value, globalValue),
			!inGlobal && isZeroJSON(value):
			delete(projectFolder, key)
			*removed = append(*removed, path)
		}
	}
}

func isZeroJSON(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case float64:
		return t == 0
	case string:
		return t == ""
	case []interface{}:
		return len(t) == 0
	case map[string]interface{}:
		return len(t) == 0
	}
	return false
}

func cloneJSONMap(m map[string]interface{}) map[string]interface{} {
	data, _ := json.Marshal(m)
	out := map[string]interface{}{}
	_ = json.Unmarshal(data, &out)
	return out
}

func dropPathsUnder(paths []string, key string) []string {
	kept := paths[:0]
	for _, p := range paths {
		if p != key && !strings.HasPrefix(p, key+".") {
			kept = append(kept, p)
		}
	}
	return kept
}

// effectiveDifferences loads global+project folder for both project folder versions
// and returns the top-level settings whose merged value differs.
func effectiveDifferences(globalFile, globalDir string, before, after map[string]interface{}) ([]string, error) {
	load := func(ws map[string]interface{}) (map[string]interface{}, error) {
		f, err := os.CreateTemp("", "sprout-project-folder-*.json")
		if err != nil {
			return nil, fmt.Errorf("stage project folder config: %w", err)
		}
		defer func() { _ = os.Remove(f.Name()) }()
		data, _ := json.Marshal(ws)
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("stage project folder config: %w", err)
		}
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("stage project folder config: %w", err)
		}
		// The staged layer lives in a temp file: no project folder, so
		// no project persona I/O during a prune.
		cfg, err := loadConfigWithLayers(globalFile, f.Name(), "", globalDir, "")
		if err != nil {
			return nil, fmt.Errorf("load layered config: %w", err)
		}
		return persistedJSONMap(cfg)
	}
	a, err := load(before)
	if err != nil {
		return nil, err
	}
	b, err := load(after)
	if err != nil {
		return nil, err
	}
	var diff []string
	for key := range a {
		if !reflect.DeepEqual(a[key], b[key]) {
			diff = append(diff, key)
		}
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			diff = append(diff, key)
		}
	}
	sort.Strings(diff)
	return diff, nil
}
