//go:build darwin && !js

package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// nativeOCRHelperSource is the macOS Vision-framework OCR program compiled
// on demand (SP-137 Phase 3). Recognition level "accurate" with language
// correction — quality over speed for agent workflows.
const nativeOCRHelperSource = `import Foundation
import Vision
import AppKit

let path = CommandLine.arguments[1]
guard let img = NSImage(contentsOfFile: path),
      let tiff = img.tiffRepresentation,
      let bmp = NSBitmapImageRep(data: tiff),
      let cg = bmp.cgImage else {
  FileHandle.standardError.write("could not load image".data(using: .utf8)!)
  exit(1)
}
let request = VNRecognizeTextRequest { req, _ in
  guard let results = req.results as? [VNRecognizedTextObservation] else { return }
  for r in results {
    if let text = r.topCandidates(1).first?.string { print(text) }
  }
}
request.recognitionLevel = .accurate
request.usesLanguageCorrection = true
let handler = VNImageRequestHandler(cgImage: cg, options: [:])
try? handler.perform([request])
`

var (
	nativeOCRCompileOnce sync.Once
	nativeOCRBinPath     string
	nativeOCRCompileErr  error
)

// nativeOCRAvailable reports whether the platform OCR shim can run.
// On macOS this requires the swiftc compiler (Xcode Command Line Tools).
func nativeOCRAvailable() bool {
	if _, err := exec.LookPath("swiftc"); err != nil {
		return false
	}
	return true
}

// nativeOCRBin returns the path to the compiled helper, compiling it on
// first use. Cached in the user cache dir; a stale or missing binary is
// recompiled (the once-guard only covers process lifetime).
func nativeOCRBin() (string, error) {
	nativeOCRCompileOnce.Do(func() {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			nativeOCRCompileErr = fmt.Errorf("native OCR cache dir: %w", err)
			return
		}
		dir := filepath.Join(cacheDir, "sprout", "bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			nativeOCRCompileErr = fmt.Errorf("native OCR bin dir: %w", err)
			return
		}
		src := filepath.Join(dir, "ocr_helper.swift")
		bin := filepath.Join(dir, "ocr_helper")

		// Recompile when the source is missing or the binary is stale/absent.
		needCompile := true
		srcInfo, srcErr := os.Stat(src)
		binInfo, binErr := os.Stat(bin)
		if srcErr == nil && binErr == nil && binInfo.ModTime().After(srcInfo.ModTime()) {
			needCompile = false
		}
		if needCompile {
			if err := os.WriteFile(src, []byte(nativeOCRHelperSource), 0o644); err != nil {
				nativeOCRCompileErr = fmt.Errorf("write OCR helper source: %w", err)
				return
			}
			cmd := exec.Command("swiftc", "-O", "-o", bin, src)
			if out, err := cmd.CombinedOutput(); err != nil {
				_ = os.Remove(bin)
				nativeOCRCompileErr = fmt.Errorf("compile OCR helper: %w: %s", err, string(out))
				return
			}
		}
		nativeOCRBinPath = bin
	})
	return nativeOCRBinPath, nativeOCRCompileErr
}

// nativeOCR runs the platform OCR shim and returns recognized text.
// Empty output is a valid result (an image with no text).
func nativeOCR(ctx context.Context, imagePath string) (string, error) {
	bin, err := nativeOCRBin()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, bin, imagePath)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("native OCR: %w: %s", err, string(ee.Stderr))
		}
		return "", fmt.Errorf("native OCR: %w", err)
	}
	return string(out), nil
}
