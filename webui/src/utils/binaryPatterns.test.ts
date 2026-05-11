import { describe, it, expect } from 'vitest';
import { isBinaryFile, BINARY_FILE_PATTERN } from './binaryPatterns';

describe('BINARY_FILE_PATTERN', () => {
  it('is a RegExp instance', () => {
    expect(BINARY_FILE_PATTERN).toBeInstanceOf(RegExp);
  });
});

describe('isBinaryFile', () => {
  describe('returns true for image files', () => {
    it.each([
      'photo.png',
      'image.jpg',
      'image.jpeg',
      'icon.gif',
      'screen.bmp',
      'avatar.webp',
      'logo.ico',
      'scan.tiff',
      'scan.tif',
      'photo.avif',
    ])('"%s" is binary', (fileName) => {
      expect(isBinaryFile(fileName)).toBe(true);
    });
  });

  describe('returns true for audio/video files', () => {
    it.each(['song.mp3', 'song.mp4', 'audio.wav', 'music.ogg', 'clip.mpg', 'movie.mpeg', 'video.avi', 'clip.mov'])(
      '"%s" is binary',
      (fileName) => {
        expect(isBinaryFile(fileName)).toBe(true);
      },
    );
  });

  describe('returns true for archive files', () => {
    it.each(['archive.zip', 'archive.tar', 'archive.gz', 'archive.bz2', 'archive.xz', 'archive.7z', 'archive.rar'])(
      '"%s" is binary',
      (fileName) => {
        expect(isBinaryFile(fileName)).toBe(true);
      },
    );
  });

  describe('returns true for executable files', () => {
    it.each(['program.exe', 'library.dll', 'lib.so', 'lib.dylib', 'module.wasm'])('"%s" is binary', (fileName) => {
      expect(isBinaryFile(fileName)).toBe(true);
    });
  });

  describe('returns true for document and data files', () => {
    it.each([
      'document.pdf',
      'report.doc',
      'report.docx',
      'spreadsheet.xls',
      'spreadsheet.xlsx',
      'presentation.ppt',
      'presentation.pptx',
      'doc.odt',
      'sheet.ods',
      'slide.odp',
      'font.ttf',
      'font.otf',
      'font.woff',
      'font.woff2',
      'font.eot',
      'data.bin',
      'data.dat',
      'database.db',
      'database.sqlite',
    ])('"%s" is binary', (fileName) => {
      expect(isBinaryFile(fileName)).toBe(true);
    });
  });

  describe('returns false for text files', () => {
    it.each([
      'readme.txt',
      'main.go',
      'app.js',
      'style.css',
      'config.json',
      'page.html',
      'script.py',
      'program.rs',
      'code.java',
      'source.c',
      'header.h',
      'script.sh',
      'config.yml',
      'config.yaml',
      'config.toml',
      'config.ini',
      'Makefile',
      'data.sql',
      'script.rb',
      'page.php',
      'script.lua',
    ])('"%s" is not binary', (fileName) => {
      expect(isBinaryFile(fileName)).toBe(false);
    });
  });

  describe('returns false for files without extension', () => {
    it.each(['Makefile', 'LICENSE', 'README', 'Dockerfile', 'vendiror'])('"%s" is not binary', (fileName) => {
      expect(isBinaryFile(fileName)).toBe(false);
    });
  });

  describe('case insensitivity', () => {
    it('matches uppercase extensions', () => {
      expect(isBinaryFile('IMAGE.PNG')).toBe(true);
      expect(isBinaryFile('FILE.JPG')).toBe(true);
      expect(isBinaryFile('FILE.EXE')).toBe(true);
      expect(isBinaryFile('FILE.PDF')).toBe(true);
    });

    it('matches mixed-case extensions', () => {
      expect(isBinaryFile('photo.JpEg')).toBe(true);
      expect(isBinaryFile('archive.ZIP')).toBe(true);
      expect(isBinaryFile('program.Exe')).toBe(true);
    });

    it('does not match text files even with uppercase extensions', () => {
      expect(isBinaryFile('main.GO')).toBe(false);
      expect(isBinaryFile('style.CSS')).toBe(false);
      expect(isBinaryFile('data.JSON')).toBe(false);
    });
  });

  describe('edge cases', () => {
    it('returns false for empty string', () => {
      expect(isBinaryFile('')).toBe(false);
    });

    it('returns false for path with no extension', () => {
      expect(isBinaryFile('src/utils/somefile')).toBe(false);
    });

    it('matches extension at end of path', () => {
      expect(isBinaryFile('deep/nested/path/image.png')).toBe(true);
      expect(isBinaryFile('deep/nested/path/readme.txt')).toBe(false);
    });

    it('handles files with multiple dots', () => {
      expect(isBinaryFile('archive.backup.tar')).toBe(true);
      expect(isBinaryFile('image.thumbnail.png')).toBe(true);
    });
  });
});
