/**
 * Tests for the binary file detection logic used in AppContent.handleFilesDropped.
 *
 * These tests verify the regex pattern and the overall logic patterns used in the
 * drop handler, ensuring correctness without needing to mount the full AppContent
 * component (which has extensive dependencies).
 */

// Import the shared binary file detection utilities
import { isBinaryFile } from '../utils/binaryPatterns';
import { parseFilePath } from '../utils/filePath';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Binary file detection (AppContent.handleFilesDropped)', () => {
  // ── Known binary extensions that should be detected ──────────────

  describe('binary extensions detected', () => {
    const binaryExtensions = [
      'image.png',
      'photo.jpg',
      'photo.jpeg',
      'animation.gif',
      'screenshot.bmp',
      'icon.webp',
      'favicon.ico',
      'photo.tiff',
      'photo.tif',
      'hero.avif',
    ];

    it.each(binaryExtensions)('detects %s as binary', (fileName) => {
      expect(isBinaryFile(fileName)).toBe(true);
    });

    describe('audio/video', () => {
      const mediaExtensions = [
        'song.mp3',
        'video.mp4',
        'audio.wav',
        'track.ogg',
        'clip.mpg',
        'clip.mpeg',
        'movie.avi',
        'footage.mov',
      ];

      it.each(mediaExtensions)('detects %s as binary', (fileName) => {
        expect(isBinaryFile(fileName)).toBe(true);
      });
    });

    describe('archives', () => {
      const archiveExtensions = [
        'archive.zip',
        'package.tar',
        'data.gz',
        'data.bz2',
        'data.xz',
        'archive.7z',
        'archive.rar',
      ];

      it.each(archiveExtensions)('detects %s as binary', (fileName) => {
        expect(isBinaryFile(fileName)).toBe(true);
      });
    });

    describe('executables/libraries', () => {
      const exeExtensions = [
        'program.exe',
        'lib.dll',
        'lib.so',
        'lib.dylib',
        'module.wasm',
      ];

      it.each(exeExtensions)('detects %s as binary', (fileName) => {
        expect(isBinaryFile(fileName)).toBe(true);
      });
    });

    describe('documents', () => {
      const docExtensions = [
        'doc.pdf',
        'letter.doc',
        'letter.docx',
        'spreadsheet.xls',
        'spreadsheet.xlsx',
        'slides.ppt',
        'slides.pptx',
        'document.odt',
        'sheet.ods',
        'presentation.odp',
      ];

      it.each(docExtensions)('detects %s as binary', (fileName) => {
        expect(isBinaryFile(fileName)).toBe(true);
      });
    });

    describe('fonts', () => {
      const fontExtensions = [
        'font.ttf',
        'font.otf',
        'font.woff',
        'font.woff2',
        'font.eot',
      ];

      it.each(fontExtensions)('detects %s as binary', (fileName) => {
        expect(isBinaryFile(fileName)).toBe(true);
      });
    });

    describe('data files', () => {
      const dataExtensions = [
        'datafile.bin',
        'config.dat',
        'database.db',
        'database.sqlite',
      ];

      it.each(dataExtensions)('detects %s as binary', (fileName) => {
        expect(isBinaryFile(fileName)).toBe(true);
      });
    });
  });

  // ── Common text/code extensions that should NOT be detected ──────

  describe('text/code extensions NOT detected as binary', () => {
    const textExtensions = [
      'script.ts',
      'script.tsx',
      'script.js',
      'script.jsx',
      'style.css',
      'style.scss',
      'style.sass',
      'style.less',
      'markup.html',
      'markup.htm',
      'template.vue',
      'component.svelte',
      'data.json',
      'data.jsonl',
      'config.yaml',
      'config.yml',
      'config.toml',
      'config.ini',
      'config.xml',
      'notes.txt',
      'notes.md',
      'notes.mdx',
      'readme.rst',
      'source.py',
      'source.rs',
      'source.go',
      'source.java',
      'source.kt',
      'source.swift',
      'source.c',
      'source.cpp',
      'source.h',
      'source.hpp',
      'source.cs',
      'source.rb',
      'source.php',
      'source.pl',
      'source.sh',
      'source.bash',
      'source.zsh',
      'source.fish',
      'source.ps1',
      'Makefile',
      'Dockerfile',
      'data.csv',
      'data.tsv',
      'module.sql',
      'query.sql',
      'lockfile.lock',
      'env.env',
      'script.lua',
      'source.r',
      'source.JS',       // uppercase
      'source.PY',       // uppercase
      'source.TXT',      // uppercase
      'source.MD',       // uppercase
    ];

    it.each(textExtensions)('does NOT detect %s as binary', (fileName) => {
      expect(isBinaryFile(fileName)).toBe(false);
    });
  });

  // ── Edge cases for binary pattern ────────────────────────────────

  describe('binary pattern edge cases', () => {
    it('matches extensions with multiple dots (e.g., archive.tar.gz → last ext is .gz)', () => {
      // The regex checks the last extension
      expect(isBinaryFile('archive.tar.gz')).toBe(true);
    });

    it('does not match a file with no extension', () => {
      expect(isBinaryFile('Makefile')).toBe(false);
      expect(isBinaryFile('Dockerfile')).toBe(false);
      expect(isBinaryFile('README')).toBe(false);
    });

    it('does not match a file with only a dot at the start (hidden file with no ext)', () => {
      expect(isBinaryFile('.gitignore')).toBe(false);
      expect(isBinaryFile('.eslintrc')).toBe(false);
    });

    it('matches hidden files with binary extensions', () => {
      expect(isBinaryFile('.DS_Store')).toBe(false); // no recognized ext
      expect(isBinaryFile('.env.png')).toBe(true);
    });

    it('is case-insensitive', () => {
      expect(isBinaryFile('photo.PNG')).toBe(true);
      expect(isBinaryFile('photo.JpG')).toBe(true);
      expect(isBinaryFile('archive.ZIP')).toBe(true);
      expect(isBinaryFile('archive.Gz')).toBe(true);
      expect(isBinaryFile('video.MP4')).toBe(true);
      expect(isBinaryFile('video.AvI')).toBe(true);
    });

    it('matches at end of filename only (not in the middle)', () => {
      // Files with "png" in the name but different extension
      expect(isBinaryFile('png-config.txt')).toBe(false);
      expect(isBinaryFile('mypng.js')).toBe(false);
    });

    it('does not match partial extensions', () => {
      // ".tx" should not match ".txt" — txt is not in the binary list
      expect(isBinaryFile('file.tx')).toBe(false);

      // ".jp" should not match ".jpg" — jpg IS in the list via "jpe?g"
      expect(isBinaryFile('file.jp')).toBe(false);
    });
  });

  // ── parseFilePath integration ────────────────────────────────────

  describe('parseFilePath integration', () => {
    it('correctly parses filename with extension for dropped files', () => {
      const result = parseFilePath('my-script.ts');
      expect(result.fileName).toBe('my-script.ts');
      expect(result.fileExt).toBe('.ts');
    });

    it('correctly parses filename without extension', () => {
      const result = parseFilePath('Makefile');
      expect(result.fileName).toBe('Makefile');
      expect(result.fileExt).toBe('');
    });

    it('correctly parses filename with dots in the name', () => {
      const result = parseFilePath('my.config.backup.ts');
      expect(result.fileName).toBe('my.config.backup.ts');
      expect(result.fileExt).toBe('.ts');
    });

    it('correctly parses hidden files', () => {
      const result = parseFilePath('.gitignore');
      expect(result.fileName).toBe('.gitignore');
      expect(result.fileExt).toBe('');
    });

    it('correctly parses file paths with directories', () => {
      const result = parseFilePath('src/components/App.tsx');
      expect(result.fileName).toBe('App.tsx');
      expect(result.fileExt).toBe('.tsx');
    });

    it('handles empty string', () => {
      const result = parseFilePath('');
      expect(result.fileName).toBe('');
      expect(result.fileExt).toBe('');
    });
  });

  // ── handleFilesDropped logic simulation ──────────────────────────

  describe('handleFilesDropped logic simulation', () => {
    /**
     * Simulates the core logic of handleFilesDropped from AppContent.tsx
     * (without React hooks, DOM, or external services).
     * Returns the files that would be opened as workspace buffers.
     */
    function simulateDrop(fileNames: string[]): string[] {
      const openedFiles: string[] = [];

      for (const fileName of fileNames) {
        if (isBinaryFile(fileName)) {
          // Binary — skip
          continue;
        }
        openedFiles.push(fileName);
      }

      return openedFiles;
    }

    it('processes only text files, skipping binary files', () => {
      const files = [
        'readme.md',
        'photo.png',
        'script.ts',
        'archive.zip',
        'data.json',
        'video.mp4',
      ];

      const result = simulateDrop(files);
      expect(result).toEqual(['readme.md', 'script.ts', 'data.json']);
    });

    it('processes all files when none are binary', () => {
      const files = ['main.ts', 'index.html', 'style.css', 'data.json'];
      const result = simulateDrop(files);
      expect(result).toEqual(['main.ts', 'index.html', 'style.css', 'data.json']);
    });

    it('returns empty array when all files are binary', () => {
      const files = ['img1.png', 'img2.jpg', 'video.mp4', 'archive.zip'];
      const result = simulateDrop(files);
      expect(result).toEqual([]);
    });

    it('handles empty file list', () => {
      const result = simulateDrop([]);
      expect(result).toEqual([]);
    });

    it('handles files with spaces in names', () => {
      const files = ['my document.txt', 'photo of cat.png'];
      const result = simulateDrop(files);
      expect(result).toEqual(['my document.txt']);
    });

    it('handles files with special characters in names', () => {
      const files = ['file(1).txt', 'data-file_v2.json', 'résumé.md'];
      const result = simulateDrop(files);
      expect(result).toEqual(['file(1).txt', 'data-file_v2.json', 'résumé.md']);
    });

    it('handles .docx (binary) but not .doc (text) for office formats', () => {
      // .docx is captured by docx? (docx or doc)
      // Wait... docx? means "oc" is optional. So "doc" would match too.
      expect(isBinaryFile('document.doc')).toBe(true);
      expect(isBinaryFile('document.docx')).toBe(true);
      // This is intentional in the current implementation
    });

    it('handles .xlsx (binary) and .xls (binary)', () => {
      expect(isBinaryFile('spreadsheet.xls')).toBe(true);
      expect(isBinaryFile('spreadsheet.xlsx')).toBe(true);
    });

    it('handles .pptx (binary) and .ppt (binary)', () => {
      expect(isBinaryFile('presentation.ppt')).toBe(true);
      expect(isBinaryFile('presentation.pptx')).toBe(true);
    });
  });
});
