package tools

import (
	"errors"
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ============================================================================
// SP-103-A6: Typed error → ErrorCode translation at the response-builder
// boundary.
//
// Replaces brittle strings.Contains(errMsg, ...) classification with:
//   1. errors.As against *agenterrors.TypedError — extract Code → map to
//      ErrCode* using a stable switch (TypedError↔ImageAnalysisResponse
//      code table below).
//   2. IsRemoteSizeExceededError (new) → ErrCodeRemoteFetchFailed.
//   3. Fall back to legacy strings.Contains classification only when the
//      error chain has no TypedError.
// ============================================================================

// typedErrorToVisionCode maps *agenterrors.TypedError.Code values to the
// vision-pipeline's ImageAnalysisResponse.ErrorCode strings. The mapping
// is intentionally narrow; unknown codes fall through to the legacy
// classifier.
func typedErrorToVisionCode(te *agenterrors.TypedError) string {
	if te == nil {
		return ""
	}
	switch te.Code {
	case agenterrors.CodeNotFound:
		// A typed NotFound is typically about a missing local/remote file.
		// The legacy strings.Contains split on inputType; we keep that
		// pattern so the response messages stay meaningful.
		return ErrCodeLocalFileNotFound
	case agenterrors.CodeNetwork:
		// Network failures during download/HTTP — surface as REMOTE_FETCH.
		return ErrCodeRemoteFetchFailed
	case agenterrors.CodeTimeout:
		return ErrCodeVisionRequestFailed
	case agenterrors.CodeValidation:
		return ErrCodeInputUnsupported
	case agenterrors.CodeTool:
		return ErrCodeVisionRequestFailed
	case agenterrors.CodeAgent, agenterrors.CodeConfig, agenterrors.CodePermission, agenterrors.CodeApproval,
		agenterrors.CodeUnknown:
		return ErrCodeVisionRequestFailed
	}
	return ""
}

// classifyVisionResponseError inspects the error chain and returns the
// best-fit ErrorCode for the response. Order of preference:
//
//   1. *remoteSizeExceededError (cap exceeded before GET) → REMOTE_FETCH.
//   2. *agenterrors.TypedError → typedErrorToVisionCode mapping.
//   3. Legacy strings.Contains classifier (legacyStringClassifyError).
//
// The legacy classifier preserves the previous behavior when neither typed
// error is present, so callers don't get a worse experience for untyped
// errors emitted by helpers outside our control.
func classifyVisionResponseError(err error) string {
	if err == nil {
		return ErrCodeVisionRequestFailed
	}
	if IsRemoteSizeExceededError(err) {
		return ErrCodeRemoteFetchFailed
	}
	var te *agenterrors.TypedError
	if errors.As(err, &te) {
		if code := typedErrorToVisionCode(te); code != "" {
			return code
		}
	}
	return legacyStringClassifyError(err)
}

// legacyStringClassifyError is the pre-A6 strings.Contains classifier,
// preserved verbatim so behavior for untyped errors doesn't regress.
// (Mirrors the analyze_image response-builder block in vision_image.go.)
func legacyStringClassifyError(err error) string {
	if err == nil {
		return ErrCodeVisionRequestFailed
	}
	msg := strings.ToLower(err.Error())

	// Image-fetch failures.
	if strings.Contains(msg, "get image data") || strings.Contains(msg, "download image") {
		// Old code branched on inputType here; the caller is expected
		// to refine ErrCodeRemoteFetchFailed vs ErrCodeLocalFileNotFound
		// at the call site when inputType is known. We default to
		// REMOTE_FETCH (the more specific failure mode).
		return ErrCodeRemoteFetchFailed
	}

	// Model returned empty / unparseable response.
	if strings.Contains(msg, "no response from vision model") {
		return ErrCodeInvalidResponse
	}

	// Conservative default for unknown vision-model failures.
	return ErrCodeVisionRequestFailed
}

// applyClassifiedError mutates the ImageAnalysisResponse with the ErrorCode
// and an ErrorMessage built from the typed error (Component + Details) when
// available, falling back to the legacy "[op] failed: <err>" template.
//
// inputType refines ambiguous classifications: for REMOTE_FETCH we keep it
// for "remote_url", and fall through to LOCAL_FILE_NOT_FOUND for "local_file"
// when the chained error matches a "stat / no such file" pattern.
func applyClassifiedError(response *ImageAnalysisResponse, err error, inputType, opName string) {
	if response == nil {
		return
	}
	code := classifyVisionResponseError(err)

	// Refine ambiguous codes by input source.
	lowerMsg := strings.ToLower(err.Error())
	if inputType == "local_file" {
		// For local files, "no such file" / "stat " patterns always
		// mean a missing source file, regardless of the upstream
		// classifier's call.
		if strings.Contains(lowerMsg, "no such file") || strings.Contains(lowerMsg, "stat ") {
			code = ErrCodeLocalFileNotFound
		}
	}

	response.ErrorCode = code

	// Prefer typed-error Component + Message when available — richer than
	// just "failed: <err>".
	var te *agenterrors.TypedError
	if errors.As(err, &te) {
		prefix := opName
		if te.Component != "" {
			prefix = opName + " [" + te.Component + "]"
		}
		response.ErrorMessage = prefix + " " + te.Message
		return
	}

	// Fallback: keep the existing "[op] failed: <err>" template.
	if opName == "" {
		response.ErrorMessage = err.Error()
		return
	}
	response.ErrorMessage = fmt.Sprintf("%s failed: %v", opName, err)
}
