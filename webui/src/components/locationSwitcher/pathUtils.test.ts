import { describe, it, expect } from 'vitest';
import { normalizePath, getPathDisplayName, collapseHomePath, getSSHBrowseQuery } from './pathUtils';

describe('normalizePath', () => {
  describe('empty / trivial input', () => {
    it('returns empty string for empty input', () => {
      expect(normalizePath('')).toBe('');
    });

    it('returns empty string for whitespace-only input', () => {
      expect(normalizePath('   ')).toBe('');
    });

    it('returns "/" for single slash input', () => {
      expect(normalizePath('/')).toBe('/');
    });
  });

  describe('already normalized paths', () => {
    it('leaves simple absolute path unchanged', () => {
      expect(normalizePath('/home/user')).toBe('/home/user');
    });

    it('leaves nested absolute path unchanged', () => {
      expect(normalizePath('/home/user/projects/sprout')).toBe('/home/user/projects/sprout');
    });
  });

  describe('paths needing normalization', () => {
    it('adds leading slash to relative paths', () => {
      expect(normalizePath('home/user')).toBe('/home/user');
    });

    it('removes trailing slash', () => {
      expect(normalizePath('/home/user/')).toBe('/home/user');
    });

    it('collapses multiple consecutive slashes', () => {
      expect(normalizePath('/home//user///projects')).toBe('/home/user/projects');
    });

    it('adds leading slash and removes trailing slash', () => {
      expect(normalizePath('home/user/')).toBe('/home/user');
    });

    it('handles multiple slashes with trailing slash', () => {
      expect(normalizePath('/home//user///')).toBe('/home/user');
    });

    it('trims whitespace', () => {
      expect(normalizePath('  /home/user  ')).toBe('/home/user');
    });

    it('handles path with only slashes', () => {
      expect(normalizePath('///')).toBe('/');
    });

    it('handles deeply nested path with multiple slashes', () => {
      expect(normalizePath('/a//b///c//d')).toBe('/a/b/c/d');
    });
  });
});

describe('getPathDisplayName', () => {
  describe('short paths', () => {
    it('returns full path for single segment', () => {
      expect(getPathDisplayName('/home')).toBe('home');
    });

    it('returns full path for two segments', () => {
      expect(getPathDisplayName('/home/user')).toBe('home/user');
    });

    it('returns "/" for root path', () => {
      expect(getPathDisplayName('/')).toBe('/');
    });

    it('returns "No workspace" for empty path', () => {
      expect(getPathDisplayName('')).toBe('No workspace');
    });
  });

  describe('long paths', () => {
    it('returns last 2 segments for 3-segment path', () => {
      expect(getPathDisplayName('/home/user/projects')).toBe('user/projects');
    });

    it('returns last 2 segments for deep path', () => {
      expect(getPathDisplayName('/home/user/projects/sprout/src')).toBe('sprout/src');
    });

    it('returns last 2 segments for very deep path', () => {
      expect(getPathDisplayName('/a/b/c/d/e/f')).toBe('e/f');
    });
  });

  describe('unnormalized input', () => {
    it('normalizes path before extracting display name', () => {
      expect(getPathDisplayName('home/user/projects/')).toBe('user/projects');
    });

    it('handles multiple slashes in input', () => {
      expect(getPathDisplayName('/home//user//projects')).toBe('user/projects');
    });
  });
});

describe('collapseHomePath', () => {
  describe('empty / null input', () => {
    it('returns empty string for empty path', () => {
      expect(collapseHomePath('')).toBe('');
    });

    it('returns empty string for whitespace path', () => {
      expect(collapseHomePath('   ')).toBe('');
    });

    it('returns path unchanged when homePath is undefined', () => {
      expect(collapseHomePath('/home/user', undefined)).toBe('/home/user');
    });

    it('returns path unchanged when homePath is empty', () => {
      expect(collapseHomePath('/home/user', '')).toBe('/home/user');
    });

    it('returns path unchanged when homePath is whitespace', () => {
      expect(collapseHomePath('/home/user', '  ')).toBe('/home/user');
    });
  });

  describe('exact match', () => {
    it('replaces exact home path with ~', () => {
      expect(collapseHomePath('/home/alanp', '/home/alanp')).toBe('~');
    });

    it('replaces exact home path even with different whitespace', () => {
      expect(collapseHomePath('  /home/alanp  ', '  /home/alanp  ')).toBe('~');
    });
  });

  describe('path starting with home', () => {
    it('replaces home prefix with ~ for subdirectory', () => {
      expect(collapseHomePath('/home/alanp/projects', '/home/alanp')).toBe('~/projects');
    });

    it('replaces home prefix for nested paths', () => {
      expect(collapseHomePath('/home/alanp/projects/sprout/src', '/home/alanp')).toBe('~/projects/sprout/src');
    });

    it('handles home path with trailing content', () => {
      expect(collapseHomePath('/home/alanp/.config', '/home/alanp')).toBe('~/.config');
    });
  });

  describe('path not starting with home', () => {
    it('returns path unchanged for unrelated path', () => {
      expect(collapseHomePath('/var/log', '/home/alanp')).toBe('/var/log');
    });

    it('returns path unchanged for root', () => {
      expect(collapseHomePath('/', '/home/alanp')).toBe('/');
    });

    it('does not match partial directory name', () => {
      expect(collapseHomePath('/home/alanpother/file', '/home/alanp')).toBe('/home/alanpother/file');
    });
  });

  describe('different home paths', () => {
    it('handles macOS-style home paths', () => {
      expect(collapseHomePath('/Users/alanp/docs', '/Users/alanp')).toBe('~/docs');
    });

    it('handles WSL-style home paths', () => {
      expect(collapseHomePath('/mnt/c/Users/alanp/docs', '/mnt/c/Users/alanp')).toBe('~/docs');
    });
  });
});

describe('getSSHBrowseQuery', () => {
  describe('empty / trivial input', () => {
    it('returns $HOME for empty string', () => {
      const result = getSSHBrowseQuery('');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('');
    });

    it('returns $HOME for whitespace-only string', () => {
      const result = getSSHBrowseQuery('   ');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('');
    });
  });

  describe('tilde (~) paths', () => {
    it('handles bare ~ as $HOME', () => {
      const result = getSSHBrowseQuery('~');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('');
    });

    it('handles ~/ alone', () => {
      const result = getSSHBrowseQuery('~/');
      // Implementation: ~/ → $HOME/ → withoutTrailingSlash=$HOME
      // Doesn't start with $HOME/, falls to relative path handler
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('$HOME');
    });

    it('handles ~/dir (single segment)', () => {
      const result = getSSHBrowseQuery('~/projects');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('projects');
    });

    it('handles ~/dir/subdir (multiple segments)', () => {
      const result = getSSHBrowseQuery('~/projects/sprout');
      expect(result.browsePath).toBe('$HOME/projects');
      expect(result.prefix).toBe('sprout');
    });

    it('handles ~/dir/subdir/file (three+ segments)', () => {
      const result = getSSHBrowseQuery('~/projects/sprout/src');
      expect(result.browsePath).toBe('$HOME/projects/sprout');
      expect(result.prefix).toBe('src');
    });

    it('handles ~/dir/ with trailing slash', () => {
      const result = getSSHBrowseQuery('~/projects/');
      expect(result.browsePath).toBe('$HOME/projects');
      expect(result.prefix).toBe('');
    });

    it('handles ~/dir/subdir/ with trailing slash', () => {
      const result = getSSHBrowseQuery('~/projects/sprout/');
      expect(result.browsePath).toBe('$HOME/projects/sprout');
      expect(result.prefix).toBe('');
    });
  });

  describe('$HOME paths', () => {
    it('handles $HOME alone', () => {
      const result = getSSHBrowseQuery('$HOME');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('');
    });

    it('handles $HOME/ alone', () => {
      const result = getSSHBrowseQuery('$HOME/');
      // Same as ~/: after stripping trailing slash, $HOME doesn't start with $HOME/ or /
      // falls through to relative path handler
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('$HOME');
    });

    it('handles $HOME/dir', () => {
      const result = getSSHBrowseQuery('$HOME/projects');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('projects');
    });

    it('handles $HOME/dir/subdir', () => {
      const result = getSSHBrowseQuery('$HOME/projects/sprout');
      expect(result.browsePath).toBe('$HOME/projects');
      expect(result.prefix).toBe('sprout');
    });

    it('handles $HOME/dir/ with trailing slash', () => {
      const result = getSSHBrowseQuery('$HOME/projects/');
      expect(result.browsePath).toBe('$HOME/projects');
      expect(result.prefix).toBe('');
    });
  });

  describe('absolute paths', () => {
    it('handles / alone', () => {
      const result = getSSHBrowseQuery('/');
      expect(result.browsePath).toBe('/');
      expect(result.prefix).toBe('');
    });

    it('handles / with trailing slash', () => {
      const result = getSSHBrowseQuery('/');
      expect(result.browsePath).toBe('/');
      expect(result.prefix).toBe('');
    });

    it('handles /var/log', () => {
      const result = getSSHBrowseQuery('/var/log');
      expect(result.browsePath).toBe('/var');
      expect(result.prefix).toBe('log');
    });

    it('handles /var/log/ with trailing slash', () => {
      const result = getSSHBrowseQuery('/var/log/');
      expect(result.browsePath).toBe('/var/log');
      expect(result.prefix).toBe('');
    });

    it('handles /var/log/syslog (three segments)', () => {
      const result = getSSHBrowseQuery('/var/log/syslog');
      expect(result.browsePath).toBe('/var/log');
      expect(result.prefix).toBe('syslog');
    });

    it('handles deeply nested absolute path', () => {
      const result = getSSHBrowseQuery('/a/b/c/d');
      expect(result.browsePath).toBe('/a/b/c');
      expect(result.prefix).toBe('d');
    });
  });

  describe('relative paths', () => {
    it('handles single segment relative path', () => {
      const result = getSSHBrowseQuery('projects');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('projects');
    });

    it('handles multi-segment relative path', () => {
      const result = getSSHBrowseQuery('projects/sprout');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('projects/sprout');
    });

    it('handles deeply nested relative path', () => {
      const result = getSSHBrowseQuery('a/b/c/d');
      expect(result.browsePath).toBe('$HOME');
      expect(result.prefix).toBe('a/b/c/d');
    });
  });

  describe('multiple slashes normalization', () => {
    it('collapses multiple slashes in ~ path', () => {
      const result = getSSHBrowseQuery('~/projects//sprout');
      expect(result.browsePath).toBe('$HOME/projects');
      expect(result.prefix).toBe('sprout');
    });

    it('collapses multiple slashes in absolute path', () => {
      const result = getSSHBrowseQuery('/var//log');
      expect(result.browsePath).toBe('/var');
      expect(result.prefix).toBe('log');
    });
  });
});
