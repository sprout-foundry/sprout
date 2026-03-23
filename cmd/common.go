package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// InstanceInfo represents a running ledit instance
type InstanceInfo struct {
	ID         string    `json:"id"`
	Port       int       `json:"port"`
	PID        int       `json:"pid"`
	StartTime  time.Time `json:"start_time"`
	WorkingDir string    `json:"working_dir"`
	LastPing   time.Time `json:"last_ping"`
	SessionID  string    `json:"session_id,omitempty"`
}

// getConfigDir returns the config directory path
func getConfigDir() string {
	if dir := os.Getenv("LEDIT_CONFIG"); dir != "" {
		return dir
	}

	// Try XDG_CONFIG_HOME
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "ledit")
	}

	// Use user home directory
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		// Fallback for Android or special environments
		return "/data/data/com.termux/files/home/.ledit"
	}

	return filepath.Join(homeDir, ".ledit")
}

// loadInstances loads running instances from config
func loadInstances() (map[string]InstanceInfo, error) {
	instancesFile := filepath.Join(getConfigDir(), "instances.json")

	data, err := os.ReadFile(instancesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]InstanceInfo), nil // No instances file yet
		}
		return nil, err
	}

	instances := make(map[string]InstanceInfo)
	if len(data) == 0 {
		return instances, nil
	}

	if err := json.Unmarshal(data, &instances); err != nil {
		return nil, err
	}

	return instances, nil
}

// saveInstances persists running instances to config.
func saveInstances(instances map[string]InstanceInfo) error {
	if err := os.MkdirAll(getConfigDir(), 0755); err != nil {
		return err
	}

	instancesFile := filepath.Join(getConfigDir(), "instances.json")
	data, err := json.MarshalIndent(instances, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := instancesFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpFile, instancesFile)
}

// cleanStaleInstances removes instances that haven't pinged recently
func cleanStaleInstances(instances map[string]InstanceInfo, staleThreshold time.Time) {
	for id, info := range instances {
		if info.LastPing.Before(staleThreshold) {
			delete(instances, id)
		}
	}
}
