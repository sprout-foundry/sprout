export interface DiffLineChange {
  type: 'added' | 'removed' | 'modified';
  newLine: number; // 0-based line number in the current file
}

export function parseGitDiff(diffOutput: string): DiffLineChange[] {
  const changes: DiffLineChange[] = [];
  const lines = diffOutput.split('\n');

  let i = 0;
  while (i < lines.length) {
    const line = lines[i];

    // Skip diff headers (--- a/file, +++ b/file)
    if (line.startsWith('---') || line.startsWith('+++')) {
      i++;
      continue;
    }

    // Find hunk header
    const hunkMatch = line.match(/^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/);
    if (!hunkMatch) {
      i++;
      continue;
    }

    const newStart = parseInt(hunkMatch[3], 10);

    i++; // Move past hunk header

    // Track state within hunk
    let newLineNum = newStart - 1; // 0-based
    let pendingRemoved = false; // Flag for modified detection

    while (i < lines.length) {
      const hunkLine = lines[i];

      // Check for next hunk or end of diff
      if (
        hunkLine.startsWith('@@') ||
        hunkLine.startsWith('diff ') ||
        hunkLine.startsWith('---') ||
        hunkLine.startsWith('+++')
      ) {
        break;
      }

      // Handle "\ No newline at end of file"
      if (hunkLine === '\\ No newline at end of file') {
        i++;
        continue;
      }

      if (hunkLine.length === 0) {
        // Empty line in diff - treat as context
        newLineNum++;
        i++;
        continue;
      }

      const firstChar = hunkLine.charAt(0);

      if (firstChar === '+') {
        // Added line
        if (pendingRemoved) {
          // This + follows a -, so it's a modification
          changes.push({ type: 'modified', newLine: newLineNum });
          pendingRemoved = false;
        } else {
          // Pure addition
          changes.push({ type: 'added', newLine: newLineNum });
        }
        newLineNum++;
      } else if (firstChar === ' ') {
        // Context line (unchanged but present in diff)
        newLineNum++;
      } else if (firstChar === '-') {
        // Removed line - don't increment newLineNum
        // But set flag that next + should be marked as modified
        pendingRemoved = true;
      }

      i++;
    }
  }

  return changes;
}
