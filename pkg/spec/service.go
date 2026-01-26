package spec

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// SpecReviewService integrates spec extraction and validation
type SpecReviewService struct {
	extractor *SpecExtractor
	validator *ScopeValidator
	logger    *utils.Logger
	cfg       *configuration.Config
}

// NewSpecReviewService creates a new spec review service
func NewSpecReviewService(cfg *configuration.Config, logger *utils.Logger) (*SpecReviewService, error) {
	extractor, err := NewSpecExtractor(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create spec extractor: %w", err)
	}

	validator, err := NewScopeValidator(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create scope validator: %w", err)
	}

	return &SpecReviewService{
		extractor: extractor,
		validator: validator,
		logger:    logger,
		cfg:       cfg,
	}, nil
}

// ExtractAndValidate extracts spec and validates changes in one call
func (s *SpecReviewService) ExtractAndValidate(conversation []Message, diff string, userIntent string) (*ScopeReviewResult, *CanonicalSpec, error) {
	// Extract spec from conversation
	s.logger.LogProcessStep("Extracting canonical specification from conversation...")
	extractionResult, err := s.extractor.ExtractSpec(conversation, userIntent)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract spec: %w", err)
	}

	spec := extractionResult.Spec

	// Validate changes against spec
	s.logger.LogProcessStep("Validating changes against specification...")
	scopeResult, err := s.validator.ValidateScope(diff, spec)
	if err != nil {
		return nil, spec, fmt.Errorf("failed to validate scope: %w", err)
	}

	return scopeResult, spec, nil
}

// GetExtractor returns the spec extractor
func (s *SpecReviewService) GetExtractor() *SpecExtractor {
	return s.extractor
}

// GetValidator returns the scope validator
func (s *SpecReviewService) GetValidator() *ScopeValidator {
	return s.validator
}
