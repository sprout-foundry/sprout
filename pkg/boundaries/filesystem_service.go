package boundaries

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"
)

// FileSystemService defines the interface for file system operations
// This provides a clean boundary between file system access and business logic
type FileSystemService interface {
	// File Operations
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error
	AppendFile(ctx context.Context, path string, data []byte) error
	DeleteFile(ctx context.Context, path string) error
	FileExists(ctx context.Context, path string) bool
	GetFileInfo(ctx context.Context, path string) (*FileInfo, error)
	CopyFile(ctx context.Context, src, dst string) error
	MoveFile(ctx context.Context, src, dst string) error
	CreateTempFile(ctx context.Context, pattern string) (*os.File, error)

	// Directory Operations
	ReadDir(ctx context.Context, path string) ([]*FileInfo, error)
	CreateDir(ctx context.Context, path string, perm os.FileMode) error
	CreateDirAll(ctx context.Context, path string, perm os.FileMode) error
	RemoveDir(ctx context.Context, path string) error
	RemoveDirAll(ctx context.Context, path string) error
	DirExists(ctx context.Context, path string) bool

	// Path Operations
	Abs(ctx context.Context, path string) (string, error)
	Rel(ctx context.Context, basepath, targetpath string) (string, error)
	Base(ctx context.Context, path string) string
	Dir(ctx context.Context, path string) string
	Ext(ctx context.Context, path string) string
	Join(ctx context.Context, elem ...string) string
	Match(ctx context.Context, pattern, name string) (bool, error)
	Glob(ctx context.Context, pattern string) ([]string, error)

	// Permissions
	Chmod(ctx context.Context, path string, mode os.FileMode) error
	Chown(ctx context.Context, path string, uid, gid int) error

	// Working Directory
	Getwd(ctx context.Context) (string, error)
	Chdir(ctx context.Context, dir string) error

	// Watch Operations
	WatchFile(ctx context.Context, path string, callback FileChangeCallback) (WatchHandle, error)
	WatchDir(ctx context.Context, path string, callback FileChangeCallback) (WatchHandle, error)

	// Stream Operations
	OpenFile(ctx context.Context, path string, flag int, perm os.FileMode) (*os.File, error)
	Create(ctx context.Context, path string, perm os.FileMode) (*os.File, error)
	Open(ctx context.Context, path string) (*os.File, error)

	// Utility Operations
	GetFileSize(ctx context.Context, path string) (int64, error)
	GetModTime(ctx context.Context, path string) (time.Time, error)
	IsDir(ctx context.Context, path string) bool
	IsRegular(ctx context.Context, path string) bool
}

// FileInfo represents file information with additional metadata
type FileInfo struct {
	Name       string      // base name of the file
	Size       int64       // length in bytes
	Mode       os.FileMode // file mode bits
	ModTime    time.Time   // modification time
	IsDir      bool        // abbreviation for Mode().IsDir()
	Type       string      // file type (regular, directory, symlink, etc.)
	Readable   bool        // whether the file is readable
	Writable   bool        // whether the file is writable
	Executable bool        // whether the file is executable
	Owner      string      // file owner (if available)
	Group      string      // file group (if available)
}

// FileChangeCallback is called when a watched file changes
type FileChangeCallback func(event *FileChangeEvent)

// FileChangeEvent represents a file system change event
type FileChangeEvent struct {
	Path      string
	Operation string // "create", "write", "remove", "rename", "chmod"
	OldPath   string // for rename operations
	Time      time.Time
	FileInfo  *FileInfo
}

// WatchHandle represents a file system watch handle
type WatchHandle interface {
	Stop() error
	IsActive() bool
}

// FileSystemServiceFactory creates file system service instances
type FileSystemServiceFactory interface {
	CreateFileSystemService() FileSystemService
	CreateFileSystemServiceWithConfig(config *FileSystemConfig) FileSystemService
}

// FileSystemConfig contains configuration for the file system service
type FileSystemConfig struct {
	AllowSymlinks   bool
	MaxFileSize     int64
	AllowedPaths    []string
	BlockedPaths    []string
	ReadOnlyPaths   []string
	DefaultFileMode os.FileMode
	DefaultDirMode  os.FileMode
	EnableWatch     bool
	WatchBufferSize int
	EnableCaching   bool
	CacheTTL        time.Duration
}

// DefaultFileSystemService provides a default implementation of FileSystemService
type DefaultFileSystemService struct {
	config  *FileSystemConfig
	watches map[string]WatchHandle
	watchID int
}

// NewDefaultFileSystemService creates a new default file system service
func NewDefaultFileSystemService(config *FileSystemConfig) *DefaultFileSystemService {
	if config == nil {
		config = &FileSystemConfig{
			AllowSymlinks:   false,
			MaxFileSize:     100 * 1024 * 1024, // 100MB
			DefaultFileMode: 0644,
			DefaultDirMode:  0755,
			EnableWatch:     false,
			EnableCaching:   false,
		}
	}

	return &DefaultFileSystemService{
		config:  config,
		watches: make(map[string]WatchHandle),
		watchID: 1,
	}
}

// ReadFile implements FileSystemService.ReadFile
func (fs *DefaultFileSystemService) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if !fs.isPathAllowed(path) {
		return nil, &os.PathError{Op: "read", Path: path, Err: os.ErrPermission}
	}

	return os.ReadFile(path)
}

// WriteFile implements FileSystemService.WriteFile
func (fs *DefaultFileSystemService) WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "write", Path: path, Err: os.ErrPermission}
	}

	if fs.isReadOnlyPath(path) {
		return &os.PathError{Op: "write", Path: path, Err: os.ErrPermission}
	}

	if fs.config.MaxFileSize > 0 && int64(len(data)) > fs.config.MaxFileSize {
		return &os.PathError{Op: "write", Path: path, Err: os.ErrInvalid}
	}

	return os.WriteFile(path, data, perm)
}

// AppendFile implements FileSystemService.AppendFile
func (fs *DefaultFileSystemService) AppendFile(ctx context.Context, path string, data []byte) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "append", Path: path, Err: os.ErrPermission}
	}

	if fs.isReadOnlyPath(path) {
		return &os.PathError{Op: "append", Path: path, Err: os.ErrPermission}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(data)
	return err
}

// DeleteFile implements FileSystemService.DeleteFile
func (fs *DefaultFileSystemService) DeleteFile(ctx context.Context, path string) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "delete", Path: path, Err: os.ErrPermission}
	}

	if fs.isReadOnlyPath(path) {
		return &os.PathError{Op: "delete", Path: path, Err: os.ErrPermission}
	}

	return os.Remove(path)
}

// FileExists implements FileSystemService.FileExists
func (fs *DefaultFileSystemService) FileExists(ctx context.Context, path string) bool {
	if !fs.isPathAllowed(path) {
		return false
	}

	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// GetFileInfo implements FileSystemService.GetFileInfo
func (fs *DefaultFileSystemService) GetFileInfo(ctx context.Context, path string) (*FileInfo, error) {
	if !fs.isPathAllowed(path) {
		return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	fileInfo := &FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}

	// Determine file type
	if info.IsDir() {
		fileInfo.Type = "directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		fileInfo.Type = "symlink"
	} else {
		fileInfo.Type = "regular"
	}

	// Check permissions (simplified)
	fileInfo.Readable = info.Mode()&0400 != 0
	fileInfo.Writable = info.Mode()&0200 != 0
	fileInfo.Executable = info.Mode()&0100 != 0

	return fileInfo, nil
}

// CopyFile implements FileSystemService.CopyFile
func (fs *DefaultFileSystemService) CopyFile(ctx context.Context, src, dst string) error {
	if !fs.isPathAllowed(src) || !fs.isPathAllowed(dst) {
		return &os.PathError{Op: "copy", Path: src, Err: os.ErrPermission}
	}

	if fs.isReadOnlyPath(dst) {
		return &os.PathError{Op: "copy", Path: dst, Err: os.ErrPermission}
	}

	return fs.copyFile(src, dst)
}

// MoveFile implements FileSystemService.MoveFile
func (fs *DefaultFileSystemService) MoveFile(ctx context.Context, src, dst string) error {
	if !fs.isPathAllowed(src) || !fs.isPathAllowed(dst) {
		return &os.PathError{Op: "move", Path: src, Err: os.ErrPermission}
	}

	if fs.isReadOnlyPath(dst) {
		return &os.PathError{Op: "move", Path: dst, Err: os.ErrPermission}
	}

	return os.Rename(src, dst)
}

// CreateTempFile implements FileSystemService.CreateTempFile
func (fs *DefaultFileSystemService) CreateTempFile(ctx context.Context, pattern string) (*os.File, error) {
	return os.CreateTemp("", pattern)
}

// ReadDir implements FileSystemService.ReadDir
func (fs *DefaultFileSystemService) ReadDir(ctx context.Context, path string) ([]*FileInfo, error) {
	if !fs.isPathAllowed(path) {
		return nil, &os.PathError{Op: "readdir", Path: path, Err: os.ErrPermission}
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var fileInfos []*FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileInfo := &FileInfo{
			Name:    info.Name(),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
			IsDir:   info.IsDir(),
		}

		if info.IsDir() {
			fileInfo.Type = "directory"
		} else if info.Mode()&os.ModeSymlink != 0 {
			fileInfo.Type = "symlink"
		} else {
			fileInfo.Type = "regular"
		}

		fileInfos = append(fileInfos, fileInfo)
	}

	return fileInfos, nil
}

// CreateDir implements FileSystemService.CreateDir
func (fs *DefaultFileSystemService) CreateDir(ctx context.Context, path string, perm os.FileMode) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "mkdir", Path: path, Err: os.ErrPermission}
	}

	return os.Mkdir(path, perm)
}

// CreateDirAll implements FileSystemService.CreateDirAll
func (fs *DefaultFileSystemService) CreateDirAll(ctx context.Context, path string, perm os.FileMode) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "mkdirall", Path: path, Err: os.ErrPermission}
	}

	return os.MkdirAll(path, perm)
}

// RemoveDir implements FileSystemService.RemoveDir
func (fs *DefaultFileSystemService) RemoveDir(ctx context.Context, path string) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "rmdir", Path: path, Err: os.ErrPermission}
	}

	return os.Remove(path)
}

// RemoveDirAll implements FileSystemService.RemoveDirAll
func (fs *DefaultFileSystemService) RemoveDirAll(ctx context.Context, path string) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "rmdirall", Path: path, Err: os.ErrPermission}
	}

	return os.RemoveAll(path)
}

// DirExists implements FileSystemService.DirExists
func (fs *DefaultFileSystemService) DirExists(ctx context.Context, path string) bool {
	if !fs.isPathAllowed(path) {
		return false
	}

	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Path operations - these are mostly pass-through to standard library

// Abs implements FileSystemService.Abs
func (fs *DefaultFileSystemService) Abs(ctx context.Context, path string) (string, error) {
	return filepath.Abs(path)
}

// Rel implements FileSystemService.Rel
func (fs *DefaultFileSystemService) Rel(ctx context.Context, basepath, targetpath string) (string, error) {
	return filepath.Rel(basepath, targetpath)
}

// Base implements FileSystemService.Base
func (fs *DefaultFileSystemService) Base(ctx context.Context, path string) string {
	return filepath.Base(path)
}

// Dir implements FileSystemService.Dir
func (fs *DefaultFileSystemService) Dir(ctx context.Context, path string) string {
	return filepath.Dir(path)
}

// Ext implements FileSystemService.Ext
func (fs *DefaultFileSystemService) Ext(ctx context.Context, path string) string {
	return filepath.Ext(path)
}

// Join implements FileSystemService.Join
func (fs *DefaultFileSystemService) Join(ctx context.Context, elem ...string) string {
	return filepath.Join(elem...)
}

// Match implements FileSystemService.Match
func (fs *DefaultFileSystemService) Match(ctx context.Context, pattern, name string) (bool, error) {
	return filepath.Match(pattern, name)
}

// Glob implements FileSystemService.Glob
func (fs *DefaultFileSystemService) Glob(ctx context.Context, pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// Permissions

// Chmod implements FileSystemService.Chmod
func (fs *DefaultFileSystemService) Chmod(ctx context.Context, path string, mode os.FileMode) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "chmod", Path: path, Err: os.ErrPermission}
	}

	return os.Chmod(path, mode)
}

// Chown implements FileSystemService.Chown
func (fs *DefaultFileSystemService) Chown(ctx context.Context, path string, uid, gid int) error {
	if !fs.isPathAllowed(path) {
		return &os.PathError{Op: "chown", Path: path, Err: os.ErrPermission}
	}

	return os.Chown(path, uid, gid)
}

// Working Directory

// Getwd implements FileSystemService.Getwd
func (fs *DefaultFileSystemService) Getwd(ctx context.Context) (string, error) {
	return os.Getwd()
}

// Chdir implements FileSystemService.Chdir
func (fs *DefaultFileSystemService) Chdir(ctx context.Context, dir string) error {
	if !fs.isPathAllowed(dir) {
		return &os.PathError{Op: "chdir", Path: dir, Err: os.ErrPermission}
	}

	return os.Chdir(dir)
}

// Watch Operations (simplified implementation)

// WatchFile implements FileSystemService.WatchFile
func (fs *DefaultFileSystemService) WatchFile(ctx context.Context, path string, callback FileChangeCallback) (WatchHandle, error) {
	// Simplified implementation - in a real system you'd use fsnotify
	if !fs.config.EnableWatch {
		return nil, os.ErrInvalid
	}

	// Return a dummy handle
	handleID := fs.watchID
	fs.watchID++

	return &DummyWatchHandle{id: handleID, active: true}, nil
}

// WatchDir implements FileSystemService.WatchDir
func (fs *DefaultFileSystemService) WatchDir(ctx context.Context, path string, callback FileChangeCallback) (WatchHandle, error) {
	return fs.WatchFile(ctx, path, callback)
}

// Stream Operations

// OpenFile implements FileSystemService.OpenFile
func (fs *DefaultFileSystemService) OpenFile(ctx context.Context, path string, flag int, perm os.FileMode) (*os.File, error) {
	if !fs.isPathAllowed(path) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
	}

	return os.OpenFile(path, flag, perm)
}

// Create implements FileSystemService.Create
func (fs *DefaultFileSystemService) Create(ctx context.Context, path string, perm os.FileMode) (*os.File, error) {
	if !fs.isPathAllowed(path) {
		return nil, &os.PathError{Op: "create", Path: path, Err: os.ErrPermission}
	}

	if fs.isReadOnlyPath(path) {
		return nil, &os.PathError{Op: "create", Path: path, Err: os.ErrPermission}
	}

	return os.Create(path)
}

// Open implements FileSystemService.Open
func (fs *DefaultFileSystemService) Open(ctx context.Context, path string) (*os.File, error) {
	if !fs.isPathAllowed(path) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
	}

	return os.Open(path)
}

// Utility Operations

// GetFileSize implements FileSystemService.GetFileSize
func (fs *DefaultFileSystemService) GetFileSize(ctx context.Context, path string) (int64, error) {
	info, err := fs.GetFileInfo(ctx, path)
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

// GetModTime implements FileSystemService.GetModTime
func (fs *DefaultFileSystemService) GetModTime(ctx context.Context, path string) (time.Time, error) {
	info, err := fs.GetFileInfo(ctx, path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime, nil
}

// IsDir implements FileSystemService.IsDir
func (fs *DefaultFileSystemService) IsDir(ctx context.Context, path string) bool {
	info, err := fs.GetFileInfo(ctx, path)
	return err == nil && info.IsDir
}

// IsRegular implements FileSystemService.IsRegular
func (fs *DefaultFileSystemService) IsRegular(ctx context.Context, path string) bool {
	info, err := fs.GetFileInfo(ctx, path)
	return err == nil && info.Type == "regular"
}

// Helper methods

// isPathAllowed checks if a path is allowed based on configuration
func (fs *DefaultFileSystemService) isPathAllowed(path string) bool {
	if len(fs.config.AllowedPaths) == 0 {
		// If no allowed paths specified, check blocked paths
		for _, blocked := range fs.config.BlockedPaths {
			if len(path) >= len(blocked) && path[:len(blocked)] == blocked {
				return false
			}
		}
		return true
	}

	// Check if path matches any allowed path
	for _, allowed := range fs.config.AllowedPaths {
		if len(path) >= len(allowed) && path[:len(allowed)] == allowed {
			return true
		}
	}

	return false
}

// isReadOnlyPath checks if a path is read-only
func (fs *DefaultFileSystemService) isReadOnlyPath(path string) bool {
	for _, readOnly := range fs.config.ReadOnlyPaths {
		if len(path) >= len(readOnly) && path[:len(readOnly)] == readOnly {
			return true
		}
	}
	return false
}

// copyFile copies a file from src to dst
func (fs *DefaultFileSystemService) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	// Copy file permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	return os.Chmod(dst, srcInfo.Mode())
}

// DummyWatchHandle provides a dummy implementation of WatchHandle
type DummyWatchHandle struct {
	id     int
	active bool
}

// Stop implements WatchHandle.Stop
func (h *DummyWatchHandle) Stop() error {
	h.active = false
	return nil
}

// IsActive implements WatchHandle.IsActive
func (h *DummyWatchHandle) IsActive() bool {
	return h.active
}
