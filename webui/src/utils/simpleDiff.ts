/**
 * Generate a simple unified diff between two strings.
 * Returns a unified diff string or null if contents are identical.
 */
export function generateUnifiedDiff(oldText: string, newText: string, oldLabel = 'editor', newLabel = 'disk'): string | null {
  if (oldText === newText) return null;

  const oldLines = oldText.split('\n');
  const newLines = newText.split('\n');

  // Guard against O(n²) memory/time for very large files
  const MAX_DIFF_LINES = 2000;
  if (oldLines.length > MAX_DIFF_LINES || newLines.length > MAX_DIFF_LINES) {
    return `--- ${oldLabel}\n+++ ${newLabel}\n\n... (file too large for inline diff: ${oldLines.length} → ${newLines.length} lines)\n`;
  }

  // Simple line-by-line diff using LCS (longest common subsequence)
  const diff = computeDiff(oldLines, newLines);

  let result = `--- ${oldLabel}\n+++ ${newLabel}\n`;

  for (const line of diff) {
    const prefix = line.type === 'equal' ? ' ' : line.type === 'remove' ? '-' : '+';
    result += `${prefix}${line.value}\n`;
  }

  return result;
}

interface DiffLine {
  type: 'equal' | 'remove' | 'add';
  value: string;
}

function computeDiff(oldLines: string[], newLines: string[]): DiffLine[] {
  // Use a simple LCS-based approach
  const m = oldLines.length;
  const n = newLines.length;

  // Build LCS table
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (oldLines[i - 1] === newLines[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1;
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
  }

  // Backtrack to produce diff
  const result: DiffLine[] = [];
  let i = m, j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
      result.push({ type: 'equal', value: oldLines[i - 1] });
      i--; j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      result.push({ type: 'add', value: newLines[j - 1] });
      j--;
    } else {
      result.push({ type: 'remove', value: oldLines[i - 1] });
      i--;
    }
  }

  return result.reverse();
}
