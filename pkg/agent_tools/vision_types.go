package tools

// Vision types are organized per-domain across these files (all in package tools):
//
//	vision_pdf_types.go     — PDF-specific constants and error codes
//	vision_image_types.go   — Image constants, error codes, and data structures
//	vision_analyze_types.go — Vision analysis types, usage tracking, and cache stats
//
// Since all files are in the same package, no external import graph is affected.
// Cross-package consumers (e.g., pkg/agent) import "pkg/agent_tools" as "tools"
// and reference types like tools.VisionProcessor or tools.ImageAnalysisResponse
// — the split is invisible to callers.
