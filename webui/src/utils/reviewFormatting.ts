import { debugLog } from './log';

export interface ReviewGuidanceEntry {
  issue: string;
  evidence?: string;
  suggestion?: string;
  file?: string;
  [key: string]: unknown;
}

export interface ReviewGuidanceSection {
  id: string;
  title: string;
  entries: ReviewGuidanceEntry[];
}

export interface ParsedReviewGuidance {
  markdown: string;
  sections: ReviewGuidanceSection[];
}

const SECTION_ORDER = ['MUST_FIX', 'SHOULD_FIX', 'VERIFY', 'SUGGEST'] as const;

const prettifySectionTitle = (key: string): string =>
  key
    .toLowerCase()
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');

const stripCodeFence = (value: string): string => {
  const trimmed = value.trim();
  const fencedMatch = trimmed.match(/^```(?:json|javascript|js)?\s*([\s\S]*?)\s*```$/i);
  return fencedMatch ? fencedMatch[1].trim() : trimmed;
};

const parseJsonLike = (value: string): unknown | null => {
  const normalized = stripCodeFence(value);
  if (!normalized) {
    return null;
  }

  try {
    const parsed = JSON.parse(normalized);
    if (typeof parsed === 'string' && parsed.trim() !== normalized) {
      return parseJsonLike(parsed) ?? parsed;
    }
    return parsed;
  } catch (err) {
    debugLog('[reviewFormatting] parseJsonLike JSON parse failed:', err);
    return null;
  }
};

const normalizeEntry = (entry: unknown): ReviewGuidanceEntry | null => {
  if (typeof entry === 'string') {
    const issue = entry.trim();
    return issue ? { issue } : null;
  }

  if (!entry || typeof entry !== 'object' || Array.isArray(entry)) {
    return null;
  }

  const record = entry as Record<string, unknown>;
  const issueCandidate =
    typeof record.issue === 'string'
      ? record.issue
      : typeof record.title === 'string'
        ? record.title
        : typeof record.summary === 'string'
          ? record.summary
          : '';
  const issue = issueCandidate.trim();
  if (!issue) {
    return null;
  }

  const normalized: ReviewGuidanceEntry = { issue };
  if (typeof record.evidence === 'string' && record.evidence.trim()) {
    normalized.evidence = record.evidence.trim();
  }
  if (typeof record.suggestion === 'string' && record.suggestion.trim()) {
    normalized.suggestion = record.suggestion.trim();
  }
  if (typeof record.file === 'string' && record.file.trim()) {
    normalized.file = record.file.trim();
  }

  Object.entries(record).forEach(([key, value]) => {
    if (key in normalized || ['issue', 'title', 'summary', 'evidence', 'suggestion', 'file'].includes(key)) {
      return;
    }
    if (typeof value === 'string' && value.trim()) {
      normalized[key] = value.trim();
    }
  });

  return normalized;
};

export const parseReviewGuidance = (rawValue?: string | null): ParsedReviewGuidance => {
  const raw = (rawValue || '').trim();
  if (!raw) {
    return { markdown: '', sections: [] };
  }

  const parsed = parseJsonLike(raw);
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { markdown: stripCodeFence(raw), sections: [] };
  }

  const record = parsed as Record<string, unknown>;
  const sections: ReviewGuidanceSection[] = [];
  const seenKeys = new Set<string>();

  SECTION_ORDER.forEach((key) => {
    const value = record[key];
    if (!Array.isArray(value)) {
      return;
    }
    const entries = value.map(normalizeEntry).filter((entry): entry is ReviewGuidanceEntry => entry !== null);
    if (entries.length === 0) {
      return;
    }
    sections.push({
      id: key,
      title: prettifySectionTitle(key),
      entries,
    });
    seenKeys.add(key);
  });

  Object.entries(record).forEach(([key, value]) => {
    if (seenKeys.has(key) || !Array.isArray(value)) {
      return;
    }
    const entries = value.map(normalizeEntry).filter((entry): entry is ReviewGuidanceEntry => entry !== null);
    if (entries.length === 0) {
      return;
    }
    sections.push({
      id: key,
      title: prettifySectionTitle(key),
      entries,
    });
  });

  if (sections.length === 0) {
    return { markdown: stripCodeFence(raw), sections: [] };
  }

  return { markdown: '', sections };
};

export const reviewGuidanceToMarkdown = (guidance: ParsedReviewGuidance): string => {
  if (guidance.markdown.trim()) {
    return guidance.markdown.trim();
  }

  return guidance.sections
    .map((section) => {
      const body = section.entries
        .map((entry) => {
          const lines = [`- **${entry.issue}**`];
          if (entry.file) {
            lines.push(`  - File: \`${entry.file}\``);
          }
          if (entry.evidence) {
            lines.push(`  - Evidence: ${entry.evidence}`);
          }
          if (entry.suggestion) {
            lines.push(`  - Next step: ${entry.suggestion}`);
          }
          Object.entries(entry)
            .filter(
              ([key, value]) =>
                !['issue', 'file', 'evidence', 'suggestion'].includes(key) && typeof value === 'string' && value.trim(),
            )
            .forEach(([key, value]) => {
              const label = key.replace(/_/g, ' ').replace(/\b\w/g, (char) => char.toUpperCase());
              lines.push(`  - ${label}: ${String(value)}`);
            });
          return lines.join('\n');
        })
        .join('\n');

      return `## ${section.title}\n\n${body}`;
    })
    .join('\n\n')
    .trim();
};
