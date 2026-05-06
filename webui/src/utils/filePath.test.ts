import { describe, it, expect } from 'vitest';
import { parseFilePath } from './filePath';

describe('parseFilePath', () => {
  it('parses a simple filename', () => {
    expect(parseFilePath('main.go')).toEqual({
      fileName: 'main.go',
      fileExt: '.go',
    });
  });

  it('parses a nested path', () => {
    expect(parseFilePath('src/utils/filePath.ts')).toEqual({
      fileName: 'filePath.ts',
      fileExt: '.ts',
    });
  });

  it('parses a deeply nested path', () => {
    expect(parseFilePath('a/b/c/d/e/test.txt')).toEqual({
      fileName: 'test.txt',
      fileExt: '.txt',
    });
  });

  it('returns empty extension for files without extension', () => {
    expect(parseFilePath('Makefile')).toEqual({
      fileName: 'Makefile',
      fileExt: '',
    });
  });

  it('handles hidden files like .gitignore (no extension)', () => {
    expect(parseFilePath('.gitignore')).toEqual({
      fileName: '.gitignore',
      fileExt: '',
    });
  });

  it('handles hidden files in nested paths', () => {
    expect(parseFilePath('home/user/.bashrc')).toEqual({
      fileName: '.bashrc',
      fileExt: '',
    });
  });

  it('handles compound extensions like .tar.gz', () => {
    const result = parseFilePath('archive.tar.gz');
    expect(result.fileName).toBe('archive.tar.gz');
    expect(result.fileExt).toBe('.gz');
  });

  it('handles trailing slashes', () => {
    expect(parseFilePath('src/utils/')).toEqual({
      fileName: 'utils',
      fileExt: '',
    });
  });

  it('handles path with multiple trailing slashes', () => {
    expect(parseFilePath('src/utils///')).toEqual({
      fileName: 'utils',
      fileExt: '',
    });
  });

  it('returns raw path for empty string', () => {
    expect(parseFilePath('')).toEqual({
      fileName: '',
      fileExt: '',
    });
  });

  it('handles root-like path', () => {
    expect(parseFilePath('/')).toEqual({
      fileName: '/',
      fileExt: '',
    });
  });

  it('handles absolute paths', () => {
    expect(parseFilePath('/home/user/docs/report.pdf')).toEqual({
      fileName: 'report.pdf',
      fileExt: '.pdf',
    });
  });

  it('handles files with multiple dots', () => {
    expect(parseFilePath('config.test.go')).toEqual({
      fileName: 'config.test.go',
      fileExt: '.go',
    });
  });

  it('handles only dots in filename', () => {
    expect(parseFilePath('...')).toEqual({
      fileName: '...',
      fileExt: '.',
    });
  });
});
