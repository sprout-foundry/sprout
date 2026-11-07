package configuration

// config_personas_legacy.go — the one-time notice for the retired
// `subagent_types` config key (SP-175).
//
// Personas are catalog-fixed and custom personas live in their own files. A
// `subagent_types` block left in an old config.json is ignored, as it has been
// since personas became catalog-fixed; the notice exists so a user who edited
// it knows where to put a custom persona now.

import (
	"log"
	"strings"
	"sync"
)

// legacySubagentTypesNoticeOnce keeps the notice to one per process: Load runs
// many times per process, and repeating the same hint is noise.
var legacySubagentTypesNoticeOnce sync.Once

// noticeLegacySubagentTypes logs where custom personas live when a loaded
// config still carries the retired `subagent_types` key.
func noticeLegacySubagentTypes(rawConfig map[string]interface{}) {
	if len(rawConfig) == 0 {
		return
	}
	value, ok := rawConfig["subagent_types"]
	if !ok {
		return
	}
	switch v := value.(type) {
	case nil:
		return
	case map[string]interface{}:
		if len(v) == 0 {
			return
		}
	}

	dir, err := UserPersonasDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		dir = "<config dir>/" + PersonasDirName
	}
	legacySubagentTypesNoticeOnce.Do(func() {
		log.Printf("[personas] note: the `subagent_types` key in config.json is ignored. "+
			"To customize a persona, put a definition in %s (see `sprout persona validate`).", dir)
	})
}
