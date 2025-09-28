package agent

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ShowColoredDiff displays a colored diff between old and new content, focusing on actual changes
// Uses Python's difflib for better diff quality when available, falls back to Go implementation
func (a *Agent) ShowColoredDiff(oldContent, newContent string, maxLines int) {
    // Try Python difflib first for better diff quality
    if a.showPythonDiff(oldContent, newContent, maxLines) {
        return
    }

    // Fallback to Go implementation
    a.showGoDiff(oldContent, newContent, maxLines)
}

// showPythonDiff attempts to use Python's difflib for superior diff output
// Returns true if successful, false if Python is unavailable or execution fails
func (a *Agent) showPythonDiff(oldContent, newContent string, maxLines int) bool {
	// Check if Python is available
	if !isPythonAvailable() {
		if a.debug {
			a.debugLog("Python not available, falling back to Go diff implementation")
		}
		return false
	}

	// Create temporary files for the diff
	tmpDir, err := ioutil.TempDir("", "coder_diff_")
	if err != nil {
		if a.debug {
			a.debugLog("Failed to create temporary directory for diff: %v", err)
		}
		return false
	}
	defer os.RemoveAll(tmpDir)

	oldFile := filepath.Join(tmpDir, "old.txt")
	newFile := filepath.Join(tmpDir, "new.txt")

	// Write content to temporary files
	if err := ioutil.WriteFile(oldFile, []byte(oldContent), 0644); err != nil {
		if a.debug {
			a.debugLog("Failed to write old content to temporary file: %v", err)
		}
		return false
	}
	if err := ioutil.WriteFile(newFile, []byte(newContent), 0644); err != nil {
		if a.debug {
			a.debugLog("Failed to write new content to temporary file: %v", err)
		}
		return false
	}

	// Create Python script for unified diff
	pythonScript := fmt.Sprintf(`
import sys
import difflib
import os

def main():
    try:
        with open('%s', 'r', encoding='utf-8', errors='replace') as f:
            old_lines = f.readlines()
        with open('%s', 'r', encoding='utf-8', errors='replace') as f:
            new_lines = f.readlines()
        
        # Generate unified diff
        diff = list(difflib.unified_diff(
            old_lines, 
            new_lines, 
            fromfile='old',
            tofile='new',
            lineterm='',
            n=3
        ))
        
        if not diff:
            print("No changes detected", end='\r\n')
            return
            
        print("File changes:", end='\r\n')
        print("----------------------------------------", end='\r\n')
        
        lines_shown = 0
        if %d == 0:
            max_lines = float('inf')
        else:
            max_lines = %d
        
        # ANSI color codes
        RED = '\033[31m'
        GREEN = '\033[32m'
        CYAN = '\033[36m'
        RESET = '\033[0m'
        
        for line in diff:
            if lines_shown >= max_lines:
                print(f"... (truncated after {max_lines} lines)", end='\r\n')
                break
                
            line = line.rstrip('\n')
            if line.startswith('---') or line.startswith('+++'):
                print(f"{CYAN}{line}{RESET}", end='\r\n')
            elif line.startswith('@@'):
                print(f"{CYAN}{line}{RESET}", end='\r\n')
            elif line.startswith('-'):
                print(f"{RED}{line}{RESET}", end='\r\n')
            elif line.startswith('+'):
                print(f"{GREEN}{line}{RESET}", end='\r\n')
            else:
                print(line, end='\r\n')
            
            lines_shown += 1
        
        print("----------------------------------------", end='\r\n')
        
    except Exception as e:
        sys.stderr.write(f"Error: {e}\n")
        sys.exit(1)

if __name__ == "__main__":
    main()
`, oldFile, newFile, maxLines, maxLines)

	scriptFile := filepath.Join(tmpDir, "diff_script.py")
	if err := ioutil.WriteFile(scriptFile, []byte(pythonScript), 0644); err != nil {
		if a.debug {
			a.debugLog("Failed to write Python diff script: %v", err)
		}
		return false
	}

	// Execute Python script
    cmd := exec.Command("python3", scriptFile)
    out, err := cmd.CombinedOutput()
    if err != nil {
        if a.debug {
            a.debugLog("python3 execution failed: %v, trying python", err)
        }
        // Try python instead of python3
        cmd = exec.Command("python", scriptFile)
        out, err = cmd.CombinedOutput()
        if err != nil {
            if a.debug {
                a.debugLog("python execution also failed: %v, falling back to Go diff", err)
            }
            return false
        }
    }

    // Route diff output through agent's streaming-aware printer to avoid being overwritten
    a.PrintLine(strings.ReplaceAll(string(out), "\r\n", "\n"))
    return true
}

// showGoDiff provides the fallback Go implementation
func (a *Agent) showGoDiff(oldContent, newContent string, maxLines int) {
    const red = "\033[31m"   // Red for deletions
    const green = "\033[32m" // Green for additions
    const reset = "\033[0m"

    oldLines := strings.Split(oldContent, "\n")
    newLines := strings.Split(newContent, "\n")

    // Find the actual changes by identifying differing regions
    changes := a.findChanges(oldLines, newLines)

    var b strings.Builder
    if len(changes) == 0 {
        b.WriteString("No changes detected\n")
        a.PrintLine(b.String())
        return
    }

    b.WriteString("File changes:\n")
    b.WriteString("----------------------------------------\n")

    totalLinesShown := 0

    for _, change := range changes {
        if totalLinesShown >= maxLines {
            b.WriteString(fmt.Sprintf("... (truncated after %d lines)\n", maxLines))
            break
        }

        // Show deletions (old content)
        if change.OldLength > 0 {
            for i := 0; i < change.OldLength && totalLinesShown < maxLines; i++ {
                lineNum := change.OldStart + i
                if lineNum < len(oldLines) {
                    b.WriteString(fmt.Sprintf("%s- %s%s\n", red, oldLines[lineNum], reset))
                    totalLinesShown++
                }
            }
        }

        // Show additions (new content)
        if change.NewLength > 0 {
            for i := 0; i < change.NewLength && totalLinesShown < maxLines; i++ {
                lineNum := change.NewStart + i
                if lineNum < len(newLines) {
                    b.WriteString(fmt.Sprintf("%s+ %s%s\n", green, newLines[lineNum], reset))
                    totalLinesShown++
                }
            }
        }

        // Add separator between changes
        if totalLinesShown < maxLines {
            b.WriteString("\n")
            totalLinesShown++
        }
    }

    b.WriteString("----------------------------------------\n")
    a.PrintLine(b.String())
}

// isPythonAvailable checks if Python is available on the system
func isPythonAvailable() bool {
	// Try python3 first
	if _, err := exec.LookPath("python3"); err == nil {
		return true
	}

	// Try python
	if _, err := exec.LookPath("python"); err == nil {
		return true
	}

	return false
}

// findChanges identifies regions where content differs between old and new versions
func (a *Agent) findChanges(oldLines, newLines []string) []DiffChange {
	var changes []DiffChange

	oldLen := len(oldLines)
	newLen := len(newLines)
	maxLen := oldLen
	if newLen > oldLen {
		maxLen = newLen
	}

	changeStart := -1

	for i := 0; i < maxLen; i++ {
		oldLine := ""
		newLine := ""

		if i < oldLen {
			oldLine = oldLines[i]
		}
		if i < newLen {
			newLine = newLines[i]
		}

		// Check if lines differ
		linesDiffer := oldLine != newLine

		if linesDiffer {
			// Start of a new change
			if changeStart == -1 {
				changeStart = i
			}
		} else {
			// End of a change (if we were in one)
			if changeStart != -1 {
				// Calculate the lengths for old and new content
				oldChangeLen := i - changeStart
				newChangeLen := i - changeStart

				// Adjust lengths if one side runs out of lines
				if changeStart+oldChangeLen > oldLen {
					oldChangeLen = oldLen - changeStart
				}
				if changeStart+newChangeLen > newLen {
					newChangeLen = newLen - changeStart
				}

				// Ensure lengths are not negative
				if oldChangeLen < 0 {
					oldChangeLen = 0
				}
				if newChangeLen < 0 {
					newChangeLen = 0
				}

				changes = append(changes, DiffChange{
					OldStart:  changeStart,
					OldLength: oldChangeLen,
					NewStart:  changeStart,
					NewLength: newChangeLen,
				})

				changeStart = -1 // Reset for next change
			}
		}
	}

	// Handle case where change extends to the end
	if changeStart != -1 {
		oldChangeLen := oldLen - changeStart
		newChangeLen := newLen - changeStart

		if oldChangeLen < 0 {
			oldChangeLen = 0
		}
		if newChangeLen < 0 {
			newChangeLen = 0
		}

		changes = append(changes, DiffChange{
			OldStart:  changeStart,
			OldLength: oldChangeLen,
			NewStart:  changeStart,
			NewLength: newChangeLen,
		})
	}

	return changes
}
