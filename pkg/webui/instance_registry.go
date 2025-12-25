package webui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// InstanceInfo represents a running ledit instance
type InstanceInfo struct {
	ID         string    `json:"id"` // Unique instance ID
	Port       int       `json:"port"`
	PID        int       `json:"pid"`
	StartTime  time.Time `json:"start_time"`
	WorkingDir string    `json:"working_dir"`
	LastPing   time.Time `json:"last_ping"`
}

// InstanceRegistry manages registration of running instances
type InstanceRegistry struct {
	instancesFile string
	mutex         sync.RWMutex
	currentID     string
}

// NewInstanceRegistry creates a new instance registry
func NewInstanceRegistry(configDir string) (*InstanceRegistry, error) {
	// Ensure config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	instancesFile := filepath.Join(configDir, "instances.json")

	return &InstanceRegistry{
		instancesFile: instancesFile,
	}, nil
}

// RegisterInstance registers this instance as running
func (ir *InstanceRegistry) RegisterInstance(port int, pid int, workingDir string) (string, error) {
	ir.mutex.Lock()
	defer ir.mutex.Unlock()

	// Generate unique instance ID
	instanceID := fmt.Sprintf("ledit_%d_%d", time.Now().UnixNano(), port)
	ir.currentID = instanceID

	// Load existing instances
	instances, err := ir.loadInstances()
	if err != nil {
		return "", err
	}

	// Get current working directory if not provided
	if workingDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workingDir = wd
		}
	}

	now := time.Now()

	// Add or update this instance
	instances[instanceID] = InstanceInfo{
		ID:         instanceID,
		Port:       port,
		PID:        pid,
		StartTime:  now,
		WorkingDir: workingDir,
		LastPing:   now,
	}

	// Remove stale instances (inactive for more than 5 minutes)
	ir.cleanupStaleInstances(instances, now.Add(-5*time.Minute))

	// Save instances
	if err := ir.saveInstances(instances); err != nil {
		return "", err
	}

	return instanceID, nil
}

// UnregisterInstance removes this instance from the registry
func (ir *InstanceRegistry) UnregisterInstance() error {
	ir.mutex.Lock()
	defer ir.mutex.Unlock()

	if ir.currentID == "" {
		return nil // Nothing to unregister
	}

	instances, err := ir.loadInstances()
	if err != nil {
		return err
	}

	delete(instances, ir.currentID)
	ir.currentID = ""

	return ir.saveInstances(instances)
}

// Ping updates the last ping time for this instance
func (ir *InstanceRegistry) Ping() error {
	ir.mutex.Lock()
	defer ir.mutex.Unlock()

	if ir.currentID == "" {
		return nil
	}

	instances, err := ir.loadInstances()
	if err != nil {
		return err
	}

	if info, exists := instances[ir.currentID]; exists {
		info.LastPing = time.Now()
		instances[ir.currentID] = info
		return ir.saveInstances(instances)
	}

	return nil
}

// ListInstances returns all registered instances
func (ir *InstanceRegistry) ListInstances() ([]InstanceInfo, error) {
	ir.mutex.RLock()
	defer ir.mutex.RUnlock()

	now := time.Now()

	instances, err := ir.loadInstances()
	if err != nil {
		return nil, err
	}

	// Clean up stale instances
	ir.cleanupStaleInstances(instances, now.Add(-5*time.Minute))

	// Convert to sorted list
	result := make([]InstanceInfo, 0, len(instances))
	for _, info := range instances {
		result = append(result, info)
	}

	// Sort by start time (newest first)
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].StartTime.Before(result[j].StartTime) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

// GetInstance returns info about a specific instance by port
func (ir *InstanceRegistry) GetInstanceByPort(port int) (*InstanceInfo, error) {
	ir.mutex.RLock()
	defer ir.mutex.RUnlock()

	instances, err := ir.loadInstances()
	if err != nil {
		return nil, err
	}

	for _, info := range instances {
		if info.Port == port {
			return &info, nil
		}
	}

	return nil, nil
}

// loadInstances loads instances from file
func (ir *InstanceRegistry) loadInstances() (map[string]InstanceInfo, error) {
	instances := make(map[string]InstanceInfo)

	data, err := os.ReadFile(ir.instancesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return instances, nil // No instances file yet
		}
		return nil, fmt.Errorf("failed to read instances file: %w", err)
	}

	if len(data) == 0 {
		return instances, nil
	}

	if err := json.Unmarshal(data, &instances); err != nil {
		return nil, fmt.Errorf("failed to parse instances file: %w", err)
	}

	return instances, nil
}

// saveInstances saves instances to file
func (ir *InstanceRegistry) saveInstances(instances map[string]InstanceInfo) error {
	data, err := json.MarshalIndent(instances, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal instances: %w", err)
	}

	if err := os.WriteFile(ir.instancesFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write instances file: %w", err)
	}

	return nil
}

// cleanupStaleInstances removes instances that haven't pinged recently
func (ir *InstanceRegistry) cleanupStaleInstances(instances map[string]InstanceInfo, before time.Time) {
	for id, info := range instances {
		if info.LastPing.Before(before) {
			delete(instances, id)
		}
	}
}
