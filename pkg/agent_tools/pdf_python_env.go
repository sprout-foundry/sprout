package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/pythonruntime"
)

const (
	pdfVenvDirName    = "pdf_venv"
	pdfPythonMinMinor = 10 // Pillow requires Python >= 3.10
)

var (
	pdfPythonEnvMu      sync.Mutex
	cachedPDFPythonExec string
)

// CheckPDFPython3Available validates that a compatible Python runtime is available for PDF processing.
func CheckPDFPython3Available() error {
	_, err := getSystemPython3Executable()
	return err
}

// GetPDFPythonExecutable ensures a consistent per-user Python environment for PDF extraction.
func GetPDFPythonExecutable() (string, error) {
	pdfPythonEnvMu.Lock()
	defer pdfPythonEnvMu.Unlock()

	if cachedPDFPythonExec != "" {
		if _, err := os.Stat(cachedPDFPythonExec); err == nil {
			return cachedPDFPythonExec, nil
		}
		cachedPDFPythonExec = ""
	}

	systemPython, err := getSystemPython3Executable()
	if err != nil {
		return "", err
	}

	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve config directory: %w", err)
	}

	venvDir := filepath.Join(configDir, pdfVenvDirName)
	venvPython := getVenvPythonPath(venvDir)

	if _, err := os.Stat(venvPython); err != nil {
		if mkErr := createVenv(systemPython, venvDir); mkErr != nil {
			return "", mkErr
		}
	}

	if err := ensurePDFPythonDependencies(venvPython); err != nil {
		return "", err
	}

	cachedPDFPythonExec = venvPython
	return venvPython, nil
}

func getSystemPython3Executable() (string, error) {
	interpreter, err := pythonruntime.FindPython3InterpreterAtLeast(pdfPythonMinMinor)
	if err != nil {
		return "", fmt.Errorf("python 3.%d+ is required for PDF processing: %w", pdfPythonMinMinor, err)
	}
	return interpreter.Path, nil
}

func createVenv(systemPython, venvDir string) error {
	cmd := exec.Command(systemPython, "-m", "venv", venvDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create PDF venv at %s: %s", venvDir, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensurePDFPythonDependencies(venvPython string) error {
	checkCmd := exec.Command(venvPython, "-c", "import pypdf; from PIL import Image")
	if err := checkCmd.Run(); err == nil {
		return nil
	}

	installCmd := exec.Command(
		venvPython, "-m", "pip", "install", "--disable-pip-version-check", "pypdf", "Pillow",
	)
	if out, err := installCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed installing PDF Python dependencies (pypdf, Pillow): %s", strings.TrimSpace(string(out)))
	}

	recheckCmd := exec.Command(venvPython, "-c", "import pypdf; from PIL import Image")
	if out, err := recheckCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("PDF Python dependencies failed validation: %s", strings.TrimSpace(string(out)))
	}

	return nil
}

func getVenvPythonPath(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python")
}
