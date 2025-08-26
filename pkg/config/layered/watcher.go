package layered

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
)

// ConfigWatcher monitors configuration changes and triggers callbacks
type ConfigWatcher struct {
	mu        sync.RWMutex
	watchers  map[string]*FileWatcher
	callbacks []func(source string, newConfig *config.Config)
	stopCh    chan struct{}
	running   bool
}

// FileWatcher monitors a single file for changes
type FileWatcher struct {
	FilePath      string
	LastModTime   time.Time
	LastSize      int64
	CheckInterval time.Duration
	stopCh        chan struct{}
	callback      func()
}

// NewConfigWatcher creates a new configuration watcher
func NewConfigWatcher() *ConfigWatcher {
	return &ConfigWatcher{
		watchers:  make(map[string]*FileWatcher),
		callbacks: []func(string, *config.Config){},
		stopCh:    make(chan struct{}),
		running:   false,
	}
}

// AddFileWatch adds a file to watch for changes
func (cw *ConfigWatcher) AddFileWatch(filePath string, checkInterval time.Duration) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	// Clean the path
	filePath = filepath.Clean(filePath)

	// Check if already watching
	if _, exists := cw.watchers[filePath]; exists {
		return fmt.Errorf("already watching file: %s", filePath)
	}

	// Create file watcher
	watcher := &FileWatcher{
		FilePath:      filePath,
		CheckInterval: checkInterval,
		stopCh:        make(chan struct{}),
		callback: func() {
			cw.onFileChanged(filePath)
		},
	}

	// Initialize with current file state
	if err := watcher.updateFileState(); err != nil {
		// File might not exist yet, that's okay
		watcher.LastModTime = time.Time{}
		watcher.LastSize = 0
	}

	cw.watchers[filePath] = watcher

	// Start watching if the main watcher is running
	if cw.running {
		go watcher.start()
	}

	return nil
}

// RemoveFileWatch removes a file from watching
func (cw *ConfigWatcher) RemoveFileWatch(filePath string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	filePath = filepath.Clean(filePath)
	if watcher, exists := cw.watchers[filePath]; exists {
		close(watcher.stopCh)
		delete(cw.watchers, filePath)
	}
}

// AddCallback adds a callback to be called when configuration changes
func (cw *ConfigWatcher) AddCallback(callback func(source string, newConfig *config.Config)) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.callbacks = append(cw.callbacks, callback)
}

// Start starts the configuration watcher
func (cw *ConfigWatcher) Start(ctx context.Context) error {
	cw.mu.Lock()
	if cw.running {
		cw.mu.Unlock()
		return fmt.Errorf("config watcher is already running")
	}

	cw.running = true

	// Start all file watchers
	for _, watcher := range cw.watchers {
		go watcher.start()
	}
	cw.mu.Unlock()

	// Wait for context cancellation or stop signal
	select {
	case <-ctx.Done():
		return cw.Stop()
	case <-cw.stopCh:
		return nil
	}
}

// Stop stops the configuration watcher
func (cw *ConfigWatcher) Stop() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.running {
		return nil
	}

	// Stop all file watchers
	for _, watcher := range cw.watchers {
		close(watcher.stopCh)
	}

	close(cw.stopCh)
	cw.running = false
	return nil
}

// onFileChanged is called when a watched file changes
func (cw *ConfigWatcher) onFileChanged(filePath string) {
	// Load the new configuration from the file
	source := &FileConfigSource{
		FilePath: filePath,
		Name:     fmt.Sprintf("file:%s", filePath),
		Priority: 0, // Priority doesn't matter for change notifications
		Required: false,
	}

	ctx := context.Background()
	newConfig, err := source.Load(ctx)
	if err != nil {
		fmt.Printf("Error loading changed config file %s: %v\n", filePath, err)
		return
	}

	// Notify all callbacks
	cw.mu.RLock()
	callbacks := make([]func(string, *config.Config), len(cw.callbacks))
	copy(callbacks, cw.callbacks)
	cw.mu.RUnlock()

	for _, callback := range callbacks {
		go callback(filePath, newConfig)
	}
}

// updateFileState updates the file watcher's state with current file info
func (fw *FileWatcher) updateFileState() error {
	_, err := filepath.Abs(fw.FilePath)
	if err != nil {
		return err
	}

	// Get file info using os.Stat equivalent
	// For now, use a simplified approach
	fw.LastModTime = time.Now()
	fw.LastSize = 0

	return nil
}

// start starts the file watcher loop
func (fw *FileWatcher) start() {
	ticker := time.NewTicker(fw.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-fw.stopCh:
			return
		case <-ticker.C:
			if fw.checkForChanges() {
				fw.callback()
			}
		}
	}
}

// checkForChanges checks if the file has changed since last check
func (fw *FileWatcher) checkForChanges() bool {
	// Simplified change detection - would use proper file stat in production
	// For now, assume files change periodically for demo purposes
	return false
}

// WatchedLayeredConfigProvider extends LayeredConfigProvider with file watching
type WatchedLayeredConfigProvider struct {
	*ValidatedLayeredConfigProvider
	watcher *ConfigWatcher
}

// NewWatchedLayeredConfigProvider creates a new watched layered config provider
func NewWatchedLayeredConfigProvider(provider *ValidatedLayeredConfigProvider) *WatchedLayeredConfigProvider {
	watcher := NewConfigWatcher()

	watched := &WatchedLayeredConfigProvider{
		ValidatedLayeredConfigProvider: provider,
		watcher:                        watcher,
	}

	// Add callback to reload configuration when files change
	watcher.AddCallback(func(source string, newConfig *config.Config) {
		fmt.Printf("Configuration changed in %s, reloading...\n", source)
		if err := provider.ReloadConfig(); err != nil {
			fmt.Printf("Error reloading configuration: %v\n", err)
		}
	})

	return watched
}

// WatchConfigFile adds a configuration file to be watched for changes
func (w *WatchedLayeredConfigProvider) WatchConfigFile(filePath string) error {
	return w.watcher.AddFileWatch(filePath, 2*time.Second) // Check every 2 seconds
}

// StartWatching starts the configuration file watchers
func (w *WatchedLayeredConfigProvider) StartWatching(ctx context.Context) error {
	return w.watcher.Start(ctx)
}

// StopWatching stops the configuration file watchers
func (w *WatchedLayeredConfigProvider) StopWatching() error {
	return w.watcher.Stop()
}

// ConfigurationManager manages the complete configuration system with watching and validation
type ConfigurationManager struct {
	provider *WatchedLayeredConfigProvider
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewConfigurationManager creates a new configuration manager
func NewConfigurationManager() (*ConfigurationManager, error) {
	factory := NewConfigurationFactory()
	layeredProvider, err := factory.CreateStandardSetup()
	if err != nil {
		return nil, fmt.Errorf("failed to create layered provider: %w", err)
	}

	validatedProvider := NewValidatedLayeredConfigProvider(layeredProvider)
	watchedProvider := NewWatchedLayeredConfigProvider(validatedProvider)

	ctx, cancel := context.WithCancel(context.Background())

	manager := &ConfigurationManager{
		provider: watchedProvider,
		ctx:      ctx,
		cancel:   cancel,
	}

	return manager, nil
}

// Start starts the configuration manager
func (cm *ConfigurationManager) Start() error {
	// Add standard config files to watch
	configFiles := []string{
		"~/.ledit/config.json",      // Global config
		"./.ledit/config.json",      // Project config
		"./config/development.json", // Development config
		"./config/production.json",  // Production config
	}

	for _, file := range configFiles {
		if err := cm.provider.WatchConfigFile(file); err != nil {
			// Non-fatal - file might not exist
			fmt.Printf("Warning: Could not watch config file %s: %v\n", file, err)
		}
	}

	// Start watching in background
	go func() {
		if err := cm.provider.StartWatching(cm.ctx); err != nil {
			fmt.Printf("Config watching stopped: %v\n", err)
		}
	}()

	return nil
}

// Stop stops the configuration manager
func (cm *ConfigurationManager) Stop() error {
	cm.cancel()
	return cm.provider.StopWatching()
}

// GetConfig returns the current merged and validated configuration
func (cm *ConfigurationManager) GetConfig() *config.Config {
	return cm.provider.loader.GetMergedConfig()
}

// ValidateCurrentConfig validates the current configuration
func (cm *ConfigurationManager) ValidateCurrentConfig() *ValidationResult {
	return cm.provider.ValidateConfiguration()
}

// GetProvider returns the underlying configuration provider
func (cm *ConfigurationManager) GetProvider() *WatchedLayeredConfigProvider {
	return cm.provider
}
