import { describe, it, expect } from 'vitest';
import {
  isImageFile,
  isAudioFile,
  isVideoFile,
  isMediaFile,
  isBinaryFile,
  isTextFile,
  getMediaCategory,
} from './mediaPatterns';

describe('isImageFile', () => {
  it('returns true for known image extensions', () => {
    for (const ext of ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'ico', 'tiff', 'tif', 'avif']) {
      expect(isImageFile(ext), `"${ext}" should be an image`).toBe(true);
    }
  });

  it('handles dot-prefixed extensions', () => {
    for (const ext of ['.png', '.jpg', '.jpeg', '.gif', '.webp']) {
      expect(isImageFile(ext), `"${ext}" should be an image`).toBe(true);
    }
  });

  it('is case-insensitive', () => {
    expect(isImageFile('PNG')).toBe(true);
    expect(isImageFile('Jpg')).toBe(true);
    expect(isImageFile('.JPEG')).toBe(true);
  });

  it('returns false for non-image extensions', () => {
    expect(isImageFile('txt')).toBe(false);
    expect(isImageFile('mp3')).toBe(false);
    expect(isImageFile('go')).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isImageFile(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isImageFile('')).toBe(false);
  });
});

describe('isAudioFile', () => {
  it('returns true for known audio extensions', () => {
    for (const ext of ['mp3', 'wav', 'ogg', 'flac', 'aac', 'm4a', 'wma', 'opus', 'weba', 'mid', 'midi', 'mp4a']) {
      expect(isAudioFile(ext), `"${ext}" should be an audio`).toBe(true);
    }
  });

  it('handles dot-prefixed extensions', () => {
    for (const ext of ['.mp3', '.wav', '.ogg', '.flac', '.aac', '.m4a']) {
      expect(isAudioFile(ext), `"${ext}" should be an audio`).toBe(true);
    }
  });

  it('is case-insensitive', () => {
    expect(isAudioFile('MP3')).toBe(true);
    expect(isAudioFile('Wav')).toBe(true);
    expect(isAudioFile('.FLAC')).toBe(true);
  });

  it('returns false for non-audio extensions', () => {
    expect(isAudioFile('png')).toBe(false);
    expect(isAudioFile('mp4')).toBe(false);
    expect(isAudioFile('txt')).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isAudioFile(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isAudioFile('')).toBe(false);
  });
});

describe('isVideoFile', () => {
  it('returns true for known video extensions', () => {
    for (const ext of ['mp4', 'webm', 'mov', 'avi', 'mkv', 'm4v', 'flv', 'wmv', 'ogv', '3gp']) {
      expect(isVideoFile(ext), `"${ext}" should be a video`).toBe(true);
    }
  });

  it('handles dot-prefixed extensions', () => {
    for (const ext of ['.mp4', '.webm', '.mov', '.avi', '.mkv']) {
      expect(isVideoFile(ext), `"${ext}" should be a video`).toBe(true);
    }
  });

  it('is case-insensitive', () => {
    expect(isVideoFile('MP4')).toBe(true);
    expect(isVideoFile('Mov')).toBe(true);
    expect(isVideoFile('.WEBM')).toBe(true);
  });

  it('returns false for non-video extensions', () => {
    expect(isVideoFile('png')).toBe(false);
    expect(isVideoFile('mp3')).toBe(false);
    expect(isVideoFile('go')).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isVideoFile(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isVideoFile('')).toBe(false);
  });
});

describe('isMediaFile', () => {
  it('returns true for any image, audio, or video extension', () => {
    expect(isMediaFile('png')).toBe(true);
    expect(isMediaFile('mp3')).toBe(true);
    expect(isMediaFile('mp4')).toBe(true);
    expect(isMediaFile('.webm')).toBe(true);
    expect(isMediaFile('.gif')).toBe(true);
    expect(isMediaFile('.ogg')).toBe(true);
  });

  it('returns false for non-media extensions', () => {
    expect(isMediaFile('txt')).toBe(false);
    expect(isMediaFile('go')).toBe(false);
    expect(isMediaFile('zip')).toBe(false);
    expect(isMediaFile('pdf')).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isMediaFile(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isMediaFile('')).toBe(false);
  });

  it('is case-insensitive', () => {
    expect(isMediaFile('PNG')).toBe(true);
    expect(isMediaFile('MP3')).toBe(true);
    expect(isMediaFile('MP4')).toBe(true);
  });
});

describe('isBinaryFile', () => {
  it('returns true for known binary extensions', () => {
    for (const ext of [
      'zip',
      'tar',
      'gz',
      'bz2',
      'xz',
      '7z',
      'rar',
      'zst',
      'tgz',
      'exe',
      'dll',
      'so',
      'dylib',
      'bin',
      'app',
      'dat',
      'pdf',
      'doc',
      'docx',
      'xls',
      'xlsx',
      'ppt',
      'pptx',
      'odt',
      'ods',
      'db',
      'sqlite',
      'sqlite3',
      'woff',
      'woff2',
      'ttf',
      'otf',
      'eot',
      'class',
      'o',
      'obj',
      'pyc',
      'pyo',
      'wasm',
      'iso',
      'dmg',
      'apk',
      'deb',
      'rpm',
      'jar',
      'war',
      'pkl',
      'pickle',
      'parquet',
      'arrow',
    ]) {
      expect(isBinaryFile(ext), `"${ext}" should be binary`).toBe(true);
    }
  });

  it('handles dot-prefixed extensions', () => {
    expect(isBinaryFile('.zip')).toBe(true);
    expect(isBinaryFile('.exe')).toBe(true);
    expect(isBinaryFile('.pdf')).toBe(true);
    expect(isBinaryFile('.wasm')).toBe(true);
  });

  it('is case-insensitive', () => {
    expect(isBinaryFile('ZIP')).toBe(true);
    expect(isBinaryFile('Exe')).toBe(true);
    expect(isBinaryFile('.PDF')).toBe(true);
  });

  it('returns false for text extensions', () => {
    expect(isBinaryFile('txt')).toBe(false);
    expect(isBinaryFile('go')).toBe(false);
    expect(isBinaryFile('json')).toBe(false);
    expect(isBinaryFile('py')).toBe(false);
  });

  it('returns false for media extensions (image/audio/video)', () => {
    // In mediaPatterns, image/audio/video are NOT in the BINARY_EXT set
    expect(isBinaryFile('png')).toBe(false);
    expect(isBinaryFile('mp3')).toBe(false);
    expect(isBinaryFile('mp4')).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isBinaryFile(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isBinaryFile('')).toBe(false);
  });
});

describe('isTextFile', () => {
  it('returns true for known text extensions', () => {
    for (const ext of [
      'txt',
      'md',
      'json',
      'xml',
      'html',
      'css',
      'js',
      'ts',
      'tsx',
      'jsx',
      'go',
      'py',
      'rs',
      'java',
      'c',
      'h',
      'cpp',
      'hpp',
      'sh',
      'bash',
      'zsh',
      'fish',
      'yml',
      'yaml',
      'toml',
      'ini',
      'cfg',
      'conf',
      'env',
      'gitignore',
      'dockerfile',
      'makefile',
      'cmake',
      'gradle',
      'sql',
      'r',
      'rb',
      'php',
      'pl',
      'lua',
      'vim',
      'el',
      'clj',
      'hs',
      'ml',
      'ex',
      'exs',
      'erl',
      'swift',
      'kt',
      'scala',
      'dart',
      'vue',
      'svelte',
      'astro',
      'graphql',
      'proto',
      'grpc',
      'tf',
      'hcl',
      'mod',
      'sum',
      'log',
      'csv',
      'tsv',
      'svg',
      'rst',
      'adoc',
      'tex',
      'org',
    ]) {
      expect(isTextFile(ext), `"${ext}" should be text`).toBe(true);
    }
  });

  it('handles dot-prefixed extensions', () => {
    expect(isTextFile('.txt')).toBe(true);
    expect(isTextFile('.json')).toBe(true);
    expect(isTextFile('.go')).toBe(true);
    expect(isTextFile('.yaml')).toBe(true);
  });

  it('is case-insensitive', () => {
    expect(isTextFile('TXT')).toBe(true);
    expect(isTextFile('Go')).toBe(true);
    expect(isTextFile('.JSON')).toBe(true);
  });

  it('returns false for binary extensions', () => {
    expect(isTextFile('zip')).toBe(false);
    expect(isTextFile('exe')).toBe(false);
    expect(isTextFile('pdf')).toBe(false);
  });

  it('returns false for media extensions', () => {
    expect(isTextFile('png')).toBe(false);
    expect(isTextFile('mp3')).toBe(false);
    expect(isTextFile('mp4')).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isTextFile(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isTextFile('')).toBe(false);
  });
});

describe('getMediaCategory', () => {
  it('returns "image" for image extensions', () => {
    expect(getMediaCategory('png')).toBe('image');
    expect(getMediaCategory('.jpg')).toBe('image');
    expect(getMediaCategory('JPEG')).toBe('image');
    expect(getMediaCategory('gif')).toBe('image');
    expect(getMediaCategory('.webp')).toBe('image');
    expect(getMediaCategory('avif')).toBe('image');
  });

  it('returns "audio" for audio extensions', () => {
    expect(getMediaCategory('mp3')).toBe('audio');
    expect(getMediaCategory('.wav')).toBe('audio');
    expect(getMediaCategory('OGG')).toBe('audio');
    expect(getMediaCategory('flac')).toBe('audio');
    expect(getMediaCategory('.m4a')).toBe('audio');
    expect(getMediaCategory('opus')).toBe('audio');
  });

  it('returns "video" for video extensions', () => {
    expect(getMediaCategory('mp4')).toBe('video');
    expect(getMediaCategory('.webm')).toBe('video');
    expect(getMediaCategory('MOV')).toBe('video');
    expect(getMediaCategory('avi')).toBe('video');
    expect(getMediaCategory('.mkv')).toBe('video');
  });

  it('returns null for non-media extensions', () => {
    expect(getMediaCategory('txt')).toBe(null);
    expect(getMediaCategory('go')).toBe(null);
    expect(getMediaCategory('zip')).toBe(null);
    expect(getMediaCategory('pdf')).toBe(null);
    expect(getMediaCategory('json')).toBe(null);
  });

  it('returns null for undefined', () => {
    expect(getMediaCategory(undefined)).toBe(null);
  });

  it('returns null for empty string', () => {
    expect(getMediaCategory('')).toBe(null);
  });

  it('is case-insensitive', () => {
    expect(getMediaCategory('PNG')).toBe('image');
    expect(getMediaCategory('MP3')).toBe('audio');
    expect(getMediaCategory('MP4')).toBe('video');
  });
});
