//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or manage configuration",
	Long:  `Display and manage sprout configuration. Output is always credential-redacted.`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current configuration (credentials are redacted)",
	Long: `Display the current sprout configuration as JSON.
All credential values (API keys, tokens, secrets in MCP env vars, etc.)
are redacted before output.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		redacted := configuration.RedactConfig(config)

		data, err := json.MarshalIndent(redacted, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		fmt.Println(string(data))
		return nil
	},
}

var configShowOrigin bool

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Long: `Get a specific configuration value by dot-separated key path.

Examples:
  sprout config get last_used_provider
  sprout config get embedding_index.enabled
  sprout config get mcp.enabled

Use --show-origin to print which layer (global, global-local, workspace,
workspace-local, session) supplied the value and the file path.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]

		if configShowOrigin {
			return showConfigOrigin(key)
		}

		config, err := configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		value, found := getConfigField(config, key)
		if !found {
			return fmt.Errorf("key %q not found in configuration", key)
		}

		fmt.Println(formatConfigValue(value))
		return nil
	},
}

func init() {
	configGetCmd.Flags().BoolVar(&configShowOrigin, "show-origin", false, "show which layer and file supplied the value")
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configGetCmd)
	rootCmd.AddCommand(configCmd)
}

// getConfigField navigates a dot-separated key path through a Config
// struct using reflection. Returns the value and whether it was found.
func getConfigField(config *configuration.Config, key string) (interface{}, bool) {
	parts := strings.Split(key, ".")
	v := reflect.ValueOf(config)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	for _, part := range parts {
		// Try struct field by JSON tag first, then by Go name
		field, found := findFieldByJSONTag(v, part)
		if !found {
			return nil, false
		}
		v = field
	}

	if !v.IsValid() {
		return nil, false
	}
	return v.Interface(), true
}

func findFieldByJSONTag(v reflect.Value, tag string) (reflect.Value, bool) {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == tag || strings.EqualFold(field.Name, tag) {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func formatConfigValue(v interface{}) string {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return fmt.Sprintf("%v", rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", rv.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%v", rv.Float())
	default:
		data, _ := json.MarshalIndent(v, "", "  ")
		return string(data)
	}
}

// showConfigOrigin determines which configuration layer supplied a value
// and prints the layer name, file path, and shadowed layers.
func showConfigOrigin(key string) error {
	type layer struct {
		name string
		path string
	}

	var layers []layer

	// 1. Global config
	if globalPath, err := configuration.GetConfigPath(); err == nil {
		layers = append(layers, layer{"global", globalPath})
	}
	// 2. Global-local override
	if configDir, err := configuration.GetConfigDir(); err == nil {
		layers = append(layers, layer{"global-local", filepath.Join(configDir, configuration.ConfigLocalFileName)})
	}
	// 3. Workspace config
	if wsPath := configuration.GetWorkspaceConfigPath("."); wsPath != "" {
		layers = append(layers, layer{"workspace", wsPath})
	}
	// 4. Workspace-local override
	wsDir := configuration.WorkspaceConfigDir(".")
	if wsDir != "" {
		layers = append(layers, layer{"workspace-local", filepath.Join(wsDir, configuration.WorkspaceLocalFileName)})
	}

	var winner *layer
	var winnerIdx int = -1
	for i := len(layers) - 1; i >= 0; i-- {
		if _, err := os.Stat(layers[i].path); err == nil {
			// File exists — check if the key is actually set in it
			if keyExistsInFile(layers[i].path, key) {
				winner = &layers[i]
				winnerIdx = i
				break
			}
		}
	}

	if winner == nil {
		fmt.Printf("Key %q: no layer supplies this value (using default)\n", key)
		return nil
	}

	fmt.Printf("Key: %s\n", key)
	fmt.Printf("  Source: %s\n", winner.name)
	fmt.Printf("  File:   %s\n", winner.path)

	// Show shadowed layers (lower-precedence layers that also set this key)
	for i := 0; i < winnerIdx; i++ {
		if _, err := os.Stat(layers[i].path); err == nil {
			if keyExistsInFile(layers[i].path, key) {
				fmt.Printf("  Shadowed layer: %s (%s)\n", layers[i].name, layers[i].path)
			}
		}
	}

	return nil
}

// keyExistsInFile reads a config JSON file and checks whether the
// dot-separated key path exists in it.
func keyExistsInFile(path, key string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	return navigateJSON(raw, strings.Split(key, "."))
}

func navigateJSON(m map[string]interface{}, parts []string) bool {
	v, ok := m[parts[0]]
	if !ok {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	next, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	return navigateJSON(next, parts[1:])
}
