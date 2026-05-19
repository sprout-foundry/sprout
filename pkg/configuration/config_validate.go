package configuration

import "fmt"

// Validate validates the configuration and returns any errors
func (c *Config) Validate() error {
	if _, ok := NormalizeSelfReviewGateMode(c.SelfReviewGateMode); !ok {
		return fmt.Errorf("invalid self_review_gate_mode: %q (allowed: off, code, always)", c.SelfReviewGateMode)
	}

	// Validate PDF OCR configuration
	if c.PDFOCREnabled {
		if c.PDFOCRProvider == "" {
			return fmt.Errorf("PDF OCR provider cannot be empty when PDF OCR is enabled")
		}
		if c.PDFOCRModel == "" {
			return fmt.Errorf("PDF OCR model cannot be empty when PDF OCR is enabled")
		}
	}

	return nil
}
