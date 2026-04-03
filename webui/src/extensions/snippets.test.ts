/**
 * snippets.test.ts — Unit tests for the snippets extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the three CM imports are mocked.  We test the exported pure functions
 * (`getSnippetsForLanguage`, `setSnippetLanguage`, `getSnippetLanguage`)
 * and the `tabExpandSnippets()` factory.
 */

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/view', () => ({
  EditorView: { baseTheme: jest.fn(() => 'mockBaseTheme') },
  keymap: {
    of: jest.fn((bindings) => ({ _keymapOf: bindings })),
  },
}));

jest.mock('@codemirror/state', () => ({
  Facet: {
    define: jest.fn(() => ({
      of: jest.fn((v: any) => ({ facetOf: v })),
    })),
  },
  Compartment: jest.fn(() => ({
    of: jest.fn((v: any) => v),
    reconfigure: jest.fn((v: any) => ({ reconfigure: v })),
  })),
}));

jest.mock('@codemirror/autocomplete', () => ({
  snippet: jest.fn((template) => () => template),
  hasNextSnippetField: jest.fn(() => false),
  hasPrevSnippetField: jest.fn(() => false),
}));

// Module under test — now safe to import because CM deps are mocked.
import {
  getSnippetsForLanguage,
  setSnippetLanguage,
  getSnippetLanguage,
  tabExpandSnippets,
} from './snippets';
import { keymap } from '@codemirror/view';

// ── getSnippetsForLanguage tests ────────────────────────────────────

describe('getSnippetsForLanguage', () => {
  // -------------------------------------------------------------------------
  // Null / undefined inputs
  // -------------------------------------------------------------------------

  it('returns an empty Map for null', () => {
    const result = getSnippetsForLanguage(null);
    expect(result).toBeInstanceOf(Map);
    expect(result.size).toBe(0);
  });

  it('returns an empty Map for an unrecognized language', () => {
    const result = getSnippetsForLanguage('nonexistent');
    expect(result).toBeInstanceOf(Map);
    expect(result.size).toBe(0);
  });

  it('returns an empty Map for empty string (falsy)', () => {
    const result = getSnippetsForLanguage('');
    expect(result).toBeInstanceOf(Map);
    expect(result.size).toBe(0);
  });

  // -------------------------------------------------------------------------
  // Go snippets
  // -------------------------------------------------------------------------

  describe('Go', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('go');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "ifn" trigger', () => {
      expect(snippets.has('ifn')).toBe(true);
    });

    it('contains the "ife" trigger', () => {
      expect(snippets.has('ife')).toBe(true);
    });

    it('contains the "forr" trigger', () => {
      expect(snippets.has('forr')).toBe(true);
    });

    it('contains the "err" trigger', () => {
      expect(snippets.has('err')).toBe(true);
    });

    it('contains the "go" trigger', () => {
      expect(snippets.has('go')).toBe(true);
    });

    it('"for" template contains Go-specific syntax', () => {
      const template = snippets.get('for')!;
      expect(template).toContain(':=');
      expect(template).toContain('++');
      expect(template).toContain('${1:i}');
      expect(template).toContain('${2:n}');
    });

    it('"if" template contains braces', () => {
      const template = snippets.get('if')!;
      expect(template).toContain('{');
      expect(template).toContain('}');
    });

    it('"fn" template starts with "func"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^func /);
    });

    it('does NOT contain an "afn" trigger', () => {
      expect(snippets.has('afn')).toBe(false);
    });
  });

  // -------------------------------------------------------------------------
  // TypeScript snippets
  // -------------------------------------------------------------------------

  describe('TypeScript', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('typescript');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "afn" trigger', () => {
      expect(snippets.has('afn')).toBe(true);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "ifn" trigger', () => {
      expect(snippets.has('ifn')).toBe(true);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "forof" trigger', () => {
      expect(snippets.has('forof')).toBe(true);
    });

    it('contains the "log" trigger', () => {
      expect(snippets.has('log')).toBe(true);
    });

    it('contains the "im" trigger', () => {
      expect(snippets.has('im')).toBe(true);
    });

    it('contains the "int" trigger (TypeScript-specific)', () => {
      expect(snippets.has('int')).toBe(true);
    });

    it('contains the "tw" trigger (TypeScript-specific)', () => {
      expect(snippets.has('tw')).toBe(true);
    });

    it('"fn" template contains "function" keyword', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^function /);
    });

    it('"afn" template contains arrow syntax', () => {
      const template = snippets.get('afn')!;
      expect(template).toContain('=>');
    });

    it('"log" template contains console.log', () => {
      const template = snippets.get('log')!;
      expect(template).toContain('console.log');
    });
  });

  // -------------------------------------------------------------------------
  // TypeScript-JSX shares snippets with TypeScript
  // -------------------------------------------------------------------------

  it('typescript-jsx returns the same snippets as typescript', () => {
    const ts = getSnippetsForLanguage('typescript');
    const tsx = getSnippetsForLanguage('typescript-jsx');
    expect(tsx.size).toBe(ts.size);
    for (const [key, value] of ts) {
      expect(tsx.get(key)).toBe(value);
    }
  });

  // -------------------------------------------------------------------------
  // JavaScript snippets
  // -------------------------------------------------------------------------

  describe('JavaScript', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('javascript');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "afn" trigger', () => {
      expect(snippets.has('afn')).toBe(true);
    });

    it('does NOT contain the "int" trigger (TypeScript-only)', () => {
      expect(snippets.has('int')).toBe(false);
    });

    it('does NOT contain the "tw" trigger (TypeScript-only)', () => {
      expect(snippets.has('tw')).toBe(false);
    });

    it('"for" template uses "let" not "var"', () => {
      const template = snippets.get('for')!;
      expect(template).toContain('let ');
    });
  });

  // -------------------------------------------------------------------------
  // JavaScript-JSX shares snippets with JavaScript
  // -------------------------------------------------------------------------

  it('javascript-jsx returns the same snippets as javascript', () => {
    const js = getSnippetsForLanguage('javascript');
    const jsx = getSnippetsForLanguage('javascript-jsx');
    expect(jsx.size).toBe(js.size);
    for (const [key, value] of js) {
      expect(jsx.get(key)).toBe(value);
    }
  });

  // -------------------------------------------------------------------------
  // Python snippets
  // -------------------------------------------------------------------------

  describe('Python', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('python');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "class" trigger', () => {
      expect(snippets.has('class')).toBe(true);
    });

    it('contains the "main" trigger', () => {
      expect(snippets.has('main')).toBe(true);
    });

    it('contains the "pr" trigger', () => {
      expect(snippets.has('pr')).toBe(true);
    });

    it('contains the "imp" trigger', () => {
      expect(snippets.has('imp')).toBe(true);
    });

    it('contains the "fr" trigger', () => {
      expect(snippets.has('fr')).toBe(true);
    });

    it('contains the "init" trigger', () => {
      expect(snippets.has('init')).toBe(true);
    });

    it('"fn" template starts with "def"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^def /);
    });

    it('"if" template contains a colon (Python syntax)', () => {
      const template = snippets.get('if')!;
      expect(template).toContain(':');
    });

    it('"main" template contains __name__', () => {
      const template = snippets.get('main')!;
      expect(template).toContain('__name__');
    });

    it('"main" template contains __main__', () => {
      const template = snippets.get('main')!;
      expect(template).toContain('__main__');
    });

    it('"init" template contains __init__', () => {
      const template = snippets.get('init')!;
      expect(template).toContain('__init__');
    });

    it('uses placeholders (${}) instead of language braces', () => {
      const template = snippets.get('if')!;
      // Python if templates use ${...} placeholders, not structural braces
      expect(template).toContain('${');
      // The template should NOT contain plain { not followed by $ (i.e. code braces)
      expect(template).not.toMatch(/[^$]\{/);
    });
  });

  // -------------------------------------------------------------------------
  // Rust snippets
  // -------------------------------------------------------------------------

  describe('Rust', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('rust');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "impl" trigger', () => {
      expect(snippets.has('impl')).toBe(true);
    });

    it('contains the "st" trigger', () => {
      expect(snippets.has('st')).toBe(true);
    });

    it('contains the "en" trigger', () => {
      expect(snippets.has('en')).toBe(true);
    });

    it('contains the "match" trigger', () => {
      expect(snippets.has('match')).toBe(true);
    });

    it('contains the "mac" trigger', () => {
      expect(snippets.has('mac')).toBe(true);
    });

    it('"fn" template starts with "fn"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^fn /);
    });

    it('"match" template contains match keyword and =>', () => {
      const template = snippets.get('match')!;
      expect(template).toMatch(/^match /);
      expect(template).toContain('=>');
    });

    it('"impl" template starts with "impl"', () => {
      const template = snippets.get('impl')!;
      expect(template).toMatch(/^impl /);
    });
  });

  // -------------------------------------------------------------------------
  // Java snippets
  // -------------------------------------------------------------------------

  describe('Java', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('java');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "fori" trigger (enhanced for)', () => {
      expect(snippets.has('fori')).toBe(true);
    });

    it('contains the "class" trigger', () => {
      expect(snippets.has('class')).toBe(true);
    });

    it('contains the "main" trigger', () => {
      expect(snippets.has('main')).toBe(true);
    });

    it('contains the "sysout" trigger', () => {
      expect(snippets.has('sysout')).toBe(true);
    });

    it('contains the "syso" trigger', () => {
      expect(snippets.has('syso')).toBe(true);
    });

    it('"main" template contains "public static void main"', () => {
      const template = snippets.get('main')!;
      expect(template).toContain('public static void main');
      expect(template).toContain('String[] args');
    });

    it('"class" template contains "public class"', () => {
      const template = snippets.get('class')!;
      expect(template).toContain('public class');
    });
  });

  // -------------------------------------------------------------------------
  // C snippets
  // -------------------------------------------------------------------------

  describe('C', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('c');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "inc" trigger', () => {
      expect(snippets.has('inc')).toBe(true);
    });

    it('contains the "main" trigger', () => {
      expect(snippets.has('main')).toBe(true);
    });

    it('"inc" template contains "#include"', () => {
      const template = snippets.get('inc')!;
      expect(template).toContain('#include');
    });

    it('"main" template contains "return 0"', () => {
      const template = snippets.get('main')!;
      expect(template).toContain('return 0');
    });
  });

  // -------------------------------------------------------------------------
  // C++ snippets (superset of C)
  // -------------------------------------------------------------------------

  describe('C++', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('cpp');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains all C snippets plus more', () => {
      const cSnippets = getSnippetsForLanguage('c');
      expect(snippets.size).toBeGreaterThan(cSnippets.size);
    });

    it('contains the "class" trigger', () => {
      expect(snippets.has('class')).toBe(true);
    });

    it('contains the "str" trigger', () => {
      expect(snippets.has('str')).toBe(true);
    });

    it('contains the "vec" trigger', () => {
      expect(snippets.has('vec')).toBe(true);
    });

    it('"class" template contains "public:"', () => {
      const template = snippets.get('class')!;
      expect(template).toContain('public:');
    });

    it('"vec" template contains "std::vector"', () => {
      const template = snippets.get('vec')!;
      expect(template).toContain('std::vector');
    });
  });

  // -------------------------------------------------------------------------
  // PHP snippets
  // -------------------------------------------------------------------------

  describe('PHP', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('php');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "ec" trigger', () => {
      expect(snippets.has('ec')).toBe(true);
    });

    it('"fn" template starts with "function"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^function /);
    });

    it('"ec" template contains "echo"', () => {
      const template = snippets.get('ec')!;
      expect(template).toContain('echo');
    });
  });

  // -------------------------------------------------------------------------
  // Ruby snippets
  // -------------------------------------------------------------------------

  describe('Ruby', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('ruby');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "each" trigger', () => {
      expect(snippets.has('each')).toBe(true);
    });

    it('contains the "puts" trigger', () => {
      expect(snippets.has('puts')).toBe(true);
    });

    it('contains the "req" trigger', () => {
      expect(snippets.has('req')).toBe(true);
    });

    it('contains the "mod" trigger', () => {
      expect(snippets.has('mod')).toBe(true);
    });

    it('"fn" template starts with "def" and ends with "end"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^def /);
      expect(template.trimEnd()).toMatch(/end$/);
    });

    it('"each" template contains ".each do"', () => {
      const template = snippets.get('each')!;
      expect(template).toContain('.each do');
    });

    it('"ifn" template contains "else"', () => {
      const template = snippets.get('ifn')!;
      expect(template).toContain('else');
    });
  });

  // -------------------------------------------------------------------------
  // Cross-language comparisons
  // -------------------------------------------------------------------------

  describe('Cross-language trigger differences', () => {
    it('"for" trigger exists in multiple languages with different templates', () => {
      const goFor = getSnippetsForLanguage('go').get('for');
      const tsFor = getSnippetsForLanguage('typescript').get('for');
      const pyFor = getSnippetsForLanguage('python').get('for');
      const rsFor = getSnippetsForLanguage('rust').get('for');

      // All should exist
      expect(goFor).toBeDefined();
      expect(tsFor).toBeDefined();
      expect(pyFor).toBeDefined();
      expect(rsFor).toBeDefined();

      // Go uses :=
      expect(goFor).toContain(':=');
      // TypeScript uses let
      expect(tsFor).toContain('let ');
      // Python uses "in" with colon
      expect(pyFor).toContain(' in ');
      expect(pyFor).toContain(':');
      // Rust uses "in" with braces
      expect(rsFor).toContain(' in ');
      expect(rsFor).toContain('{');
    });

    it('"if" trigger exists in all supported languages', () => {
      const languages = ['go', 'typescript', 'javascript', 'python', 'rust', 'java', 'c', 'cpp', 'php', 'ruby'];
      for (const lang of languages) {
        const value = getSnippetsForLanguage(lang).has('if');
        expect(value).toBe(true); // `if` should exist in ${lang}
      }
    });

    it('"fn" trigger exists in Go, TS, JS, Python, Rust, PHP, Ruby but NOT Java', () => {
      expect(getSnippetsForLanguage('go').has('fn')).toBe(true);
      expect(getSnippetsForLanguage('typescript').has('fn')).toBe(true);
      expect(getSnippetsForLanguage('javascript').has('fn')).toBe(true);
      expect(getSnippetsForLanguage('python').has('fn')).toBe(true);
      expect(getSnippetsForLanguage('rust').has('fn')).toBe(true);
      expect(getSnippetsForLanguage('php').has('fn')).toBe(true);
      expect(getSnippetsForLanguage('ruby').has('fn')).toBe(true);
      expect(getSnippetsForLanguage('java').has('fn')).toBe(false);
    });
  });

  // -------------------------------------------------------------------------
  // Caching behavior
  // -------------------------------------------------------------------------

  describe('Caching', () => {
    it('returns the same Map instance for repeated calls', () => {
      const first = getSnippetsForLanguage('go');
      const second = getSnippetsForLanguage('go');
      expect(first).toBe(second); // Same reference
    });
  });

  // -------------------------------------------------------------------------
  // Template content validation
  // -------------------------------------------------------------------------

  describe('Template content', () => {
    it('all templates contain at least one placeholder (${...})', () => {
      const languagesWithSnippets = ['go', 'typescript', 'javascript', 'python', 'rust', 'java', 'c', 'cpp', 'php', 'ruby'];
      for (const lang of languagesWithSnippets) {
        const snippets = getSnippetsForLanguage(lang);
        for (const [trigger, template] of snippets) {
          expect(template).toMatch(/\$\{[^}]+\}/); // ${lang}: "${trigger}" has no placeholders
        }
      }
    });

    it('Go "for" template contains newline and tab characters', () => {
      const template = getSnippetsForLanguage('go').get('for')!;
      // Template literals with \n and \t become actual newline/tab characters
      expect(template).toContain('\n');
      expect(template).toContain('\t');
    });
  });
});

// ── setSnippetLanguage / getSnippetLanguage tests ───────────────────

describe('setSnippetLanguage / getSnippetLanguage', () => {
  afterEach(() => {
    // Reset to null between tests — pass null as view since it's only
    // used for the compartment dispatch which is mocked in tests
    setSnippetLanguage(null as any, null);
  });

  it('initial language is null', () => {
    expect(getSnippetLanguage()).toBeNull();
  });

  it('setting to "go" updates the language', () => {
    setSnippetLanguage(null as any, 'go');
    expect(getSnippetLanguage()).toBe('go');
  });

  it('setting to "python" updates the language', () => {
    setSnippetLanguage(null as any, 'python');
    expect(getSnippetLanguage()).toBe('python');
  });

  it('setting back to null resets the language', () => {
    setSnippetLanguage(null as any, 'go');
    expect(getSnippetLanguage()).toBe('go');
    setSnippetLanguage(null as any, null);
    expect(getSnippetLanguage()).toBeNull();
  });

  it('overwriting with a different language works', () => {
    setSnippetLanguage(null as any, 'javascript');
    expect(getSnippetLanguage()).toBe('javascript');
    setSnippetLanguage(null as any, 'rust');
    expect(getSnippetLanguage()).toBe('rust');
  });
});

// ── tabExpandSnippets tests ─────────────────────────────────────────

describe('tabExpandSnippets', () => {
  it('returns an array (extension bundle)', () => {
    const ext = tabExpandSnippets();
    expect(Array.isArray(ext)).toBe(true);
    expect(ext.length).toBeGreaterThanOrEqual(1);
  });

  it('keymap.of was called during construction', () => {
    (keymap.of as jest.Mock).mockClear();
    tabExpandSnippets();
    expect(keymap.of).toHaveBeenCalled();
    expect((keymap.of as jest.Mock).mock.calls[0][0][0].key).toBe('Tab');
    expect(typeof (keymap.of as jest.Mock).mock.calls[0][0][0].run).toBe('function');
  });
});
