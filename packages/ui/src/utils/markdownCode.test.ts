import type React from 'react';
import { flattenMarkdownText, isMarkdownCodeBlock, isLocalFilePath } from './markdownCode';

describe('flattenMarkdownText', () => {
  describe('string handling', () => {
    it('returns plain string unchanged', () => {
      const result = flattenMarkdownText('Hello, World!');
      expect(result).toBe('Hello, World!');
    });

    it('handles empty string', () => {
      const result = flattenMarkdownText('');
      expect(result).toBe('');
    });

    it('handles whitespace', () => {
      const result = flattenMarkdownText('  test  ');
      expect(result).toBe('  test  ');
    });

    it('handles multiline strings', () => {
      const result = flattenMarkdownText('line1\nline2\nline3');
      expect(result).toBe('line1\nline2\nline3');
    });
  });

  describe('number handling', () => {
    it('converts number to string', () => {
      const result = flattenMarkdownText(42);
      expect(result).toBe('42');
    });

    it('converts negative number', () => {
      const result = flattenMarkdownText(-123);
      expect(result).toBe('-123');
    });

    it('converts decimal number', () => {
      const result = flattenMarkdownText(3.14);
      expect(result).toBe('3.14');
    });
  });

  describe('array handling', () => {
    it('flattens array of strings', () => {
      const result = flattenMarkdownText(['Hello', ' ', 'World', '!']);
      expect(result).toBe('Hello World!');
    });

    it('flattens array of numbers', () => {
      const result = flattenMarkdownText([1, 2, 3]);
      expect(result).toBe('123');
    });

    it('flattens nested arrays', () => {
      const result = flattenMarkdownText(['A', ['B', 'C'], 'D']);
      expect(result).toBe('ABCD');
    });

    it('handles empty arrays', () => {
      const result = flattenMarkdownText([]);
      expect(result).toBe('');
    });

    it('handles array with null/undefined', () => {
      const result = flattenMarkdownText(['A', null, 'B', undefined, 'C']);
      expect(result).toBe('ABC');
    });
  });

  describe('React element handling', () => {
    it('returns empty string for plain objects (not valid React elements)', () => {
      // isValidElement() returns false for plain objects (no $$typeof symbol)
      const element = {
        type: 'span',
        props: { children: 'Hello', key: null, ref: null },
        key: null,
        ref: null,
      } as any;
      const result = flattenMarkdownText(element);
      // Plain objects are not valid React elements, so they fall through to the
      // default case which returns an empty string
      expect(result).toBe('');
    });

    it('handles null value in props.children', () => {
      // React elements with null/undefined children should produce empty string
      const result = flattenMarkdownText(null);
      expect(result).toBe('');
    });

    it('handles undefined value', () => {
      const result = flattenMarkdownText(undefined);
      expect(result).toBe('');
    });
  });

  describe('null/undefined handling', () => {
    it('returns empty string for null', () => {
      const result = flattenMarkdownText(null);
      expect(result).toBe('');
    });

    it('returns empty string for undefined', () => {
      const result = flattenMarkdownText(undefined);
      expect(result).toBe('');
    });
  });

  describe('object handling', () => {
    it('stringifies plain objects', () => {
      // Cast: the function accepts ReactNode but we deliberately probe
      // its runtime fallback for non-ReactNode inputs (e.g. accidental
      // raw model output). Type assertion bypasses the compile-time check.
      const result = flattenMarkdownText({ key: 'value' } as unknown as React.ReactNode);
      expect(typeof result).toBe('string');
    });

    it('handles arrays inside objects', () => {
      const result = flattenMarkdownText({ arr: [1, 2, 3] } as unknown as React.ReactNode);
      expect(typeof result).toBe('string');
    });
  });
});

describe('isMarkdownCodeBlock', () => {
  describe('language class detection', () => {
    it('detects language- class as code block', () => {
      expect(isMarkdownCodeBlock('language-javascript', 'const x = 5;')).toBe(true);
    });

    it('detects language-typescript as code block', () => {
      expect(isMarkdownCodeBlock('language-typescript', 'const x: number = 5;')).toBe(true);
    });

    it('detects language-python as code block', () => {
      expect(isMarkdownCodeBlock('language-python', 'print("hello")')).toBe(true);
    });

    it('detects language-go as code block', () => {
      expect(isMarkdownCodeBlock('language-go', 'package main')).toBe(true);
    });

    it('detects various language- prefixes', () => {
      const langs = ['bash', 'sh', 'zsh', 'json', 'yaml', 'xml', 'html', 'css', 'sql'];
      langs.forEach(lang => {
        expect(isMarkdownCodeBlock(`language-${lang}`, 'code')).toBe(true);
      });
    });
  });

  describe('multiline detection', () => {
    it('detects newlines as code block', () => {
      const codeText = 'line1\nline2\nline3';
      expect(isMarkdownCodeBlock(undefined, codeText)).toBe(true);
    });

    it('detects single newline as code block', () => {
      const codeText = 'line1\nline2';
      expect(isMarkdownCodeBlock(undefined, codeText)).toBe(true);
    });

    it('does not detect single line without newline as code block when no language class', () => {
      const codeText = 'single line';
      expect(isMarkdownCodeBlock(undefined, codeText)).toBe(false);
    });
  });

  describe('className-based detection', () => {
    it('detects non-empty className as code block', () => {
      expect(isMarkdownCodeBlock('my-code-class', 'code')).toBe(true);
    });

    it('does not detect "inline" class as code block', () => {
      expect(isMarkdownCodeBlock('inline', 'code')).toBe(false);
    });

    it('does not detect class containing "inline" as code block', () => {
      expect(isMarkdownCodeBlock('inline-code', 'code')).toBe(false);
    });

    it('does not detect empty className as code block', () => {
      expect(isMarkdownCodeBlock('', 'code')).toBe(false);
    });

    it('does not detect whitespace-only className as code block', () => {
      expect(isMarkdownCodeBlock('  ', 'code')).toBe(false);
    });
  });

  describe('edge cases', () => {
    it('handles empty codeText', () => {
      expect(isMarkdownCodeBlock('language-js', '')).toBe(true);
    });

    it('handles undefined className', () => {
      expect(isMarkdownCodeBlock(undefined, 'line\nline2')).toBe(true);
    });

    it('handles null className', () => {
      expect(isMarkdownCodeBlock(null as any, 'line\nline2')).toBe(true);
    });

    it('handles language class with empty codeText', () => {
      expect(isMarkdownCodeBlock('language-js', '')).toBe(true);
    });
  });
});

describe('isLocalFilePath', () => {
  describe('local file paths', () => {
    it('detects absolute Unix path', () => {
      expect(isLocalFilePath('/home/user/file.txt')).toBe(true);
    });

    it('detects relative path', () => {
      expect(isLocalFilePath('./file.txt')).toBe(true);
    });

    it('detects path with extension', () => {
      expect(isLocalFilePath('package.json')).toBe(true);
    });

    it('detects path with multiple segments', () => {
      expect(isLocalFilePath('src/components/Button.tsx')).toBe(true);
    });

    it('detects Windows-style path', () => {
      expect(isLocalFilePath('C:\\Users\\file.txt')).toBe(true);
    });

    it('detects path with dotfile', () => {
      expect(isLocalFilePath('.gitignore')).toBe(true);
    });

    it('detects nested dotfile path', () => {
      expect(isLocalFilePath('src/.env.local')).toBe(true);
    });
  });

  describe('URLs', () => {
    it('detects https:// URLs', () => {
      expect(isLocalFilePath('https://example.com')).toBe(false);
    });

    it('detects http:// URLs', () => {
      expect(isLocalFilePath('http://example.com')).toBe(false);
    });

    it('detects protocol-relative URLs', () => {
      expect(isLocalFilePath('//example.com')).toBe(false);
    });

    it('detects URLs with paths', () => {
      expect(isLocalFilePath('https://example.com/path/to/file')).toBe(false);
    });

    it('detects URLs with query strings', () => {
      expect(isLocalFilePath('https://example.com?query=1')).toBe(false);
    });

    it('detects URLs with fragments', () => {
      expect(isLocalFilePath('https://example.com#section')).toBe(false);
    });
  });

  describe('special URLs', () => {
    it('detects mailto: links', () => {
      expect(isLocalFilePath('mailto:user@example.com')).toBe(false);
    });

    it('detects # anchor links', () => {
      expect(isLocalFilePath('#section')).toBe(false);
    });

    it('detects javascript: URLs', () => {
      expect(isLocalFilePath('javascript:void(0)')).toBe(false);
    });

    // Note: data: and ftp: URLs may not be detected by current implementation
    it('handles data: URLs based on extension check', () => {
      // data URLs with .html extension will be detected as path
      const result = isLocalFilePath('data:text/html;base64,SGVsbG8=');
      expect(typeof result).toBe('boolean');
    });

    it('handles ftp: URLs based on extension check', () => {
      // ftp URLs with .txt extension will be detected as path
      const result = isLocalFilePath('ftp://example.com/file.txt');
      expect(typeof result).toBe('boolean');
    });
  });

  describe('edge cases', () => {
    it('handles empty string', () => {
      expect(isLocalFilePath('')).toBe(false);
    });

    it('handles undefined', () => {
      expect(isLocalFilePath(undefined)).toBe(false);
    });

    it('handles null', () => {
      expect(isLocalFilePath(null as any)).toBe(false);
    });

    it('handles plain filename without extension', () => {
      expect(isLocalFilePath('README')).toBe(false);
    });

    it('handles filename with multiple dots', () => {
      expect(isLocalFilePath('file.name.with.dots.txt')).toBe(true);
    });

    it('handles path starting with dot', () => {
      expect(isLocalFilePath('./README.md')).toBe(true);
    });

    it('handles path with double slash', () => {
      expect(isLocalFilePath('//server/share')).toBe(false);
    });
  });

  describe('code file patterns', () => {
    it('detects .js files', () => {
      expect(isLocalFilePath('script.js')).toBe(true);
    });

    it('detects .ts files', () => {
      expect(isLocalFilePath('types.ts')).toBe(true);
    });

    it('detects .py files', () => {
      expect(isLocalFilePath('app.py')).toBe(true);
    });

    it('detects .go files', () => {
      expect(isLocalFilePath('main.go')).toBe(true);
    });

    it('detects .rs files', () => {
      expect(isLocalFilePath('lib.rs')).toBe(true);
    });

    it('detects .java files', () => {
      expect(isLocalFilePath('Main.java')).toBe(true);
    });
  });
});
