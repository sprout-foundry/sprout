//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

const instanceLockTimeout = 5 * time.Second

// InstanceInfo represents a running sprout instance
type InstanceInfo struct {
	ID         string    `json:"id"`
	Port       int       `json:"port"`
	PID        int       `json:"pid"`
	StartTime  time.Time `json:"start_time"`
	WorkingDir string    `json:"working_dir"`
	LastPing   time.Time `json:"last_ping"`
	SessionID  string    `json:"session_id,omitempty"`
}

// getConfigDir returns the config directory path, using the same canonical
// resolution as configuration.GetConfigDir() so that all subsystems (config,
// providers, API keys, instances) share a single directory.
func getConfigDir() string {
	dir, err := configuration.GetConfigDir()
	if err != nil {
		// Fallback for Android/Termux or environments where home is unavailable
		if homeDir := os.Getenv("HOME"); homeDir != "" {
			return filepath.Join(homeDir, ".config", "sprout")
		}
		return "/data/data/com.termux/files/home/.config/sprout"
	}
	return dir
}

// loadInstances loads running instances from config
func loadInstances() (map[string]InstanceInfo, error) {
	instancesFile := filepath.Join(getConfigDir(), "instances.json")

	data, err := os.ReadFile(instancesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]InstanceInfo), nil // No instances file yet
		}
		return nil, fmt.Errorf("failed to read instances file: %w", err)
	}

	instances := make(map[string]InstanceInfo)
	if len(data) == 0 {
		return instances, nil
	}

	if err := json.Unmarshal(data, &instances); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instances JSON: %w", err)
	}

	return instances, nil
}

// saveInstances persists running instances to config.
// Uses a unique temp file per write to avoid race conditions when multiple
// sprout processes write heartbeats to the same file simultaneously.
func saveInstances(instances map[string]InstanceInfo) error {
	configDir := getConfigDir()
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir for instances: %w", err)
	}

	instancesFile := filepath.Join(configDir, "instances.json")
	data, err := json.MarshalIndent(instances, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal instances data: %w", err)
	}

	// Use a process-specific temp file to avoid races with other sprout
	// instances that are also writing heartbeats concurrently.
	tmpFile := instancesFile + fmt.Sprintf(".tmp.%d", os.Getpid())
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write instances temp file: %w", err)
	}
	if err := os.Rename(tmpFile, instancesFile); err != nil {
		// Clean up orphaned temp file on rename failure.
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to rename instances temp file: %w", err)
	}
	return nil
}

// withInstanceLock acquires an exclusive interprocess lock on instances.json,
// loads the current contents, hands the map to the mutate function for
// modification, and persists the result. The flock prevents last-writer-wins
// data loss when multiple sprout processes heartbeat concurrently.
func withInstanceLock(ctx context.Context, mutate func(instances map[string]InstanceInfo) error) error {
	configDir := getConfigDir()
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir for instances: %w", err)
	}
	instancesFile := filepath.Join(configDir, "instances.json")
	// Ensure the file exists so flock can open it.
	if err := touchFile(instancesFile); err != nil {
		return fmt.Errorf("failed to touch instances file: %w", err)
	}

	lock := flock.New(instancesFile + ".lock")
	locked, err := lock.TryLockContext(ctx, instanceLockTimeout)
	if err != nil {
		return fmt.Errorf("failed to acquire instances lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("timed out acquiring instances lock")
	}
	defer func() { _ = lock.Unlock() }()

	instances, err := loadInstances()
	if err != nil {
		instances = make(map[string]InstanceInfo)
	}
	if err := mutate(instances); err != nil {
		return err
	}
	return saveInstances(instances)
}

func touchFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

// cleanStaleInstances removes instances that haven't pinged recently
func cleanStaleInstances(instances map[string]InstanceInfo, staleThreshold time.Time) {
	for id, info := range instances {
		if info.LastPing.Before(staleThreshold) {
			delete(instances, id)
		}
	}
}
