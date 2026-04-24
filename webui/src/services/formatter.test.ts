/**
 * formatter.test.ts — Unit tests for the formatter service.
 *
 * Prettier 3.x is ESM-only and triggers ERR_VM_DYNAMIC_IMPORT_CALLBACK_MISSING_FLAG
 * inside react-scripts (Jest). We mock Prettier to test all the surrounding logic
 * deterministically, which covers extension detection, size limits, error fallback, etc.
 */

jest.mock('prettier', () => ({
  format: jest.fn(),
  __esModule: true,
}));

import { formatCode, formatCodeWithConfigDiscovery, isFormattable, setConfigFetcher, clearConfigCache } from './formatter';

// Get a reference to the mocked format function
const mockedFormat = jest.requireMock('prettier').format as jest.Mock;

beforeEach(() => {
  mockedFormat.mockReset();
  // Default: Prettier succeeds, returns input unchanged
  mockedFormat.mockImplementation(async (input: string) => input);
});

// ---------------------------------------------------------------------------
// isFormattable
// ---------------------------------------------------------------------------

describe('isFormattable', () => {
  const supported = [
    '.js', '.jsx', '.mjs', '.cjs',
    '.ts', '.tsx',
    '.css', '.scss', '.less',
    '.html', '.htm', '.vue',
    '.json', '.json5', '.jsonc',
    '.md', '.markdown', '.mdx',
    '.yaml', '.yml',
    '.graphql', '.gql',
  ];

  for (const ext of supported) {
    it(`returns true for ${ext}`, () => {
      expect(isFormattable(`file${ext}`)).toBe(true);
    });
  }

  const unsupported = ['.go', '.py', '.rs', '.txt', '.xml', '.toml', '.sh', '.java'];

  for (const ext of unsupported) {
    it(`returns false for ${ext}`, () => {
      expect(isFormattable(`file${ext}`)).toBe(false);
    });
  }

  // Edge cases
  it('returns false for no extension', () => expect(isFormattable('Makefile')).toBe(false));
  it('returns false for empty string', () => expect(isFormattable('')).toBe(false));
  it('returns false for file ending with dot', () => expect(isFormattable('file.')).toBe(false));
  it('returns false for hidden files (no supported ext)', () => expect(isFormattable('.gitignore')).toBe(false));
  it('returns true for hidden file with supported ext', () => expect(isFormattable('.editorconfig.ts')).toBe(true));

  // Case insensitivity
  it('handles uppercase extension .JS', () => expect(isFormattable('file.JS')).toBe(true));
  it('handles uppercase extension .TS', () => expect(isFormattable('file.TS')).toBe(true));
  it('handles mixed-case extension .Tsx', () => expect(isFormattable('file.Tsx')).toBe(true));

  // Paths with directories
  it('handles absolute path', () => expect(isFormattable('/path/to/file.ts')).toBe(true));
  it('handles relative path', () => expect(isFormattable('src/components/App.tsx')).toBe(true));
  it('handles dot-relative path', () => expect(isFormattable('./src/utils/helper.js')).toBe(true));
});

// ---------------------------------------------------------------------------
// formatCode — unsupported file types (never calls Prettier)
// ---------------------------------------------------------------------------

describe('formatCode — unsupported files', () => {
  it('returns original for .go files without calling Prettier', async () => {
    const input = 'package main\nfunc main() {}';
    const r = await formatCode(input, 'main.go');
    expect(r.formatted).toBe(input);
    expect(r.error).toBeUndefined();
    expect(mockedFormat).not.toHaveBeenCalled();
  });

  it('returns original for .py files', async () => {
    const input = 'print("hello")';
    const r = await formatCode(input, 'script.py');
    expect(r.formatted).toBe(input);
    expect(r.error).toBeUndefined();
    expect(mockedFormat).not.toHaveBeenCalled();
  });

  it('returns original for no extension', async () => {
    const input = 'some content';
    const r = await formatCode(input, 'Makefile');
    expect(r.formatted).toBe(input);
    expect(r.error).toBeUndefined();
    expect(mockedFormat).not.toHaveBeenCalled();
  });

  it('returns original for empty extension', async () => {
    const r = await formatCode('code', '');
    expect(r.formatted).toBe('code');
    expect(r.error).toBeUndefined();
    expect(mockedFormat).not.toHaveBeenCalled();
  });

  it('returns original for file ending with dot', async () => {
    const r = await formatCode('code', 'file.');
    expect(r.formatted).toBe('code');
    expect(r.error).toBeUndefined();
    expect(mockedFormat).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// formatCode — file size limits
// ---------------------------------------------------------------------------

describe('formatCode — file size limits', () => {
  const ONE_MB = 1024 * 1024;

  it('returns original + error for file > 1 MB', async () => {
    const r = await formatCode('const x=1;', 'big.js', ONE_MB + 1);
    expect(r.formatted).toBe('const x=1;');
    expect(r.error).toBe('File too large to format');
    expect(mockedFormat).not.toHaveBeenCalled();
  });

  it('calls Prettier for file exactly at 1 MB', async () => {
    const r = await formatCode('const x=1;', 'exact.js', ONE_MB);
    expect(r.error).toBeUndefined();
    expect(mockedFormat).toHaveBeenCalledTimes(1);
  });

  it('calls Prettier for file below 1 MB', async () => {
    await formatCode('const x=1;', 'small.js', 100);
    expect(mockedFormat).toHaveBeenCalledTimes(1);
  });

  it('calls Prettier when fileSize is undefined', async () => {
    await formatCode('const x=1;', 'file.js', undefined);
    expect(mockedFormat).toHaveBeenCalledTimes(1);
  });

  it('calls Prettier when fileSize is not provided', async () => {
    await formatCode('const x=1;', 'file.js');
    expect(mockedFormat).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// formatCode — passes correct options to Prettier
// ---------------------------------------------------------------------------

describe('formatCode — Prettier options', () => {
  it('calls Prettier with babel parser for .js files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'app.js');
    expect(mockedFormat).toHaveBeenCalledWith('input', {
      parser: 'babel',
      semi: true,
      singleQuote: true,
      tabWidth: 2,
      trailingComma: 'all',
      printWidth: 80,
    });
  });

  it('calls Prettier with typescript parser for .ts files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'app.ts');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'typescript',
    }));
  });

  it('calls Prettier with typescript parser for .tsx files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'App.tsx');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'typescript',
    }));
  });

  it('calls Prettier with json parser for .json files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'data.json');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'json',
    }));
  });

  it('calls Prettier with css parser for .css files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'style.css');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'css',
    }));
  });

  it('calls Prettier with html parser for .html files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'index.html');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'html',
    }));
  });

  it('calls Prettier with markdown parser for .md files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'readme.md');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'markdown',
    }));
  });

  it('calls Prettier with yaml parser for .yaml files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'config.yaml');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'yaml',
    }));
  });

  it('calls Prettier with graphql parser for .graphql files', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'query.graphql');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'graphql',
    }));
  });

  it('always includes standard formatting options', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'file.ts');
    expect(mockedFormat).toHaveBeenCalledWith('input', {
      parser: 'typescript',
      semi: true,
      singleQuote: true,
      tabWidth: 2,
      trailingComma: 'all',
      printWidth: 80,
    });
  });
});

// ---------------------------------------------------------------------------
// formatCode — Prettier result handling
// ---------------------------------------------------------------------------

describe('formatCode — result handling', () => {
  it('returns Prettier output on success', async () => {
    mockedFormat.mockResolvedValue('const x = 1;');
    const r = await formatCode('const x=1;', 'app.js');
    expect(r.formatted).toBe('const x = 1;');
    expect(r.error).toBeUndefined();
  });

  it('returns original content + error when Prettier throws', async () => {
    mockedFormat.mockRejectedValue(new Error('SyntaxError: Unexpected token'));
    const r = await formatCode('const x = ;', 'app.js');
    expect(r.formatted).toBe('const x = ;');
    expect(r.error).toBe('SyntaxError: Unexpected token');
  });

  it('returns original content when Prettier throws non-Error', async () => {
    mockedFormat.mockRejectedValue('string error');
    const r = await formatCode('input', 'app.js');
    expect(r.formatted).toBe('input');
    expect(r.error).toBe('string error');
  });

  it('returns original content when Prettier throws undefined', async () => {
    mockedFormat.mockRejectedValue(undefined);
    const r = await formatCode('input', 'app.js');
    expect(r.formatted).toBe('input');
    expect(r.error).toBe('undefined');
  });

  it('returns original content when Prettier returns null', async () => {
    mockedFormat.mockResolvedValue(null as unknown as string);
    const r = await formatCode('input', 'app.js');
    expect(r.formatted).toBe('input');
    expect(r.error).toBeUndefined();
  });

  it('returns original content when Prettier returns undefined', async () => {
    mockedFormat.mockResolvedValue(undefined as unknown as string);
    const r = await formatCode('input', 'app.js');
    expect(r.formatted).toBe('input');
    expect(r.error).toBeUndefined();
  });

  it('returns empty string when Prettier returns empty string', async () => {
    mockedFormat.mockResolvedValue('');
    const r = await formatCode('const x=1;', 'app.js');
    expect(r.formatted).toBe('');
    expect(r.error).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// formatCode — edge cases
// ---------------------------------------------------------------------------

describe('formatCode — edge cases', () => {
  it('handles empty content', async () => {
    mockedFormat.mockResolvedValue('');
    const r = await formatCode('', 'app.js');
    expect(r.formatted).toBe('');
    expect(r.error).toBeUndefined();
  });

  it('handles case-insensitive extension', async () => {
    mockedFormat.mockResolvedValue('formatted');
    const r = await formatCode('const x=1', 'app.JS');
    expect(r.error).toBeUndefined();
    expect(mockedFormat).toHaveBeenCalledWith('const x=1', expect.objectContaining({
      parser: 'babel',
    }));
  });

  it('handles uppercase .TS extension', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('const x:number=1', 'app.TS');
    expect(mockedFormat).toHaveBeenCalledWith('const x:number=1', expect.objectContaining({
      parser: 'typescript',
    }));
  });

  it('handles .Tsx extension (case-insensitive)', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'file.Tsx');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'typescript',
    }));
  });

  it('handles file path with directories — correct extension extraction', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', '/home/user/project/src/App.tsx');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'typescript',
    }));
  });

  it('handles nested relative path', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', './src/components/utils/helper.ts');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'typescript',
    }));
  });

  it('is idempotent (formatting formatted code returns same result)', async () => {
    mockedFormat.mockResolvedValueOnce('const x = 1;');
    mockedFormat.mockResolvedValueOnce('const x = 1;');
    const r1 = await formatCode('const x=1', 'app.js');
    const r2 = await formatCode(r1.formatted, 'app.js');
    expect(r1.formatted).toBe(r2.formatted);
  });

  it('FormatResult always has formatted property as string', async () => {
    mockedFormat.mockResolvedValue('result');
    const r = await formatCode('input', 'app.js');
    expect(typeof r.formatted).toBe('string');
    expect(typeof r.error).toBe('undefined');
  });
});

// ---------------------------------------------------------------------------
// formatCode — config merging
// ---------------------------------------------------------------------------

describe('formatCode — config merging', () => {
  beforeEach(() => {
    clearConfigCache();
  });

  it('uses default options when no config provided', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'app.js');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'babel',
      semi: true,
      singleQuote: true,
      tabWidth: 2,
      trailingComma: 'all',
      printWidth: 80,
    }));
  });

  it('merges user config with defaults (user config takes precedence)', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'app.js', undefined, { singleQuote: false, printWidth: 100 });
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'babel',
      semi: true, // default
      singleQuote: false, // from user config
      tabWidth: 2, // default
      trailingComma: 'all', // default
      printWidth: 100, // from user config
    }));
  });

  it('allows full override of all default options', async () => {
    mockedFormat.mockResolvedValue('formatted');
    await formatCode('input', 'app.js', undefined, {
      semi: false,
      singleQuote: false,
      tabWidth: 4,
      trailingComma: 'none',
      printWidth: 120,
    });
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'babel',
      semi: false,
      singleQuote: false,
      tabWidth: 4,
      trailingComma: 'none',
      printWidth: 120,
    }));
  });
});

// ---------------------------------------------------------------------------
// formatCodeWithConfigDiscovery — config fetcher integration
// ---------------------------------------------------------------------------

describe('formatCodeWithConfigDiscovery — config discovery', () => {
  beforeEach(() => {
    clearConfigCache();
  });

  it('fetches config when config fetcher is set', async () => {
    const mockFetcher = jest.fn().mockResolvedValue({ singleQuote: false, tabWidth: 4 });
    setConfigFetcher(mockFetcher);

    mockedFormat.mockResolvedValue('formatted');
    await formatCodeWithConfigDiscovery('input', 'src/app.js');

    expect(mockFetcher).toHaveBeenCalledWith('src/app.js');
    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'babel',
      singleQuote: false,
      tabWidth: 4,
    }));
  });

  it('uses empty config when fetcher returns empty', async () => {
    setConfigFetcher(jest.fn().mockResolvedValue({}));

    mockedFormat.mockResolvedValue('formatted');
    await formatCodeWithConfigDiscovery('input', 'app.js');

    expect(mockedFormat).toHaveBeenCalledWith('input', expect.objectContaining({
      parser: 'babel',
      semi: true,
      singleQuote: true,
      tabWidth: 2,
    }));
  });

  it('caches config per directory', async () => {
    const mockFetcher = jest.fn().mockResolvedValue({ singleQuote: false });
    setConfigFetcher(mockFetcher);

    mockedFormat.mockResolvedValue('formatted');

    // Format multiple files in same directory
    await formatCodeWithConfigDiscovery('input1', 'src/components/a.js');
    await formatCodeWithConfigDiscovery('input2', 'src/components/b.ts');

    // Should only fetch config once (for the first file)
    expect(mockFetcher).toHaveBeenCalledTimes(1);
  });

  it('fetches new config for different directory', async () => {
    const mockFetcher = jest.fn().mockResolvedValue({ singleQuote: false });
    setConfigFetcher(mockFetcher);

    mockedFormat.mockResolvedValue('formatted');

    // Format files in different directories
    await formatCodeWithConfigDiscovery('input1', 'src/components/a.js');
    await formatCodeWithConfigDiscovery('input2', 'lib/utils/b.ts');

    // Should fetch config twice (once per directory)
    expect(mockFetcher).toHaveBeenCalledTimes(2);
  });

  it('fails gracefully when config fetcher throws', async () => {
    setConfigFetcher(jest.fn().mockRejectedValue(new Error('network error')));

    mockedFormat.mockResolvedValue('formatted');
    const result = await formatCodeWithConfigDiscovery('input', 'app.js');

    // Should fall back to defaults when fetcher throws
    expect(result.formatted).toBe('formatted');
    expect(mockedFormat).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// clearConfigCache
// ---------------------------------------------------------------------------

describe('clearConfigCache', () => {
  it('clears cached config', async () => {
    const mockFetcher = jest.fn().mockResolvedValue({ singleQuote: false });
    setConfigFetcher(mockFetcher);

    mockedFormat.mockResolvedValue('formatted');

    // First format - populates cache
    await formatCodeWithConfigDiscovery('input1', 'src/a.js');
    expect(mockFetcher).toHaveBeenCalledTimes(1);

    // Clear cache
    clearConfigCache();

    // Second format - should fetch again
    await formatCodeWithConfigDiscovery('input2', 'src/b.js');
    expect(mockFetcher).toHaveBeenCalledTimes(2);
  });
});
