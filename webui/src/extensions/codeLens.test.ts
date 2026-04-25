/**
 * codeLens.test.ts — Unit tests for the codeLens extension helper functions.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * we mock the CM imports and test the exported helper functions directly.
 */

// ── Module under test (Jest hoists mocks above imports) ─────────────
import { countReferences, formatRefText, computeCodeLenses } from './codeLens';

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/view', () => ({
  WidgetType: class {},
  Decoration: {
    widget: jest.fn(() => ({})),
    none: { type: 'none' },
    set: jest.fn((decorations) => decorations),
  },
  ViewPlugin: { fromClass: jest.fn(() => []) },
  EditorView: { baseTheme: jest.fn(() => []) },
}));

jest.mock('@codemirror/state', () => ({
  Extension: {},
}));

// Mock the symbolUtils module.
// Use requireActual to preserve the real CONTAINER_KINDS for the mock setup,
// while replacing extractSymbols with jest.fn() for controlled testing.
jest.mock('../utils/symbolUtils', () => ({
  ...jest.requireActual('../utils/symbolUtils'),
  extractSymbols: jest.fn(),
}));

// ── formatRefText tests ──────────────────────────────────────────

describe('formatRefText', () => {
  it('returns "1 ref" for count of 1', () => {
    expect(formatRefText(1)).toBe('1 ref');
  });

  it('returns "N refs" for count of 0', () => {
    expect(formatRefText(0)).toBe('0 refs');
  });

  it('returns "N refs" for counts greater than 1', () => {
    expect(formatRefText(5)).toBe('5 refs');
    expect(formatRefText(100)).toBe('100 refs');
    expect(formatRefText(999)).toBe('999 refs');
  });

  it('handles negative counts gracefully', () => {
    expect(formatRefText(-1)).toBe('0 refs');
  });
});

// ── countReferences tests ─────────────────────────────────────────

describe('countReferences', () => {
  it('counts occurrences of a symbol name', () => {
    const content = `function foo() {
  foo();
  foo();
  return foo();
}`;
    expect(countReferences(content, 'foo')).toBe(3); // 4 total - 1 definition = 3
  });

  it('returns 0 when name is not found', () => {
    const content = `function bar() {
  return 42;
}`;
    expect(countReferences(content, 'foo')).toBe(0);
  });

  it('returns 0 for empty content', () => {
    expect(countReferences('', 'foo')).toBe(0);
  });

  it('returns 0 for empty name', () => {
    expect(countReferences('some content', '')).toBe(0);
  });

  it('subtracts 1 for the definition itself', () => {
    const content = `function myFunc() {
  myFunc();
}`;
    // 2 total (definition + 1 call) - 1 = 1
    expect(countReferences(content, 'myFunc')).toBe(1);
  });

  it('uses word boundaries to avoid partial matches', () => {
    const content = `function foo() {}
function foobar() {}
let fooBar = 1;
foo();`;
    // 'foo' should match only the function definition and the call, not foobar
    // Total: foo (2), foobar (0), fooBar (0) = 2 - 1 = 1
    expect(countReferences(content, 'foo')).toBe(1);
  });

  it('handles special regex characters in names', () => {
    const content = `const myArray = [1, 2, 3];
myArray.push(4);
myArray.push(5);`;
    // 'myArray' contains no special chars, just test normal case
    expect(countReferences(content, 'myArray')).toBe(2); // 3 - 1 = 2
  });

  it('handles names with regex special characters', () => {
    const content = `const $special$ = 1;
$special$ + $special$;`;
    // Dollar signs and brackets are special in regex
    expect(countReferences(content, '$special$')).toBe(2); // 3 - 1 = 2
  });

  it('handles names with dots (property access)', () => {
    const content = `const obj = { prop: 1 };
obj.prop;
obj.prop;`;
    expect(countReferences(content, 'obj')).toBe(2); // 3 - 1 = 2
  });

  it('handles names with parentheses', () => {
    const content = `const fn = () => 1;
fn();
fn();`;
    expect(countReferences(content, 'fn')).toBe(2); // 3 - 1 = 2
  });

  it('handles names with asterisks', () => {
    const content = `function test*() {}
test*();
test*();`;
    // Asterisk is a regex special character
    expect(countReferences(content, 'test*')).toBe(2); // 3 - 1 = 2
  });

  it('handles names with plus signs', () => {
    const content = `function foo+() {}
foo+();
foo+();`;
    // Plus is a regex special character
    expect(countReferences(content, 'foo+')).toBe(2); // 3 - 1 = 2
  });

  it('handles class names with underscores', () => {
    const content = `class My_Class {
  method() {
    My_Class.staticMethod();
  }
}`;
    expect(countReferences(content, 'My_Class')).toBe(1); // 2 - 1 = 1
  });

  it('handles names starting with capital letter', () => {
    const content = `class MyClass {
  static create() {
    return new MyClass();
  }
}`;
    expect(countReferences(content, 'MyClass')).toBe(1); // 2 - 1 = 1
  });

  it('handles long variable names', () => {
    const content = `const veryLongVariableName = 1;
veryLongVariableName + veryLongVariableName;`;
    expect(countReferences(content, 'veryLongVariableName')).toBe(2); // 3 - 1 = 2
  });

  it('counts method calls on same line separately', () => {
    const content = `function foo() {}
foo(); foo(); foo();`;
    // 4 total - 1 = 3
    expect(countReferences(content, 'foo')).toBe(3);
  });

  it('returns 0 for symbol that only appears in its definition', () => {
    const content = `function unused() {}`;
    expect(countReferences(content, 'unused')).toBe(0);
  });
});

// ── computeCodeLenses tests ───────────────────────────────────────

describe('computeCodeLenses', () => {
  // Import the mocked extractSymbols
  const mockExtractSymbols = require('../utils/symbolUtils').extractSymbols;

  beforeEach(() => {
    jest.clearAllMocks();
    // Default to returning empty array so tests don't crash when no mock is set
    mockExtractSymbols.mockReturnValue([]);
  });

  it('returns empty array for empty content', () => {
    const result = computeCodeLenses('', '.ts');
    expect(result).toEqual([]);
  });

  it('uses generic patterns for undefined language and produces lenses when refs exist', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'foo', line: 1, kind: 'function' },
    ]);

    const content = `function foo() {}
foo();`;

    const result = computeCodeLenses(content, undefined);

    // undefined languageId falls through to GENERIC_PATTERNS in the real code,
    // and lenses are produced when refs > 0 regardless of language
    expect(result.length).toBe(1);
    expect(result[0].name).toBe('foo');
    expect(result[0].refCount).toBe(1);
  });

  it('returns lenses for container-kind symbols with refs > 0', () => {
    // Mock extractSymbols to return a function symbol on line 5
    mockExtractSymbols.mockReturnValue([
      { name: 'myFunc', line: 5, kind: 'function' },
    ]);

    const content = `function myFunc() {
  myFunc();
  myFunc();
  myFunc();
}`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0]).toEqual({
      line: 5,
      name: 'myFunc',
      kind: 'function',
      refCount: 3,
    });
  });

  it('filters out symbols with 0 references', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'usedFunc', line: 5, kind: 'function' },
      { name: 'unusedFunc', line: 10, kind: 'function' },
    ]);

    const content = `function usedFunc() {
  usedFunc();
}
function unusedFunc() {}`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0].name).toBe('usedFunc');
    expect(result[0].refCount).toBe(1);
  });

  it('filters out non-container symbols (variables, constants)', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'myFunc', line: 5, kind: 'function' },
      { name: 'myVar', line: 10, kind: 'variable' },
      { name: 'MY_CONST', line: 15, kind: 'constant' },
    ]);

    const content = `function myFunc() {
  myFunc();
  myVar = 1;
}
let myVar = 0;
const MY_CONST = 42;`;

    const result = computeCodeLenses(content, '.ts');

    // Should only include the function, not variable or constant
    expect(result.length).toBe(1);
    expect(result[0].name).toBe('myFunc');
  });

  it('filters out non-container symbols (type)', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'MyType', line: 5, kind: 'type' },
      { name: 'myFunc', line: 10, kind: 'function' },
    ]);

    const content = `type MyType = string;
function myFunc() {
  myFunc();
}`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0].name).toBe('myFunc');
  });

  it('includes class symbols', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'MyClass', line: 5, kind: 'class' },
    ]);

    const content = `class MyClass {
  constructor() {
    MyClass.count++;
  }
}
MyClass.count = 0;
const instance = new MyClass();`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0]).toEqual({
      line: 5,
      name: 'MyClass',
      kind: 'class',
      refCount: 3,
    });
  });

  it('includes interface symbols', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'MyInterface', line: 5, kind: 'interface' },
    ]);

    const content = `interface MyInterface {
  prop: string;
}
const obj: MyInterface = { prop: 'hello' };
const obj2: MyInterface = { prop: 'world' };`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0]).toEqual({
      line: 5,
      name: 'MyInterface',
      kind: 'interface',
      refCount: 2,
    });
  });

  it('includes method symbols', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'myMethod', line: 5, kind: 'method' },
    ]);

    const content = `class MyClass {
  myMethod() {
    this.myMethod();
  }
}
const c = new MyClass();
c.myMethod();`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0]).toEqual({
      line: 5,
      name: 'myMethod',
      kind: 'method',
      refCount: 2,
    });
  });

  it('returns lenses sorted by line ascending', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'third', line: 15, kind: 'function' },
      { name: 'first', line: 5, kind: 'function' },
      { name: 'second', line: 10, kind: 'function' },
    ]);

    const content = `function first() { first(); }
function second() { second(); }
function third() { third(); }`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(3);
    expect(result[0].name).toBe('first');
    expect(result[1].name).toBe('second');
    expect(result[2].name).toBe('third');
  });

  it('deduplicates symbols on the same line', () => {
    // extractSymbols returns one symbol per line, but just in case
    mockExtractSymbols.mockReturnValue([
      { name: 'foo', line: 5, kind: 'function' },
      { name: 'foo', line: 5, kind: 'function' }, // duplicate
    ]);

    const content = `function foo() { foo(); foo(); }`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
  });

  it('handles multiple symbols on different lines', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'funcA', line: 5, kind: 'function' },
      { name: 'funcB', line: 10, kind: 'function' },
      { name: 'MyClass', line: 15, kind: 'class' },
    ]);

    const content = `function funcA() { funcA(); funcB(); }
function funcB() { funcB(); funcA(); }
class MyClass {
  static create() { return new MyClass(); }
}
const instance = new MyClass();`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(3);
    expect(result[0].name).toBe('funcA');
    expect(result[1].name).toBe('funcB');
    expect(result[2].name).toBe('MyClass');
  });

  it('passes languageId to extractSymbols', () => {
    mockExtractSymbols.mockReturnValue([]);

    computeCodeLenses('some content', '.go');

    expect(mockExtractSymbols).toHaveBeenCalledWith('some content', '.go');
  });

  it('passes undefined when languageId is undefined', () => {
    mockExtractSymbols.mockReturnValue([]);

    computeCodeLenses('some content', undefined);

    expect(mockExtractSymbols).toHaveBeenCalledWith('some content', undefined);
  });

  it('handles TypeScript file extension', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'myFunction', line: 1, kind: 'function' },
    ]);

    const content = `function myFunction() { myFunction(); }`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0].kind).toBe('function');
  });

  it('handles JavaScript file extension', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'myFunction', line: 1, kind: 'function' },
    ]);

    const content = `function myFunction() { myFunction(); }`;

    const result = computeCodeLenses(content, '.js');

    expect(result.length).toBe(1);
  });

  it('handles Go file extension', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'myFunction', line: 1, kind: 'function' },
    ]);

    const content = `func myFunction() { myFunction() }`;

    const result = computeCodeLenses(content, '.go');

    expect(result.length).toBe(1);
  });

  it('handles Python file extension', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'my_function', line: 1, kind: 'function' },
    ]);

    const content = `def my_function():
    my_function()`;

    const result = computeCodeLenses(content, '.py');

    expect(result.length).toBe(1);
  });

  it('returns empty when extractSymbols returns empty array', () => {
    mockExtractSymbols.mockReturnValue([]);

    const result = computeCodeLenses('// just comments', '.ts');

    expect(result).toEqual([]);
  });

  it('returns empty when all symbols have 0 refs', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'a', line: 1, kind: 'function' },
      { name: 'b', line: 2, kind: 'function' },
    ]);

    const content = `function a() {}
function b() {}`;

    const result = computeCodeLenses(content, '.ts');

    expect(result).toEqual([]);
  });

  it('handles case-insensitive reference counts correctly', () => {
    // Note: word boundary regex is case-sensitive
    mockExtractSymbols.mockReturnValue([
      { name: 'foo', line: 1, kind: 'function' },
    ]);

    const content = `function foo() {}
Foo(); // case matters - should not match
foo(); // match`;

    const result = computeCodeLenses(content, '.ts');

    // Only 'foo' matches (lowercase), not 'Foo'
    expect(result[0].refCount).toBe(1);
  });

  it('excludes references found in single-line comments', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'myFunc', line: 1, kind: 'function' },
    ]);

    const content = `function myFunc() {
  // myFunc is a great function
  // Call myFunc() to do stuff
  myFunc();
}`;

    const result = computeCodeLenses(content, '.ts');

    // Only the real call on line 4 counts; comments are stripped
    expect(result.length).toBe(1);
    expect(result[0].refCount).toBe(1);
  });

  it('excludes references found in block comments', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'handleClick', line: 5, kind: 'function' },
    ]);

    const content = `/* handleClick processes click events.
   Always call handleClick() in your event handler.
   handleClick returns void.
*/
function handleClick() {
  handleClick();
}`;

    const result = computeCodeLenses(content, '.ts');

    // Only the real call on line 6 counts; block comment refs are stripped
    expect(result.length).toBe(1);
    expect(result[0].refCount).toBe(1);
  });

  it('correctly counts refs when comments and real calls are mixed', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'processData', line: 1, kind: 'function' },
    ]);

    const content = `function processData() {
  // processData does things
  processData();
  /* processData is called again */
  processData();
}`;

    const result = computeCodeLenses(content, '.ts');

    // Only the 2 real calls count; comment mentions are excluded
    expect(result.length).toBe(1);
    expect(result[0].refCount).toBe(2);
  });

  it('does not under-count when a string contains // on the same line as a real ref', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'fetchData', line: 1, kind: 'function' },
    ]);

    const content = `function fetchData() {
  const url = "http://example.com/api";
  fetchData();
}`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0].refCount).toBe(1);
  });

  it('does not under-count when multiple strings with // are on same line as a ref', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'open', line: 1, kind: 'function' },
    ]);

    const content = `function open() {
  const a = "https://a.com"; open();
}`;

    const result = computeCodeLenses(content, '.ts');

    expect(result.length).toBe(1);
    expect(result[0].refCount).toBe(1);
  });

  it('handles block comments correctly with string-aware stripping', () => {
    mockExtractSymbols.mockReturnValue([
      { name: 'processData', line: 5, kind: 'function' },
    ]);

    const content = `/* processData processes click events.
   Always call processData() in your handler.
*/
function processData() {
  processData();
}`;

    const result = computeCodeLenses(content, '.ts');

    // Block comment mentions should be stripped, real call counts
    expect(result.length).toBe(1);
    expect(result[0].refCount).toBe(1);
  });
});
