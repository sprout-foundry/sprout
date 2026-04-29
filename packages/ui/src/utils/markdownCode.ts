import { isValidElement } from 'react';
import type { ReactNode } from 'react';

export const flattenMarkdownText = (value: ReactNode): string => {
  if (typeof value === 'string') {
    return value;
  }

  if (typeof value === 'number') {
    return String(value);
  }

  if (Array.isArray(value)) {
    return value.map(flattenMarkdownText).join('');
  }

  if (isValidElement(value)) {
    return flattenMarkdownText(value.props.children);
  }

  return '';
};

/**
 * Detects if the content is a markdown code block.
 * A code block is identified by:
 * - Having a language class (e.g., language-go, language-typescript)
 * - OR containing multiple lines (newlines indicate a block)
 * - OR being a pre-formatted block (detected by className structure)
 */
export const isMarkdownCodeBlock = (className: string | undefined, codeText: string): boolean => {
  // If there's a language class, it's definitely a code block
  if (className && className.includes('language-')) {
    return true;
  }
  // If the text has newlines, it's likely a code block
  if (codeText.includes('\n')) {
    return true;
  }
  // Check if className suggests a code block (even without language prefix)
  if (className && className.trim() !== '' && !className.includes('inline')) {
    return true;
  }
  return false;
};

/** Returns true if href looks like a local file path rather than a URL. */
export function isLocalFilePath(href: string | undefined): boolean {
  if (!href) return false;
  if (
    href.startsWith('http://') ||
    href.startsWith('https://') ||
    href.startsWith('//') ||
    href.startsWith('mailto:') ||
    href.startsWith('#') ||
    href.startsWith('javascript:')
  ) {
    return false;
  }
  // Must look like a path: contains a slash, or is a bare filename with a code extension
  return href.includes('/') || /\.\w{1,10}$/.test(href);
}
