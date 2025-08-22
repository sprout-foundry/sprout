package boundaries

import (
	"context"
	"fmt"
	"time"
)

// NewValidationError creates a validation error
func NewValidationError(field, reason string) error {
	return fmt.Errorf("validation failed for %s: %s", field, reason)
}

// Repository defines the interface for data access operations
// This provides a clean boundary between data persistence and business logic
type Repository interface {
	// Save stores an entity
	Save(ctx context.Context, entity Entity) error

	// Get retrieves an entity by ID
	Get(ctx context.Context, id string) (Entity, error)

	// GetByQuery retrieves entities matching a query
	GetByQuery(ctx context.Context, query Query) ([]Entity, error)

	// Update updates an existing entity
	Update(ctx context.Context, entity Entity) error

	// Delete removes an entity
	Delete(ctx context.Context, id string) error

	// Exists checks if an entity exists
	Exists(ctx context.Context, id string) (bool, error)

	// Count returns the count of entities matching a query
	Count(ctx context.Context, query Query) (int64, error)

	// List returns a paginated list of entities
	List(ctx context.Context, pagination Pagination) ([]Entity, error)
}

// Entity represents a data entity that can be stored
type Entity interface {
	GetID() string
	GetType() string
	GetCreatedAt() time.Time
	GetUpdatedAt() time.Time
	SetUpdatedAt(time.Time)
	Validate() error
}

// Query represents a query for retrieving entities
type Query struct {
	Type       string                 `json:"type"`        // Entity type
	Filters    map[string]interface{} `json:"filters"`     // Field filters
	SortBy     string                 `json:"sort_by"`     // Sort field
	SortOrder  string                 `json:"sort_order"`  // "asc" or "desc"
	Limit      int                    `json:"limit"`       // Max results
	Offset     int                    `json:"offset"`      // Pagination offset
	SearchText string                 `json:"search_text"` // Full-text search
}

// Pagination represents pagination parameters
type Pagination struct {
	Page     int `json:"page"`      // Page number (1-based)
	PageSize int `json:"page_size"` // Items per page
	Limit    int `json:"limit"`     // Maximum items to return
	Offset   int `json:"offset"`    // Starting offset
}

// QueryResult contains the results of a query operation
type QueryResult struct {
	Entities   []Entity `json:"entities"`
	Total      int64    `json:"total"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	TotalPages int      `json:"total_pages"`
	HasNext    bool     `json:"has_next"`
	HasPrev    bool     `json:"has_prev"`
}

// RepositoryFactory creates repository instances
type RepositoryFactory interface {
	CreateRepository(entityType string) Repository
	CreateRepositoryWithConfig(entityType string, config *RepositoryConfig) Repository
}

// RepositoryConfig contains configuration for repositories
type RepositoryConfig struct {
	DatabaseURL    string
	CollectionName string
	CacheEnabled   bool
	CacheTTL       time.Duration
	BatchSize      int
	Timeout        time.Duration
	RetryCount     int
}

// Transaction represents a database transaction
type Transaction interface {
	Commit() error
	Rollback() error
	IsActive() bool
}

// TransactionManager manages database transactions
type TransactionManager interface {
	BeginTransaction(ctx context.Context) (Transaction, error)
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// CacheManager manages caching operations
type CacheManager interface {
	Get(ctx context.Context, key string) (interface{}, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) bool
	Clear(ctx context.Context) error
	GetStats() *CacheStats
}

// CacheStats contains cache statistics
type CacheStats struct {
	Hits      int64   `json:"hits"`
	Misses    int64   `json:"misses"`
	HitRate   float64 `json:"hit_rate"`
	Items     int64   `json:"items"`
	SizeBytes int64   `json:"size_bytes"`
	Evictions int64   `json:"evictions"`
}

// MigrationManager handles database migrations
type MigrationManager interface {
	CreateMigration(name string, up, down func() error) error
	RunMigrations(ctx context.Context) error
	RollbackMigration(ctx context.Context) error
	GetMigrationStatus() ([]MigrationInfo, error)
}

// MigrationInfo contains information about a migration
type MigrationInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	AppliedAt time.Time `json:"applied_at"`
	Checksum  string    `json:"checksum"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID         string                 `json:"id"`
	EntityID   string                 `json:"entity_id"`
	EntityType string                 `json:"entity_type"`
	Action     string                 `json:"action"` // "create", "update", "delete"
	UserID     string                 `json:"user_id"`
	Changes    map[string]interface{} `json:"changes"`
	Timestamp  time.Time              `json:"timestamp"`
	IPAddress  string                 `json:"ip_address"`
}

// AuditRepository provides audit logging capabilities
type AuditRepository interface {
	LogAction(ctx context.Context, log *AuditLog) error
	GetAuditTrail(ctx context.Context, entityID string, entityType string) ([]*AuditLog, error)
	GetUserActions(ctx context.Context, userID string, limit int) ([]*AuditLog, error)
}

// ConfigRepository manages application configuration
type ConfigRepository interface {
	GetConfig(ctx context.Context, key string) (interface{}, error)
	SetConfig(ctx context.Context, key string, value interface{}) error
	DeleteConfig(ctx context.Context, key string) error
	ListConfigs(ctx context.Context, prefix string) (map[string]interface{}, error)
	WatchConfig(ctx context.Context, key string, callback func(key string, value interface{})) (ConfigWatchHandle, error)
}

// ConfigWatchHandle represents a configuration watch handle
type ConfigWatchHandle interface {
	Stop() error
	IsActive() bool
}

// SessionRepository manages user sessions
type SessionRepository interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, sessionID string) (*Session, error)
	UpdateSession(ctx context.Context, session *Session) error
	DeleteSession(ctx context.Context, sessionID string) error
	CleanupExpiredSessions(ctx context.Context) error
}

// Session represents a user session
type Session struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	Token     string                 `json:"token"`
	CreatedAt time.Time              `json:"created_at"`
	ExpiresAt time.Time              `json:"expires_at"`
	Data      map[string]interface{} `json:"data"`
	IPAddress string                 `json:"ip_address"`
	UserAgent string                 `json:"user_agent"`
}

// IsExpired checks if the session is expired
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// Extend extends the session by the specified duration
func (s *Session) Extend(duration time.Duration) {
	s.ExpiresAt = time.Now().Add(duration)
}

// FileRepository manages file metadata and references
type FileRepository interface {
	SaveFileInfo(ctx context.Context, info *FileMetadata) error
	GetFileInfo(ctx context.Context, fileID string) (*FileMetadata, error)
	DeleteFileInfo(ctx context.Context, fileID string) error
	ListFilesByOwner(ctx context.Context, ownerID string) ([]*FileMetadata, error)
	SearchFiles(ctx context.Context, query string) ([]*FileMetadata, error)
	UpdateFileAccess(ctx context.Context, fileID string, access *FileAccess) error
}

// FileMetadata contains metadata about a file
type FileMetadata struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Path      string                 `json:"path"`
	Size      int64                  `json:"size"`
	Hash      string                 `json:"hash"`
	MimeType  string                 `json:"mime_type"`
	OwnerID   string                 `json:"owner_id"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	Tags      []string               `json:"tags"`
	Metadata  map[string]interface{} `json:"metadata"`
	Access    *FileAccess            `json:"access"`
}

// FileAccess contains file access permissions
type FileAccess struct {
	PublicRead   bool     `json:"public_read"`
	PublicWrite  bool     `json:"public_write"`
	AllowedUsers []string `json:"allowed_users"`
	BlockedUsers []string `json:"blocked_users"`
}

// MetricsRepository manages application metrics
type MetricsRepository interface {
	RecordMetric(ctx context.Context, metric *Metric) error
	QueryMetrics(ctx context.Context, query *MetricsQuery) ([]*Metric, error)
	GetMetricStats(ctx context.Context, name string, start, end time.Time) (*MetricStats, error)
	DeleteOldMetrics(ctx context.Context, before time.Time) error
}

// Metric represents a single metric measurement
type Metric struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags"`
	Timestamp time.Time         `json:"timestamp"`
	Source    string            `json:"source"`
}

// MetricsQuery represents a query for metrics
type MetricsQuery struct {
	Name        string            `json:"name"`
	Tags        map[string]string `json:"tags"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Limit       int               `json:"limit"`
	GroupBy     []string          `json:"group_by"`
	Aggregation string            `json:"aggregation"` // "sum", "avg", "max", "min", "count"
	Interval    time.Duration     `json:"interval"`    // For time-based grouping
}

// MetricStats contains statistics for a metric
type MetricStats struct {
	Name      string    `json:"name"`
	Count     int64     `json:"count"`
	Sum       float64   `json:"sum"`
	Avg       float64   `json:"avg"`
	Min       float64   `json:"min"`
	Max       float64   `json:"max"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// NotificationRepository manages notifications
type NotificationRepository interface {
	CreateNotification(ctx context.Context, notification *Notification) error
	GetNotifications(ctx context.Context, userID string, limit int) ([]*Notification, error)
	MarkAsRead(ctx context.Context, notificationID string) error
	MarkAllAsRead(ctx context.Context, userID string) error
	DeleteNotification(ctx context.Context, notificationID string) error
	GetUnreadCount(ctx context.Context, userID string) (int64, error)
}

// Notification represents a user notification
type Notification struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	Type      string                 `json:"type"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data"`
	Read      bool                   `json:"read"`
	ReadAt    *time.Time             `json:"read_at"`
	CreatedAt time.Time              `json:"created_at"`
	ExpiresAt *time.Time             `json:"expires_at"`
}

// IsExpired checks if the notification is expired
func (n *Notification) IsExpired() bool {
	return n.ExpiresAt != nil && time.Now().After(*n.ExpiresAt)
}

// MarkRead marks the notification as read
func (n *Notification) MarkRead() {
	now := time.Now()
	n.Read = true
	n.ReadAt = &now
}

// GetEntityID implements Entity.GetID
func (n *Notification) GetID() string {
	return n.ID
}

// GetType implements Entity.GetType
func (n *Notification) GetType() string {
	return "notification"
}

// GetCreatedAt implements Entity.GetCreatedAt
func (n *Notification) GetCreatedAt() time.Time {
	return n.CreatedAt
}

// GetUpdatedAt implements Entity.GetUpdatedAt
func (n *Notification) GetUpdatedAt() time.Time {
	if n.ReadAt != nil {
		return *n.ReadAt
	}
	return n.CreatedAt
}

// SetUpdatedAt implements Entity.SetUpdatedAt
func (n *Notification) SetUpdatedAt(t time.Time) {
	if n.Read {
		n.ReadAt = &t
	}
}

// Validate implements Entity.Validate
func (n *Notification) Validate() error {
	if n.UserID == "" {
		return NewValidationError("user_id", "cannot be empty")
	}
	if n.Type == "" {
		return NewValidationError("type", "cannot be empty")
	}
	if n.Title == "" {
		return NewValidationError("title", "cannot be empty")
	}
	return nil
}

// Implement Entity interface for other types as needed...

// GetEntityID implements Entity.GetID for Session
func (s *Session) GetID() string {
	return s.ID
}

// GetType implements Entity.GetType for Session
func (s *Session) GetType() string {
	return "session"
}

// GetCreatedAt implements Entity.GetCreatedAt for Session
func (s *Session) GetCreatedAt() time.Time {
	return s.CreatedAt
}

// GetUpdatedAt implements Entity.GetUpdatedAt for Session
func (s *Session) GetUpdatedAt() time.Time {
	return s.ExpiresAt
}

// SetUpdatedAt implements Entity.SetUpdatedAt for Session
func (s *Session) SetUpdatedAt(t time.Time) {
	s.ExpiresAt = t
}

// Validate implements Entity.Validate for Session
func (s *Session) Validate() error {
	if s.UserID == "" {
		return NewValidationError("user_id", "cannot be empty")
	}
	if s.Token == "" {
		return NewValidationError("token", "cannot be empty")
	}
	return nil
}

// GetEntityID implements Entity.GetID for FileMetadata
func (f *FileMetadata) GetID() string {
	return f.ID
}

// GetType implements Entity.GetType for FileMetadata
func (f *FileMetadata) GetType() string {
	return "file_metadata"
}

// GetCreatedAt implements Entity.GetCreatedAt for FileMetadata
func (f *FileMetadata) GetCreatedAt() time.Time {
	return f.CreatedAt
}

// GetUpdatedAt implements Entity.GetUpdatedAt for FileMetadata
func (f *FileMetadata) GetUpdatedAt() time.Time {
	return f.UpdatedAt
}

// SetUpdatedAt implements Entity.SetUpdatedAt for FileMetadata
func (f *FileMetadata) SetUpdatedAt(t time.Time) {
	f.UpdatedAt = t
}

// Validate implements Entity.Validate for FileMetadata
func (f *FileMetadata) Validate() error {
	if f.Name == "" {
		return NewValidationError("name", "cannot be empty")
	}
	if f.Path == "" {
		return NewValidationError("path", "cannot be empty")
	}
	if f.OwnerID == "" {
		return NewValidationError("owner_id", "cannot be empty")
	}
	return nil
}
