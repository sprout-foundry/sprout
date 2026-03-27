// @ts-nocheck

import { flattenMarkdownText, isMarkdownCodeBlock } from './markdownCode';

describe('markdownCode', () => {
  it('flattens nested markdown children into plain text', () => {
    expect(flattenMarkdownText(['git_', ['status'], 1])).toBe('git_status1');
  });

  it('treats single-line code without a language as inline code', () => {
    expect(isMarkdownCodeBlock(undefined, 'git status')).toBe(false);
  });

  it('treats multiline code as a block', () => {
    expect(isMarkdownCodeBlock(undefined, 'git status\npwd')).toBe(true);
  });

  it('treats language-tagged code as a block', () => {
    expect(isMarkdownCodeBlock('language-bash', 'git status')).toBe(true);
  });
});
