package configuration

import (
	"encoding/json"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// Config files written before the project folder rename spell two keys with
// "workspace"; these decoders still read them when the current key is absent.

func (s *ShellConfig) UnmarshalJSON(data []byte) error {
	type plain ShellConfig
	if err := json.Unmarshal(data, (*plain)(s)); err != nil {
		return err
	}
	return utils.AdoptEarlierJSONKey(data, "project_folder_overlay", "workspace_overlay", &s.ProjectFolderOverlay)
}

func (c *ComputerUseConfig) UnmarshalJSON(data []byte) error {
	type plain ComputerUseConfig
	if err := json.Unmarshal(data, (*plain)(c)); err != nil {
		return err
	}
	return utils.AdoptEarlierJSONKey(data, "project_folder_allowlist", "workspace_allowlist", &c.ProjectFolderAllowlist)
}
