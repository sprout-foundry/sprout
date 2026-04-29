import type { ComponentType } from 'react';
import type { ReactNode } from 'react';
import { FileCode, FileText, Image, Music, Video, Archive, FileJson, FileSpreadsheet } from 'lucide-react';

/**
 * Simple fuzzy match utility for file search.
 */
export function fuzzyMatch(query: string, text: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  const t = text.toLowerCase();
  let qIdx = 0;
  for (let i = 0; i < t.length && qIdx < q.length; i++) {
    if (t[i] === q[qIdx]) qIdx++;
  }
  return qIdx === q.length;
}

/**
 * Get icon based on file extension.
 */
export function getFileIcon(name: string): ComponentType<{ size?: number | string; className?: string }> {
  const ext = name.split('.').pop()?.toLowerCase();

  const iconMap: Record<string, ComponentType<{ size?: number | string; className?: string }>> = {
    // Code
    js: FileCode,
    jsx: FileCode,
    ts: FileCode,
    tsx: FileCode,
    py: FileCode,
    rb: FileCode,
    go: FileCode,
    rs: FileCode,
    java: FileCode,
    cpp: FileCode,
    c: FileCode,
    h: FileCode,
    cs: FileCode,
    php: FileCode,
    // Data
    json: FileJson,
    xml: FileJson,
    yaml: FileJson,
    yml: FileJson,
    csv: FileSpreadsheet,
    xls: FileSpreadsheet,
    xlsx: FileSpreadsheet,
    // Media
    png: Image,
    jpg: Image,
    jpeg: Image,
    gif: Image,
    svg: Image,
    mp3: Music,
    wav: Music,
    mp4: Video,
    mov: Video,
    // Archives
    zip: Archive,
    tar: Archive,
    gz: Archive,
    rar: Archive,
  };

  return iconMap[ext || ''] || FileText;
}

/**
 * Highlight search query match in text.
 */
export function highlightMatch(text: string, query: string): ReactNode {
  if (!query) return text;

  const q = query.toLowerCase();
  const t = text.toLowerCase();

  let matchStart = -1;
  for (let i = 0; i <= t.length - q.length; i++) {
    if (t.substring(i, i + q.length) === q) {
      matchStart = i;
      break;
    }
  }

  if (matchStart === -1) return text;

  return (
    <span>
      {text.substring(0, matchStart)}
      <span className="filetree-highlight">{text.substring(matchStart, matchStart + q.length)}</span>
      {text.substring(matchStart + q.length)}
    </span>
  );
}
