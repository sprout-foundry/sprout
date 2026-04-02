package codereview

import (
	"fmt"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	maxReviewPromptBytes        = 700 * 1024
	maxReviewMetadataFieldBytes = 16 * 1024
	maxReviewRelatedFiles       = 50
	estimatedCharsPerToken      = 4
)

// ReviewContext represents the context for a code review request
type ReviewContext struct {
	Diff                  string // The code diff to review
	OriginalPrompt        string // The original user prompt (for automated reviews)
	ProcessedInstructions string // Processed instructions (for automated reviews)
	RevisionID            string // Revision ID for change tracking
	Config                *configuration.Config
	Logger                *utils.Logger
	History               *ReviewHistory      // Review history for this context
	SessionID             string              // Unique session identifier
	CurrentIteration      int                 // Current iteration number
	FullFileContext       string              // Full file content for patch resolution context
	RelatedFiles          []string            // Files that might be affected by changes
	AgentClient           api.ClientInterface // Agent API client for LLM calls
	// Metadata for enhanced context
	ProjectType      string // Project type (Go, Node.js, etc.)
	CommitMessage    string // Commit message/intent
	KeyComments      string // Key code comments explaining WHY
	ChangeCategories string // Categorization of changes
}

// ReviewType defines the type of code review being performed
type ReviewType int

const (
	StagedReview ReviewType = iota // Used for reviewing Git staged changes
)

// ReviewOptions contains options for the code review
type ReviewOptions struct {
	Type             ReviewType
	SkipPrompt       bool
	PreapplyReview   bool
	RollbackOnReject bool // Whether to rollback changes when review is rejected
}

// CodeReviewService provides a unified interface for code review operations
type CodeReviewService struct {
	config             *configuration.Config
	logger             *utils.Logger
	reviewConfig       *ReviewConfiguration
	contextStore       map[string]*ReviewContext // Store contexts by session ID for persistence
	defaultAgentClient api.ClientInterface       // Default agent client for LLM calls
}

// NewCodeReviewService creates a new code review service instance
func NewCodeReviewService(cfg *configuration.Config, logger *utils.Logger) *CodeReviewService {
	agentClient := createReviewAgentClient(cfg, logger)

	return &CodeReviewService{
		config:             cfg,
		logger:             logger,
		reviewConfig:       DefaultReviewConfiguration(),
		contextStore:       make(map[string]*ReviewContext),
		defaultAgentClient: agentClient,
	}
}

// NewCodeReviewServiceWithConfig creates a new code review service instance with custom configuration
func NewCodeReviewServiceWithConfig(cfg *configuration.Config, logger *utils.Logger, reviewConfig *ReviewConfiguration) *CodeReviewService {
	agentClient := createReviewAgentClient(cfg, logger)

	return &CodeReviewService{
		config:             cfg,
		logger:             logger,
		reviewConfig:       reviewConfig,
		contextStore:       make(map[string]*ReviewContext),
		defaultAgentClient: agentClient,
	}
}

func createReviewAgentClient(cfg *configuration.Config, logger *utils.Logger) api.ClientInterface {
	clientType, model, err := configuration.ResolveProviderModel(cfg, "", "")
	if err != nil {
		if logger != nil {
			logger.LogProcessStep(fmt.Sprintf("Warning: failed to resolve provider/model for code review: %v", err))
		}
		return nil
	}

	client, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		if logger != nil {
			logger.LogProcessStep(fmt.Sprintf("Warning: failed to initialize review provider '%s' (model '%s'): %v", clientType, model, err))
		}
		return nil
	}

	return client
}

// GetDefaultAgentClient returns the default agent client for this service
func (s *CodeReviewService) GetDefaultAgentClient() api.ClientInterface {
	return s.defaultAgentClient
}

// storeContext stores a review context for later retrieval
func (s *CodeReviewService) storeContext(ctx *ReviewContext) {
	if ctx.SessionID != "" {
		s.contextStore[ctx.SessionID] = ctx
	}
}

// getStoredContext retrieves a previously stored context by session ID
func (s *CodeReviewService) getStoredContext(sessionID string) (*ReviewContext, bool) {
	ctx, exists := s.contextStore[sessionID]
	return ctx, exists
}
