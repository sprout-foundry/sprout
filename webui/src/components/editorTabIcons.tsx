// File-extension → icon-component / color helpers for EditorTabs. Extracted
// from EditorTabs.tsx (which had grown past 950 LOC) to keep the tab-list
// component focused on tab interaction. Pure functions, no React state.

import {
  Braces,
  Code2,
  File,
  FileCode,
  FileText,
  FileWarning,
  GitCompareArrows,
  Globe,
  Headphones,
  ImageIcon,
  MessageSquareText,
  Palette,
  Settings,
  ShieldCheck,
  Sparkles,
  Terminal,
  Video,
  type LucideIcon,
} from 'lucide-react';
import type { ReactNode } from 'react';
import type { EditorBuffer } from '../types/editor';

/** Duck-type check for thenable values (handles sync/async prop mismatch). */
export function catchIfAsync(value: void | Promise<void>, handler: (err: unknown) => void): void {
  if (value != null && typeof (value as { then?: unknown }).then === 'function') {
    Promise.resolve(value).catch(handler);
  }
}

export const FILE_ICON_SIZE = 16;

/** Map a file extension to its Lucide icon component (no JSX). */
export const getFileIconComponent = (ext?: string): LucideIcon => {
  if (!ext) return File;

  switch (ext.toLowerCase()) {
    case '.js':
    case '.jsx':
      return FileCode;
    case '.ts':
    case '.tsx':
      return Braces;
    case '.go':
      return Code2;
    case '.py':
      return FileCode;
    case '.json':
      return Braces;
    case '.html':
      return Globe;
    case '.css':
      return Palette;
    case '.md':
      return FileText;
    case '.txt':
      return FileText;
    case '.yml':
    case '.yaml':
      return Settings;
    case '.sh':
      return Terminal;
    // Image files
    case '.png':
    case '.jpg':
    case '.jpeg':
    case '.gif':
    case '.bmp':
    case '.webp':
    case '.ico':
    case '.tiff':
    case '.tif':
    case '.avif':
      return ImageIcon;
    // Audio files
    case '.mp3':
    case '.wav':
    case '.ogg':
    case '.flac':
    case '.aac':
    case '.m4a':
    case '.wma':
    case '.opus':
    case '.weba':
      return Headphones;
    // Video files
    case '.mp4':
    case '.webm':
    case '.mov':
    case '.avi':
    case '.mkv':
    case '.m4v':
    case '.flv':
    case '.wmv':
      return Video;
    // Binary/compressed/compiled files
    case '.zip':
    case '.tar':
    case '.gz':
    case '.rar':
    case '.pdf':
    case '.exe':
    case '.dll':
    case '.so':
    case '.wasm':
    case '.jar':
    case '.woff':
    case '.woff2':
    case '.ttf':
    case '.db':
    case '.sqlite':
      return FileWarning;
    default:
      return File;
  }
};

/** Render the file icon as a React node at the standard tab-icon size. */
export const getFileIcon = (ext?: string): ReactNode => {
  const Icon = getFileIconComponent(ext);
  return <Icon size={FILE_ICON_SIZE} />;
};

/** Brand-style file-type color hint. Intentional raw hex per design system —
 * these are file-type identifiers (JS yellow, TS blue, Go cyan) not theme
 * tokens. */
export const getFileIconColor = (ext?: string): string => {
  if (ext === '.chat') return 'var(--accent-primary)';
  if (ext === '.diff') return '#22c55e';
  if (ext === '.review') return '#f59e0b';
  if (ext === '.welcome') return 'var(--accent-color)';
  if (!ext) return '#9ca3af';

  switch (ext.toLowerCase()) {
    case '.js':
    case '.jsx':
      return '#f1e05a';
    case '.ts':
    case '.tsx':
      return '#519aba';
    case '.go':
      return '#00add8';
    case '.py':
      return '#5b9bd5';
    case '.json':
      return '#f1e05a';
    case '.html':
      return '#e44d26';
    case '.css':
      return '#5b8def';
    case '.md':
      return '#519aba';
    // Image files
    case '.png':
    case '.jpg':
    case '.jpeg':
    case '.gif':
    case '.bmp':
    case '.webp':
    case '.ico':
    case '.tiff':
    case '.tif':
    case '.avif':
      return '#a855f7';
    // Audio files
    case '.mp3':
    case '.wav':
    case '.ogg':
    case '.flac':
    case '.aac':
    case '.m4a':
    case '.wma':
    case '.opus':
    case '.weba':
      return '#3b82f6';
    // Video files
    case '.mp4':
    case '.webm':
    case '.mov':
    case '.avi':
    case '.mkv':
    case '.m4v':
    case '.flv':
    case '.wmv':
      return '#ef4444';
    // Binary/compressed/compiled files
    case '.zip':
    case '.tar':
    case '.gz':
    case '.rar':
    case '.pdf':
    case '.exe':
    case '.dll':
    case '.so':
    case '.wasm':
    case '.jar':
    case '.woff':
    case '.woff2':
    case '.ttf':
    case '.db':
    case '.sqlite':
      return '#f59e0b';
    default:
      return '#9ca3af';
  }
};

/** Render the icon for a buffer — chat/diff/review/welcome get dedicated
 * icons; otherwise falls back to the file-extension icon. */
export const getBufferIcon = (buffer: EditorBuffer): ReactNode => {
  switch (buffer.kind) {
    case 'chat':
      return <MessageSquareText size={FILE_ICON_SIZE} />;
    case 'diff':
      return <GitCompareArrows size={FILE_ICON_SIZE} />;
    case 'review':
      return <ShieldCheck size={FILE_ICON_SIZE} />;
    case 'welcome':
      return <Sparkles size={FILE_ICON_SIZE} />;
    default:
      return getFileIcon(buffer.file.ext);
  }
};

/**
 * Extract the chat session ID from a buffer's metadata or path.
 * Returns undefined if the buffer is not a chat or has no identifiable ID.
 */
export function getChatId(buffer: EditorBuffer): string | undefined {
  if (buffer.kind !== 'chat') return undefined;
  return buffer.metadata?.chatId as string | undefined;
}
