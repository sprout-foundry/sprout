//go:build windows && !js

package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// nativeOCRAvailable reports whether the platform OCR shim can run.
// Windows ships Windows.Media.Ocr with the OS; the PowerShell bridge is
// always present.
func nativeOCRAvailable() bool {
	if _, err := exec.LookPath("powershell"); err != nil {
		return false
	}
	return true
}

// ocrScript is the PowerShell shim bridging Windows.Media.Ocr. The generic
// WinRT type is matched with -like 'IAsyncOperation*' because the literal
// name contains a backtick, which cannot appear inside a Go raw string.
const ocrScript = `
function Await {
  param($WinRtTask, $ResultType)
  $asTaskGeneric = ([System.WindowsRuntimeSystemExtensions].GetMethods() | ? { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -like 'IAsyncOperation*' })[0]
  $asTask = $asTaskGeneric.MakeGenericMethod($ResultType)
  $netTask = $asTask.Invoke($null, @($WinRtTask))
  $netTask.Wait(-1) | Out-Null
  $netTask.Result
}
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Media.Ocr.OcrEngine, Windows.Media.Ocr, ContentType = WindowsRuntime]
$null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics.Imaging, ContentType = WindowsRuntime]
$path = $args[0]
$file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($path)) ([Windows.Storage.StorageFile])
$stream = Await ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
$decoder = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
$bmp = Await ($decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
$ocr = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
$result = Await ($ocr.RecognizeAsync($bmp)) ([Windows.Media.Ocr.OcrResult])
Write-Output $result.Text
`

// nativeOCR runs Windows.Media.Ocr via PowerShell and returns recognized
// text. Empty output is a valid result (an image with no text).
func nativeOCR(ctx context.Context, imagePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ocrScript, imagePath)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("native OCR: %w: %s", err, string(ee.Stderr))
		}
		return "", fmt.Errorf("native OCR: %w", err)
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n"), nil
}
