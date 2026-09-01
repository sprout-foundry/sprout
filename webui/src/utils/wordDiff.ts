/**
 * Word-level diff for intraline highlighting (GitHub-style).
 *
 * Tokens are words or runs of whitespace; a character LCS over the token
 * arrays finds changed spans. Bounded: falls back to "everything changed"
 * when either side exceeds MAX_TOKENS so huge rewrites don't O(n·m) the
 * main thread.
 */

export interface WordDiffPart {
  text: string;
  changed: boolean;
}

export interface WordDiffSides {
  /** Spans for the old (deletion) side. */
  del: WordDiffPart[];
  /** Spans for the new (addition) side. */
  add: WordDiffPart[];
}

const MAX_TOKENS = 4000;

/** Split text into word/whitespace tokens, keeping everything. */
export function tokenizeWords(text: string): string[] {
  return text.split(/(\s+)/).filter((t) => t.length > 0);
}

/** True when both sides share a common prefix/suffix worth trimming. */
function trimCommon(a: string[], b: string[]): { prefixLen: number; suffixLen: number } {
  let prefixLen = 0;
  const maxPrefix = Math.min(a.length, b.length);
  while (prefixLen < maxPrefix && a[prefixLen] === b[prefixLen]) prefixLen++;

  let suffixLen = 0;
  const maxSuffix = Math.min(a.length - prefixLen, b.length - prefixLen);
  while (suffixLen < maxSuffix && a[a.length - 1 - suffixLen] === b[b.length - 1 - suffixLen]) suffixLen++;

  return { prefixLen, suffixLen };
}

/** Build parts for a side from token equality flags. */
function buildParts(tokens: string[], changedFlags: boolean[]): WordDiffPart[] {
  const parts: WordDiffPart[] = [];
  for (let i = 0; i < tokens.length; i++) {
    const changed = changedFlags[i];
    const last = parts[parts.length - 1];
    if (last && last.changed === changed) {
      last.text += tokens[i];
    } else {
      parts.push({ text: tokens[i], changed });
    }
  }
  return parts;
}

/**
 * Compute a word-level diff between two strings, returning highlight spans
 * for each side. Runs LCS over token arrays; bounded by MAX_TOKENS.
 */
export function computeWordDiff(oldText: string, newText: string): WordDiffSides {
  const oldTokens = tokenizeWords(oldText);
  const newTokens = tokenizeWords(newText);

  if (oldTokens.length === 0 || newTokens.length === 0) {
    return {
      del: oldTokens.length ? [{ text: oldText, changed: true }] : [],
      add: newTokens.length ? [{ text: newText, changed: true }] : [],
    };
  }

  // Trim identical head/tail first — shrinks the LCS matrix massively for
  // the common "one token changed in a long line" case.
  const { prefixLen, suffixLen } = trimCommon(oldTokens, newTokens);
  const coreOld = oldTokens.slice(prefixLen, oldTokens.length - suffixLen);
  const coreNew = newTokens.slice(prefixLen, newTokens.length - suffixLen);

  const oldChanged = new Array<boolean>(oldTokens.length).fill(true);
  const newChanged = new Array<boolean>(newTokens.length).fill(true);
  for (let i = 0; i < prefixLen; i++) {
    oldChanged[i] = false;
    newChanged[i] = false;
  }
  for (let i = 0; i < suffixLen; i++) {
    oldChanged[oldTokens.length - 1 - i] = false;
    newChanged[newTokens.length - 1 - i] = false;
  }

  if (coreOld.length === 0 || coreNew.length === 0) {
    // Pure insertion/deletion in the middle — nothing more to compute.
    return { del: buildParts(oldTokens, oldChanged), add: buildParts(newTokens, newChanged) };
  }

  if (coreOld.length + coreNew.length > MAX_TOKENS) {
    return { del: buildParts(oldTokens, oldChanged), add: buildParts(newTokens, newChanged) };
  }

  // LCS over the trimmed cores.
  const m = coreOld.length;
  const n = coreNew.length;
  const dp = new Int32Array((m + 1) * (n + 1));
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      dp[i * (n + 1) + j] =
        coreOld[i] === coreNew[j]
          ? dp[(i + 1) * (n + 1) + (j + 1)] + 1
          : Math.max(dp[(i + 1) * (n + 1) + j], dp[i * (n + 1) + (j + 1)]);
    }
  }

  let i = 0;
  let j = 0;
  while (i < m && j < n) {
    if (coreOld[i] === coreNew[j]) {
      oldChanged[prefixLen + i] = false;
      newChanged[prefixLen + j] = false;
      i++;
      j++;
    } else if (dp[(i + 1) * (n + 1) + j] >= dp[i * (n + 1) + (j + 1)]) {
      i++;
    } else {
      j++;
    }
  }

  return { del: buildParts(oldTokens, oldChanged), add: buildParts(newTokens, newChanged) };
}
