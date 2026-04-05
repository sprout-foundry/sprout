package pythonruntime

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Interpreter contains resolved Python interpreter metadata.
type Interpreter struct {
	Alias   string
	Path    string
	Version string
	Major   int
	Minor   int
}

// FindPython3Interpreter resolves a Python 3 interpreter from common aliases.
func FindPython3Interpreter() (Interpreter, error) {
	return FindPython3InterpreterAtLeast(0)
}

// FindPython3InterpreterAtLeast resolves a Python 3 interpreter with a minimum minor version.
func FindPython3InterpreterAtLeast(minMinor int) (Interpreter, error) {
	candidates := []string{"python3", "python"}
	var versionMismatches []string

	for _, alias := range candidates {
		pythonPath, err := exec.LookPath(alias)
		if err != nil {
			continue
		}

		interp, err := inspectInterpreter(alias, pythonPath)
		if err != nil {
			versionMismatches = append(versionMismatches, fmt.Sprintf("%s: could not read version", alias))
			continue
		}

		if interp.Major != 3 {
			versionMismatches = append(versionMismatches, fmt.Sprintf("%s -> %s (major=%d)", alias, interp.Version, interp.Major))
			continue
		}
		if interp.Minor < minMinor {
			versionMismatches = append(versionMismatches, fmt.Sprintf("%s -> %s (requires >=3.%d)", alias, interp.Version, minMinor))
			continue
		}

		return interp, nil
	}

	if len(versionMismatches) > 0 {
		return Interpreter{}, fmt.Errorf(
			"python 3.%d+ is required; found incompatible interpreters: %s",
			minMinor,
			strings.Join(versionMismatches, "; "),
		)
	}

	return Interpreter{}, fmt.Errorf("python 3.%d+ is required but neither 'python3' nor 'python' was found in PATH", minMinor)
}

func inspectInterpreter(alias, path string) (Interpreter, error) {
	cmd := exec.Command(
		path,
		"-c",
		"import sys; print(sys.version_info.major); print(sys.version_info.minor); print(sys.version.split()[0])",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Interpreter{}, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		return Interpreter{}, fmt.Errorf("unexpected version response")
	}

	major, majorErr := strconv.Atoi(strings.TrimSpace(lines[0]))
	if majorErr != nil {
		return Interpreter{}, fmt.Errorf("invalid major version: %w", majorErr)
	}

	minor, minorErr := strconv.Atoi(strings.TrimSpace(lines[1]))
	if minorErr != nil {
		return Interpreter{}, fmt.Errorf("invalid minor version: %w", minorErr)
	}

	return Interpreter{
		Alias:   alias,
		Path:    path,
		Version: strings.TrimSpace(lines[2]),
		Major:   major,
		Minor:   minor,
	}, nil
}
