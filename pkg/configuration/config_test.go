package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "PDF OCR disabled - should pass",
			config: &Config{
				PDFOCREnabled: false,
			},
			expectError: false,
		},
		{
			name: "PDF OCR enabled with provider and model - should pass",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "ollama",
				PDFOCRModel:    "glm-ocr",
			},
			expectError: false,
		},
		{
			name: "PDF OCR enabled but empty provider - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "",
				PDFOCRModel:    "glm-ocr",
			},
			expectError: true,
			errorMsg:    "PDF OCR provider cannot be empty when PDF OCR is enabled",
		},
		{
			name: "PDF OCR enabled but empty model - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "ollama",
				PDFOCRModel:    "",
			},
			expectError: true,
			errorMsg:    "PDF OCR model cannot be empty when PDF OCR is enabled",
		},
		{
			name: "PDF OCR enabled with empty provider and model - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "",
				PDFOCRModel:    "",
			},
			expectError: true,
			errorMsg:    "PDF OCR provider cannot be empty when PDF OCR is enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}