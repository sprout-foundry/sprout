package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// SaveWorkspaceOverlay persists a layered (global + workspace) config to the
// workspace file as an overlay: the settings the file already holds, plus
// whatever changed since since. The in-memory config is the merge of both
// layers, so writing it whole copied every global setting into the workspace
// file, where it then overrode later global changes.
func (c *Config) SaveWorkspaceOverlay(dir, fileName string, since *Config) error {
	c.prepareForPersist()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}
	path := filepath.Join(dir, fileName)

	overlay := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &overlay); err != nil {
			return fmt.Errorf("parse workspace config %q: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read workspace config %q: %w", path, err)
	}

	current, err := persistedJSONMap(c)
	if err != nil {
		return err
	}
	previous := map[string]interface{}{}
	if since != nil {
		if previous, err = persistedJSONMap(since); err != nil {
			return err
		}
	}
	applyChangedJSON(overlay, previous, current)
	overlay["version"] = ConfigVersion

	data, err := json.MarshalIndent(overlay, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace config: %w", err)
	}
	if err := writeFileAtomic(path, data, 0600); err != nil {
		return fmt.Errorf("write workspace config: %w", err)
	}
	return nil
}

func persistedJSONMap(c *Config) (map[string]interface{}, error) {
	persisted := *c
	persisted.Version = ConfigVersion
	persisted.CustomProviders = nil
	data, err := json.Marshal(&persisted)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return out, nil
}

// applyChangedJSON writes into dst every value that differs between previous
// and current, descending into objects so a change to one nested setting
// doesn't pin its siblings. A value current dropped (a zero under omitempty)
// is written as that zero, so the overlay still overrides the layer below.
func applyChangedJSON(dst, previous, current map[string]interface{}) {
	for key, cur := range current {
		prev, had := previous[key]
		if had && reflect.DeepEqual(prev, cur) {
			continue
		}
		curObj, curIsObj := cur.(map[string]interface{})
		prevObj, prevIsObj := prev.(map[string]interface{})
		if curIsObj && (prevIsObj || !had) {
			if prevObj == nil {
				prevObj = map[string]interface{}{}
			}
			sub, ok := dst[key].(map[string]interface{})
			if !ok {
				sub = map[string]interface{}{}
			}
			applyChangedJSON(sub, prevObj, curObj)
			if len(sub) > 0 {
				dst[key] = sub
			}
			continue
		}
		dst[key] = cur
	}
	for key, prev := range previous {
		if _, still := current[key]; still {
			continue
		}
		dst[key] = zeroJSON(prev)
	}
}

func zeroJSON(v interface{}) interface{} {
	switch v.(type) {
	case bool:
		return false
	case float64:
		return 0
	case string:
		return ""
	case []interface{}:
		return []interface{}{}
	case map[string]interface{}:
		return map[string]interface{}{}
	}
	return nil
}
