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

    it('does NOT contain a "do" trigger (not standalone in Ruby)', () => {
      expect(snippets.has('do')).toBe(false);
    });

    it('"ifn" template uses unique placeholder numbers (no duplicate ${0})', () => {
      const template = snippets.get('ifn')!;
      expect(template).toContain('${2}');
      expect(template).toContain('${3}');
      // Should not have two ${0} placeholders
      const zeroCount = (template.match(/\$\{0\}/g) || []).length;
      expect(zeroCount).toBeLessThanOrEqual(1);
    });
  });

  // -------------------------------------------------------------------------
  // Shell snippets
  // -------------------------------------------------------------------------

  describe('Shell', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('shell');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "func" trigger', () => {
      expect(snippets.has('func')).toBe(true);
    });

    it('contains the "shebang" trigger', () => {
      expect(snippets.has('shebang')).toBe(true);
    });

    it('contains the "case" trigger', () => {
      expect(snippets.has('case')).toBe(true);
    });

    it('contains the "wh" trigger', () => {
      expect(snippets.has('wh')).toBe(true);
    });

    it('"if" template contains "if" and "then" and "fi"', () => {
      const template = snippets.get('if')!;
      expect(template).toContain('if');
      expect(template).toContain('then');
      expect(template).toContain('fi');
    });

    it('"shebang" template contains "#!/bin/bash"', () => {
      const template = snippets.get('shebang')!;
      expect(template).toContain('#!/bin/bash');
    });

    it('"for" template contains "do" and "done"', () => {
      const template = snippets.get('for')!;
      expect(template).toContain('do');
      expect(template).toContain('done');
    });

    it('"case" template contains "esac"', () => {
      const template = snippets.get('case')!;
      expect(template).toContain('esac');
    });
  });

  // -------------------------------------------------------------------------
  // HTML snippets
  // -------------------------------------------------------------------------

  describe('HTML', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('html');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "div" trigger', () => {
      expect(snippets.has('div')).toBe(true);
    });

    it('contains the "span" trigger', () => {
      expect(snippets.has('span')).toBe(true);
    });

    it('contains the "html" trigger', () => {
      expect(snippets.has('html')).toBe(true);
    });

    it('contains the "input" trigger', () => {
      expect(snippets.has('input')).toBe(true);
    });

    it('contains the "form" trigger', () => {
      expect(snippets.has('form')).toBe(true);
    });

    it('"div" template contains "<div>" tags', () => {
      const template = snippets.get('div')!;
      expect(template).toContain('<div>');
      expect(template).toContain('</div>');
    });

    it('"html" template contains "<!DOCTYPE html>"', () => {
      const template = snippets.get('html')!;
      expect(template).toContain('<!DOCTYPE html>');
      expect(template).toContain('<html');
    });

    it('"html" template contains <head> and <body>', () => {
      const template = snippets.get('html')!;
      expect(template).toContain('<head>');
      expect(template).toContain('<body>');
    });

    it('"a" template contains href attribute', () => {
      const template = snippets.get('a')!;
      expect(template).toContain('href=');
      expect(template).toContain('<a');
      expect(template).toContain('</a>');
    });

    it('"img" template contains src and alt attributes', () => {
      const template = snippets.get('img')!;
      expect(template).toContain('src=');
      expect(template).toContain('alt=');
    });
  });

  // -------------------------------------------------------------------------
  // SQL snippets
  // -------------------------------------------------------------------------

  describe('SQL', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('sql');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "sel" trigger', () => {
      expect(snippets.has('sel')).toBe(true);
    });

    it('contains the "selw" trigger', () => {
      expect(snippets.has('selw')).toBe(true);
    });

    it('contains the "ins" trigger', () => {
      expect(snippets.has('ins')).toBe(true);
    });

    it('contains the "upd" trigger', () => {
      expect(snippets.has('upd')).toBe(true);
    });

    it('contains the "del" trigger', () => {
      expect(snippets.has('del')).toBe(true);
    });

    it('contains the "ct" trigger', () => {
      expect(snippets.has('ct')).toBe(true);
    });

    it('contains the "join" trigger', () => {
      expect(snippets.has('join')).toBe(true);
    });

    it('"sel" template contains "SELECT"', () => {
      const template = snippets.get('sel')!;
      expect(template).toContain('SELECT');
      expect(template).toContain('FROM');
    });

    it('"ins" template contains "INSERT INTO"', () => {
      const template = snippets.get('ins')!;
      expect(template).toContain('INSERT INTO');
      expect(template).toContain('VALUES');
    });

    it('"del" template contains "DELETE FROM"', () => {
      const template = snippets.get('del')!;
      expect(template).toContain('DELETE FROM');
    });

    it('"ct" template contains "CREATE TABLE"', () => {
      const template = snippets.get('ct')!;
      expect(template).toContain('CREATE TABLE');
    });

    it('"join" template contains "INNER JOIN" and "ON"', () => {
      const template = snippets.get('join')!;
      expect(template).toContain('INNER JOIN');
      expect(template).toContain('ON');
    });
  });

  // -------------------------------------------------------------------------
  // YAML snippets
  // -------------------------------------------------------------------------

  describe('YAML', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('yaml');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "key" trigger', () => {
      expect(snippets.has('key')).toBe(true);
    });

    it('contains the "list" trigger', () => {
      expect(snippets.has('list')).toBe(true);
    });

    it('"key" template contains a colon (YAML key-value separator)', () => {
      const template = snippets.get('key')!;
      expect(template).toContain(':');
    });

    it('"list" template contains "- " (YAML list item)', () => {
      const template = snippets.get('list')!;
      expect(template).toContain('- ');
    });
  });

  // -------------------------------------------------------------------------
  // Markdown snippets
  // -------------------------------------------------------------------------

  describe('Markdown', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('markdown');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "code" trigger', () => {
      expect(snippets.has('code')).toBe(true);
    });

    it('contains the "link" trigger', () => {
      expect(snippets.has('link')).toBe(true);
    });

    it('contains the "img" trigger', () => {
      expect(snippets.has('img')).toBe(true);
    });

    it('contains the "bold" trigger', () => {
      expect(snippets.has('bold')).toBe(true);
    });

    it('contains the "italic" trigger', () => {
      expect(snippets.has('italic')).toBe(true);
    });

    it('contains the "tbl" trigger', () => {
      expect(snippets.has('tbl')).toBe(true);
    });

    it('"code" template contains triple backticks', () => {
      const template = snippets.get('code')!;
      expect(template).toContain('```');
    });

    it('"link" template contains markdown link syntax []()', () => {
      const template = snippets.get('link')!;
      expect(template).toContain('[');
      expect(template).toContain('](');
      expect(template).toContain(')');
    });

    it('"img" template contains markdown image syntax ![]()', () => {
      const template = snippets.get('img')!;
      expect(template).toContain('![');
      expect(template).toContain('](');
    });

    it('"bold" template contains double asterisks', () => {
      const template = snippets.get('bold')!;
      expect(template).toContain('**');
    });

    it('"tbl" template contains pipe characters for table columns', () => {
      const template = snippets.get('tbl')!;
      expect(template).toContain('|');
    });
  });

  // -------------------------------------------------------------------------
  // Swift snippets
  // -------------------------------------------------------------------------

  describe('Swift', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('swift');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "guard" trigger', () => {
      expect(snippets.has('guard')).toBe(true);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "sw" trigger', () => {
      expect(snippets.has('sw')).toBe(true);
    });

    it('contains the "cls" trigger', () => {
      expect(snippets.has('cls')).toBe(true);
    });

    it('"fn" template starts with "func"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^func /);
    });

    it('"guard" template contains "guard" and "else"', () => {
      const template = snippets.get('guard')!;
      expect(template).toContain('guard');
      expect(template).toContain('else');
    });

    it('"guard" template contains "return"', () => {
      const template = snippets.get('guard')!;
      expect(template).toContain('return');
    });
  });

  // -------------------------------------------------------------------------
  // Kotlin snippets
  // -------------------------------------------------------------------------

  describe('Kotlin', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('kotlin');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "when" trigger', () => {
      expect(snippets.has('when')).toBe(true);
    });

    it('contains the "cls" trigger', () => {
      expect(snippets.has('cls')).toBe(true);
    });

    it('contains the "obj" trigger', () => {
      expect(snippets.has('obj')).toBe(true);
    });

    it('"fn" template starts with "fun"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^fun /);
    });

    it('"when" template contains "when" and "->"', () => {
      const template = snippets.get('when')!;
      expect(template).toContain('when');
      expect(template).toContain('->');
    });

    it('"obj" template starts with "object"', () => {
      const template = snippets.get('obj')!;
      expect(template).toMatch(/^object /);
    });
  });

  // -------------------------------------------------------------------------
  // Dart snippets
  // -------------------------------------------------------------------------

  describe('Dart', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('dart');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "cls" trigger', () => {
      expect(snippets.has('cls')).toBe(true);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "main" trigger', () => {
      expect(snippets.has('main')).toBe(true);
    });

    it('"main" template contains "void main()"', () => {
      const template = snippets.get('main')!;
      expect(template).toContain('void main()');
    });

    it('"for" template contains "in" keyword', () => {
      const template = snippets.get('for')!;
      expect(template).toContain(' in ');
    });
  });

  // -------------------------------------------------------------------------
  // Scala snippets
  // -------------------------------------------------------------------------

  describe('Scala', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('scala');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "cls" trigger', () => {
      expect(snippets.has('cls')).toBe(true);
    });

    it('contains the "obj" trigger', () => {
      expect(snippets.has('obj')).toBe(true);
    });

    it('contains the "trt" trigger (trait)', () => {
      expect(snippets.has('trt')).toBe(true);
    });

    it('contains the "match" trigger', () => {
      expect(snippets.has('match')).toBe(true);
    });

    it('"fn" template starts with "def"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^def /);
    });

    it('"match" template contains "match" and "case"', () => {
      const template = snippets.get('match')!;
      expect(template).toContain(' match ');
      expect(template).toContain('case ');
    });

    it('"trt" template starts with "trait"', () => {
      const template = snippets.get('trt')!;
      expect(template).toMatch(/^trait /);
    });
  });

  // -------------------------------------------------------------------------
  // C# snippets
  // -------------------------------------------------------------------------

  describe('C#', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('csharp');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "fore" trigger (foreach)', () => {
      expect(snippets.has('fore')).toBe(true);
    });

    it('contains the "cw" trigger', () => {
      expect(snippets.has('cw')).toBe(true);
    });

    it('contains the "cls" trigger', () => {
      expect(snippets.has('cls')).toBe(true);
    });

    it('contains the "prop" trigger', () => {
      expect(snippets.has('prop')).toBe(true);
    });

    it('"cw" template contains "Console.WriteLine"', () => {
      const template = snippets.get('cw')!;
      expect(template).toContain('Console.WriteLine');
    });

    it('"fore" template contains "foreach" and "var"', () => {
      const template = snippets.get('fore')!;
      expect(template).toContain('foreach');
      expect(template).toContain('var');
    });

    it('"cls" template contains "public class"', () => {
      const template = snippets.get('cls')!;
      expect(template).toContain('public class');
    });

    it('"prop" template contains "get; set;"', () => {
      const template = snippets.get('prop')!;
      expect(template).toContain('get; set;');
    });
  });

  // -------------------------------------------------------------------------
  // Lua snippets
  // -------------------------------------------------------------------------

  describe('Lua', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('lua');
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

    it('contains the "wh" trigger', () => {
      expect(snippets.has('wh')).toBe(true);
    });

    it('contains the "pr" trigger', () => {
      expect(snippets.has('pr')).toBe(true);
    });

    it('"fn" template starts with "local function"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^local function /);
    });

    it('"fn" template ends with "end"', () => {
      const template = snippets.get('fn')!;
      expect(template.trimEnd()).toMatch(/end$/);
    });

    it('"if" template contains "then" and "end"', () => {
      const template = snippets.get('if')!;
      expect(template).toContain('then');
      expect(template).toContain('end');
    });
  });

  // -------------------------------------------------------------------------
  // Groovy snippets
  // -------------------------------------------------------------------------

  describe('Groovy', () => {
    let snippets: Map<string, string>;

    beforeAll(() => {
      snippets = getSnippetsForLanguage('groovy');
    });

    it('returns a non-empty Map', () => {
      expect(snippets.size).toBeGreaterThan(0);
    });

    it('contains the "fn" trigger', () => {
      expect(snippets.has('fn')).toBe(true);
    });

    it('contains the "cls" trigger', () => {
      expect(snippets.has('cls')).toBe(true);
    });

    it('contains the "if" trigger', () => {
      expect(snippets.has('if')).toBe(true);
    });

    it('contains the "for" trigger', () => {
      expect(snippets.has('for')).toBe(true);
    });

    it('contains the "each" trigger', () => {
      expect(snippets.has('each')).toBe(true);
    });

    it('contains the "println" trigger', () => {
      expect(snippets.has('println')).toBe(true);
    });

    it('"fn" template starts with "def"', () => {
      const template = snippets.get('fn')!;
      expect(template).toMatch(/^def /);
    });

    it('"each" template contains ".each" and "->"', () => {
      const template = snippets.get('each')!;
      expect(template).toContain('.each');
      expect(template).toContain('->');
    });

    it('"println" template contains "println"', () => {
      const template = snippets.get('println')!;
      expect(template).toContain('println ');
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
      const shFor = getSnippetsForLanguage('shell').get('for');

      // All should exist
      expect(goFor).toBeDefined();
      expect(tsFor).toBeDefined();
      expect(pyFor).toBeDefined();
      expect(rsFor).toBeDefined();
      expect(shFor).toBeDefined();

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
      // Shell uses "do" and "done"
      expect(shFor).toContain('do');
      expect(shFor).toContain('done');
    });

    it('"if" trigger exists in all supported languages', () => {
      const languages = ['go', 'typescript', 'javascript', 'python', 'rust', 'java', 'c', 'cpp', 'php', 'ruby', 'shell', 'swift', 'kotlin', 'dart', 'scala', 'csharp', 'lua', 'groovy'];
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

    it('all 24 registered language IDs return non-empty snippet maps', () => {
      const allLanguageIds = ['go', 'typescript', 'typescript-jsx', 'javascript', 'javascript-jsx', 'python', 'rust', 'java', 'c', 'cpp', 'php', 'ruby', 'shell', 'html', 'sql', 'yaml', 'markdown', 'swift', 'kotlin', 'dart', 'scala', 'csharp', 'lua', 'groovy'];
      for (const langId of allLanguageIds) {
        const snippets = getSnippetsForLanguage(langId);
        expect(snippets.size).toBeGreaterThan(0); // ${langId} should have registered snippets
      }
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
      const languagesWithSnippets = ['go', 'typescript', 'javascript', 'python', 'rust', 'java', 'c', 'cpp', 'php', 'ruby', 'shell', 'html', 'sql', 'yaml', 'markdown', 'swift', 'kotlin', 'dart', 'scala', 'csharp', 'lua', 'groovy'];
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

    it('no template has duplicate ${0} placeholders (exit point must be unique)', () => {
      const languagesWithSnippets = ['go', 'typescript', 'javascript', 'python', 'rust', 'java', 'c', 'cpp', 'php', 'ruby', 'shell', 'html', 'sql', 'yaml', 'markdown', 'swift', 'kotlin', 'dart', 'scala', 'csharp', 'lua', 'groovy'];
      for (const lang of languagesWithSnippets) {
        const snippets = getSnippetsForLanguage(lang);
        for (const [trigger, template] of snippets) {
          const zeroCount = (template.match(/\$\{0\}/g) || []).length;
          expect(zeroCount).toBeLessThanOrEqual(1);
          // ${lang}: "${trigger}" has ${zeroCount} ${0} placeholders
        }
      }
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
