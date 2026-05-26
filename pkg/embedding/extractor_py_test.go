package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractPy_SameMethodNameAcrossClasses_DistinctIDs is the
// upstream regression guard for the "node not added" panic the hnsw
// store hit. Before `makeUnitID` added the `#L<line>` suffix, two
// methods named `run` in different classes of the same file both
// serialized to `path:run` → duplicate record IDs → hnsw Add panic.
// With the line suffix, each method gets its own ID.
func TestExtractPy_SameMethodNameAcrossClasses_DistinctIDs(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "collisions.py", `class A:
    def run(self):
        return "a"

class B:
    def run(self):
        return "b"
`)
	units, err := ExtractFromFile(path)
	if err != nil {
		t.Fatalf("ExtractFromFile: %v", err)
	}

	seen := map[string]string{}
	for _, u := range units {
		if prev, ok := seen[u.ID]; ok {
			t.Errorf("duplicate ID %q (first body: %q; second body: %q) — line disambiguation regressed", u.ID, prev, u.Body)
		}
		seen[u.ID] = u.Body
	}

	// Sanity: both A.run and B.run should be extracted, with distinct IDs.
	var aRunID, bRunID string
	for _, u := range units {
		switch u.Name {
		case "A.run":
			aRunID = u.ID
		case "B.run":
			bRunID = u.ID
		}
	}
	if aRunID == "" || bRunID == "" {
		t.Fatalf("missing A.run or B.run in extracted units (names found: %v)", names(units))
	}
	if aRunID == bRunID {
		t.Fatalf("A.run and B.run share an ID %q — line suffix not applied", aRunID)
	}
}

// names returns the Name field of each unit, for diagnostic test output.
func names(units []CodeUnit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.Name
	}
	return out
}

// writeTempPyFile creates a temp .py file with the given content and returns its path.
func writeTempPyFile(dir, name, content string) string {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		panic(err)
	}
	return path
}

func TestExtractPyBasicFunction(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "example.py", `def greet(name):
    print(f"Hello, {name}!")
    return True
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d: %+v", len(units), units)
	}

	u := units[0]

	if u.Name != "greet" {
		t.Errorf("expected name 'greet', got %q", u.Name)
	}

	if u.Language != "python" {
		t.Errorf("expected language 'python', got %q", u.Language)
	}

	if u.File != path {
		t.Errorf("expected file %q, got %q", path, u.File)
	}

	expectedID := fmt.Sprintf("%s:greet#L%d", path, u.StartLine)
	if u.ID != expectedID {
		t.Errorf("expected ID %q, got %q", expectedID, u.ID)
	}

	// Verify StartLine and EndLine.
	// Note: trailing newline produces an extra empty line that findBodyEnd includes.
	if u.StartLine != 1 {
		t.Errorf("expected StartLine 1, got %d", u.StartLine)
	}
	if u.EndLine != 4 {
		t.Errorf("expected EndLine 4, got %d", u.EndLine)
	}

	// Verify body contains function body content.
	if !strings.Contains(u.Body, "print") {
		t.Errorf("body should contain 'print': %q", u.Body)
	}
	if !strings.Contains(u.Body, "return True") {
		t.Errorf("body should contain 'return True': %q", u.Body)
	}

	// Verify hash is non-empty.
	if u.Hash == "" {
		t.Error("expected non-empty hash")
	}
}

func TestExtractPyMultipleFunctions(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "multi.py", `def add(a, b):
    return a + b

def subtract(a, b):
    return a - b

def multiply(a, b):
    return a * b
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 3 {
		t.Fatalf("expected 3 units, got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	for _, expected := range []string{"add", "subtract", "multiply"} {
		if !names[expected] {
			t.Errorf("missing function %q", expected)
		}
	}
}

func TestExtractPyClassWithMethods(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "class.py", `class Counter:
    def __init__(self):
        self.value = 0

    def increment(self):
        self.value += 1

    def decrement(self):
        self.value -= 1

    def get_value(self):
        return self.value
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have the class plus its methods (4 units total).
	if len(units) < 4 {
		t.Fatalf("expected at least 4 units (class + 4 methods), got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	// Class itself should be extracted.
	if !names["Counter"] {
		t.Errorf("missing class 'Counter', found: %v", names)
	}

	// Methods should have ClassName.methodName format.
	for _, expected := range []string{"Counter.__init__", "Counter.increment", "Counter.decrement", "Counter.get_value"} {
		if !names[expected] {
			t.Errorf("missing method %q, found: %v", expected, names)
		}
	}

	// Verify method IDs use the format file:ClassName.method_name#L<startLine>.
	// The #L suffix disambiguates same-named methods across classes in the
	// same file (see embedding/extractor.go:makeUnitID for context).
	for _, u := range units {
		if strings.Contains(u.Name, ".") {
			expectedID := fmt.Sprintf("%s:%s#L%d", path, u.Name, u.StartLine)
			if u.ID != expectedID {
				t.Errorf("method %q: expected ID %q, got %q", u.Name, expectedID, u.ID)
			}
		}
	}

	// Verify class ID.
	classUnit := units[0]
	if classUnit.Name == "Counter" {
		expectedID := fmt.Sprintf("%s:Counter#L%d", path, classUnit.StartLine)
		if classUnit.ID != expectedID {
			t.Errorf("class: expected ID %q, got %q", expectedID, classUnit.ID)
		}
	}
}

func TestExtractPyAsyncFunction(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "async.py", `async def fetch(url):
    response = await http.get(url)
    return response.text()

def sync_work():
    return True
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	if !names["fetch"] {
		t.Errorf("missing async function 'fetch', found: %v", names)
	}
	if !names["sync_work"] {
		t.Errorf("missing function 'sync_work', found: %v", names)
	}

	// Verify signature contains "async def".
	for _, u := range units {
		if u.Name == "fetch" {
			if !strings.Contains(u.Signature, "async def") {
				t.Errorf("signature for async function missing 'async def': %q", u.Signature)
			}
		}
	}
}

func TestExtractPyDecorators(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "decorators.py", `@staticmethod
def helper():
    return True

@cache
@lru_cache(maxsize=128)
def expensive(x):
    return x * x

@property
def name(self):
    return self._name
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 3 {
		t.Fatalf("expected 3 units, got %d: %v", len(units), collectNames(units))
	}

	// Verify decorator is included in the body for each function.
	for _, u := range units {
		if u.Name == "helper" {
			if !strings.Contains(u.Body, "@staticmethod") {
				t.Errorf("body for 'helper' should contain @staticmethod: %q", u.Body)
			}
		}
		if u.Name == "expensive" {
			if !strings.Contains(u.Body, "@cache") {
				t.Errorf("body for 'expensive' should contain @cache: %q", u.Body)
			}
			if !strings.Contains(u.Body, "@lru_cache") {
				t.Errorf("body for 'expensive' should contain @lru_cache: %q", u.Body)
			}
		}
		if u.Name == "name" {
			if !strings.Contains(u.Body, "@property") {
				t.Errorf("body for 'name' should contain @property: %q", u.Body)
			}
		}
	}
}

func TestExtractPyDecoratedClass(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "decorated_class.py", `@dataclass
class Point:
    x: int
    y: int

    def distance(self):
        return (self.x ** 2 + self.y ** 2) ** 0.5
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) < 1 {
		t.Fatalf("expected at least 1 unit, got %d: %v", len(units), collectNames(units))
	}

	classUnit := units[0]
	if classUnit.Name != "Point" {
		t.Errorf("expected class 'Point', got %q", classUnit.Name)
	}

	// Body should include the decorator.
	if !strings.Contains(classUnit.Body, "@dataclass") {
		t.Errorf("body for class 'Point' should contain @dataclass: %q", classUnit.Body)
	}
}

func TestExtractPySkipTestFunctions(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "app.py", `def do_work():
    return True

def test_do_work():
    assert do_work() == True

def test_something_else():
    pass

def helper():
    return 42
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only non-test functions should be extracted.
	if len(units) != 2 {
		t.Fatalf("expected 2 units (non-test functions), got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	if !names["do_work"] {
		t.Error("missing function 'do_work'")
	}
	if !names["helper"] {
		t.Error("missing function 'helper'")
	}
	if names["test_do_work"] {
		t.Error("test_do_work should be excluded")
	}
	if names["test_something_else"] {
		t.Error("test_something_else should be excluded")
	}
}

func TestExtractPyIncludeTests(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "app.py", `def do_work():
    return True

def test_do_work():
    assert do_work() == True
`)

	units, err := ExtractPyFile(path, WithIncludeTests(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 2 {
		t.Fatalf("expected 2 units with IncludeTests, got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	if !names["do_work"] {
		t.Error("missing function 'do_work'")
	}
	if !names["test_do_work"] {
		t.Error("missing function 'test_do_work'")
	}
}

func TestExtractPySkipTestFiles(t *testing.T) {
	dir := t.TempDir()
	src := `def test_something():
    assert True

def helper():
    return True
`

	// test_*.py should be skipped
	testPath := writeTempPyFile(dir, "test_example.py", src)
	units, err := ExtractPyFile(testPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(units) != 0 {
		t.Errorf("expected 0 units from test_example.py, got %d: %v", len(units), collectNames(units))
	}

	// *_test.py should be skipped
	testPath2 := writeTempPyFile(dir, "example_test.py", src)
	units, err = ExtractPyFile(testPath2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(units) != 0 {
		t.Errorf("expected 0 units from example_test.py, got %d: %v", len(units), collectNames(units))
	}

	// With IncludeTests=true, test files should be processed.
	units, err = ExtractPyFile(testPath, WithIncludeTests(true))
	if err != nil {
		t.Fatalf("unexpected error with IncludeTests: %v", err)
	}
	if len(units) == 0 {
		t.Error("expected units from test_example.py with IncludeTests=true")
	}
}

func TestExtractPyNestedFunction(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "nested.py", `def outer():
    def inner():
        return True
    return inner()

def sibling():
    return False
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only top-level functions should be extracted; inner() is nested.
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	if !names["outer"] {
		t.Error("missing function 'outer'")
	}
	if !names["sibling"] {
		t.Error("missing function 'sibling'")
	}
	if names["inner"] {
		t.Error("nested function 'inner' should NOT be extracted as a top-level unit")
	}

	// Verify that the body of 'outer' includes 'inner'.
	for _, u := range units {
		if u.Name == "outer" {
			if !strings.Contains(u.Body, "inner") {
				t.Errorf("body of 'outer' should contain nested 'inner': %q", u.Body)
			}
		}
	}
}

func TestExtractPyLineRanges(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "ranges.py", `# Line 1: comment
# Line 2: another comment

def alpha():        # Line 4
    return 1       # Line 5
                    # Line 6 (blank)

def beta():         # Line 8
    x = 2          # Line 9
    y = 3          # Line 10
    return x + y   # Line 11
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}

	// alpha should start at line 4.
	alpha := units[0]
	if alpha.Name != "alpha" {
		t.Errorf("expected first function 'alpha', got %q", alpha.Name)
	}
	if alpha.StartLine != 4 {
		t.Errorf("alpha: expected StartLine 4, got %d", alpha.StartLine)
	}
	// Body includes blank line 6 and trailing newline produces extra line 7.
	if alpha.EndLine < 5 || alpha.EndLine > 8 {
		t.Errorf("alpha: EndLine %d is out of expected range [5, 8]", alpha.EndLine)
	}

	// beta should start at line 8.
	beta := units[1]
	if beta.Name != "beta" {
		t.Errorf("expected second function 'beta', got %q", beta.Name)
	}
	if beta.StartLine != 8 {
		t.Errorf("beta: expected StartLine 8, got %d", beta.StartLine)
	}
	// Body includes trailing newline.
	if beta.EndLine < 11 || beta.EndLine > 13 {
		t.Errorf("beta: EndLine %d is out of expected range [11, 13]", beta.EndLine)
	}

	// Verify all line ranges are valid.
	for _, u := range units {
		if u.StartLine <= 0 || u.EndLine < u.StartLine {
			t.Errorf("invalid line range for %s: start=%d end=%d", u.Name, u.StartLine, u.EndLine)
		}
	}
}

func TestExtractPySignatureAndBody(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "sig.py", `def process_items(items):
    return [item.strip() for item in items]
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	u := units[0]

	if u.Name != "process_items" {
		t.Errorf("expected name 'process_items', got %q", u.Name)
	}

	// Signature should be just the def line (trimmed).
	if !strings.Contains(u.Signature, "def process_items") {
		t.Errorf("signature missing function name: %q", u.Signature)
	}
	if !strings.Contains(u.Signature, "items") {
		t.Errorf("signature missing parameter: %q", u.Signature)
	}

	// Body should contain the body content.
	if !strings.Contains(u.Body, "return") {
		t.Errorf("body missing return statement: %q", u.Body)
	}

	// Signature should NOT contain the body content (just the def line).
	if strings.Contains(u.Signature, "return") {
		t.Errorf("signature should not contain body: %q", u.Signature)
	}
}

func TestExtractPyFromExtractFromFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "test.py", `def hello():
    return "world"
`)

	// ExtractFromFile should route .py files to ExtractPyFile.
	units, err := ExtractFromFile(path)
	if err != nil {
		t.Fatalf("ExtractFromFile failed: %v", err)
	}

	if len(units) == 0 {
		t.Error("expected at least 1 unit from .py file via ExtractFromFile")
	}

	if len(units) > 0 {
		if units[0].Language != "python" {
			t.Errorf("expected language 'python', got %q", units[0].Language)
		}
	}
}

func TestExtractPyNonExistentFile(t *testing.T) {
	_, err := ExtractPyFile("/tmp/nonexistent_file_xyz.py")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestExtractPyClassWithDecoratedMethods(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "decorated_methods.py", `class MyClass:
    @staticmethod
    def static_method():
        return True

    @classmethod
    def class_method(cls):
        return cls()

    @property
    def value(self):
        return self._value
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	if !names["MyClass"] {
		t.Error("missing class 'MyClass'")
	}
	if !names["MyClass.static_method"] {
		t.Error("missing method 'MyClass.static_method'")
	}
	if !names["MyClass.class_method"] {
		t.Error("missing method 'MyClass.class_method'")
	}
	if !names["MyClass.value"] {
		t.Error("missing method 'MyClass.value'")
	}

	// Verify decorators are in the method bodies.
	for _, u := range units {
		if u.Name == "MyClass.static_method" {
			if !strings.Contains(u.Body, "@staticmethod") {
				t.Errorf("body of static_method should contain @staticmethod: %q", u.Body)
			}
		}
		if u.Name == "MyClass.class_method" {
			if !strings.Contains(u.Body, "@classmethod") {
				t.Errorf("body of class_method should contain @classmethod: %q", u.Body)
			}
		}
		if u.Name == "MyClass.value" {
			if !strings.Contains(u.Body, "@property") {
				t.Errorf("body of value should contain @property: %q", u.Body)
			}
		}
	}
}

func TestExtractPyMultipleClasses(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "multiple_classes.py", `class Parent:
    def greet(self):
        return "parent"

class Child(Parent):
    def greet(self):
        return "child"
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	if !names["Parent"] {
		t.Error("missing class 'Parent'")
	}
	if !names["Parent.greet"] {
		t.Error("missing method 'Parent.greet'")
	}
	if !names["Child"] {
		t.Error("missing class 'Child'")
	}
	if !names["Child.greet"] {
		t.Error("missing method 'Child.greet'")
	}
}

func TestExtractPyFunctionWithBlankLinesInBody(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "blanks.py", `def complex_function():
    # Step 1
    x = 1

    # Step 2
    y = 2

    return x + y

def next_function():
    return True
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d: %v", len(units), collectNames(units))
	}

	// complex_function body should include blank lines and comments.
	cf := units[0]
	if cf.Name != "complex_function" {
		t.Errorf("expected 'complex_function', got %q", cf.Name)
	}

	if !strings.Contains(cf.Body, "x = 1") {
		t.Errorf("body should contain 'x = 1': %q", cf.Body)
	}
	if !strings.Contains(cf.Body, "y = 2") {
		t.Errorf("body should contain 'y = 2': %q", cf.Body)
	}
	if !strings.Contains(cf.Body, "return x + y") {
		t.Errorf("body should contain 'return x + y': %q", cf.Body)
	}
}

func TestExtractPyEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "empty.py", "")

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 0 {
		t.Errorf("expected 0 units from empty file, got %d", len(units))
	}
}

func TestExtractPyFileWithOnlyComments(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "comments.py", `# This is just a comment
# Nothing to see here
# Just comments
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 0 {
		t.Errorf("expected 0 units from comment-only file, got %d", len(units))
	}
}

func TestExtractPyMethodsNotExtractedAsTopLevel(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "no_dup.py", `class Service:
    def handle(self):
        return True

    def process(self):
        return False
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	// Methods should NOT appear as standalone top-level functions.
	if names["handle"] {
		t.Error("'handle' should not appear as a top-level function (it's a method of Service)")
	}
	if names["process"] {
		t.Error("'process' should not appear as a top-level function (it's a method of Service)")
	}

	// They should appear as class methods.
	if !names["Service.handle"] {
		t.Error("missing method 'Service.handle'")
	}
	if !names["Service.process"] {
		t.Error("missing method 'Service.process'")
	}
}

func TestExtractPyClassLineRanges(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "class_ranges.py", `class MyClass:         # line 1
    def method1(self): # line 2
        return 1       # line 3
                       # line 4
    def method2(self): # line 5
        return 2       # line 6
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First unit should be the class.
	classUnit := units[0]
	if classUnit.Name != "MyClass" {
		t.Errorf("expected class 'MyClass', got %q", classUnit.Name)
	}
	if classUnit.StartLine != 1 {
		t.Errorf("class: expected StartLine 1, got %d", classUnit.StartLine)
	}

	// Class body should span to the last method.
	if classUnit.EndLine < 5 {
		t.Errorf("class: expected EndLine >= 5, got %d (body should include all methods)", classUnit.EndLine)
	}

	// Methods should have correct line ranges.
	for _, u := range units {
		if u.Name == "MyClass.method1" {
			if u.StartLine != 2 {
				t.Errorf("method1: expected StartLine 2, got %d", u.StartLine)
			}
			// Body includes trailing blank/newline line.
			if u.EndLine < 3 || u.EndLine > 5 {
				t.Errorf("method1: EndLine %d is out of expected range [3, 5]", u.EndLine)
			}
		}
		if u.Name == "MyClass.method2" {
			if u.StartLine != 5 {
				t.Errorf("method2: expected StartLine 5, got %d", u.StartLine)
			}
			// Body includes trailing newline.
			if u.EndLine < 6 || u.EndLine > 8 {
				t.Errorf("method2: EndLine %d is out of expected range [6, 8]", u.EndLine)
			}
		}
	}
}

func TestExtractPyFunctionWithDocstring(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "docstring.py", `def documented():
    """This function does a thing."""
    return True
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	u := units[0]
	if !strings.Contains(u.Body, "This function does a thing") {
		t.Errorf("body should contain docstring: %q", u.Body)
	}
	if !strings.Contains(u.Body, "return True") {
		t.Errorf("body should contain return statement: %q", u.Body)
	}
}

func TestExtractPyFunctionAtEOFNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "eof.py", `def last():
    return True`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	if units[0].Name != "last" {
		t.Errorf("expected 'last', got %q", units[0].Name)
	}
}

func TestExtractPyClassWithNoMethods(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "empty_class.py", `class Empty:
    """A class with no methods."""
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	if units[0].Name != "Empty" {
		t.Errorf("expected 'Empty', got %q", units[0].Name)
	}
}

func TestExtractPyMixedFunctionsAndClasses(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "mixed.py", `def setup():
    return True

class App:
    def run(self):
        return True

def teardown():
    return True

class Config:
    def load(self):
        return {}
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: setup, App, App.run, teardown, Config, Config.load = 6
	if len(units) < 6 {
		t.Fatalf("expected at least 6 units, got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	for _, expected := range []string{"setup", "App", "App.run", "teardown", "Config", "Config.load"} {
		if !names[expected] {
			t.Errorf("missing %q, found: %v", expected, names)
		}
	}
}

func TestExtractPyHashConsistency(t *testing.T) {
	dir := t.TempDir()
	src := `def greet(name):
    print(f"Hello, {name}!")
`

	// Write same content twice to two different files.
	path1 := writeTempPyFile(dir, "file1.py", src)
	path2 := writeTempPyFile(dir, "file2.py", src)

	units1, err := ExtractPyFile(path1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	units2, err := ExtractPyFile(path2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units1) != 1 || len(units2) != 1 {
		t.Fatalf("expected 1 unit each, got %d and %d", len(units1), len(units2))
	}

	// Same content should produce the same hash.
	if units1[0].Hash != units2[0].Hash {
		t.Errorf("same content should produce same hash: %q vs %q", units1[0].Hash, units2[0].Hash)
	}

	// But IDs should differ (different file paths).
	if units1[0].ID == units2[0].ID {
		t.Error("different files should produce different IDs")
	}
}

func TestExtractPyLanguage(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "lang.py", `def x(): pass`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	if units[0].Language != "python" {
		t.Errorf("expected language 'python', got %q", units[0].Language)
	}
}

func TestExtractPyClassLanguage(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "lang_class.py", `class A:
    def m(self): pass`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, u := range units {
		if u.Language != "python" {
			t.Errorf("expected language 'python' for %q, got %q", u.Name, u.Language)
		}
	}
}

func TestExtractPyTabsInIndentation(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "tabs.py", "def greet():\n\tprint('hello')\n")

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	if units[0].Name != "greet" {
		t.Errorf("expected 'greet', got %q", units[0].Name)
	}

	if units[0].EndLine < 2 || units[0].EndLine > 4 {
		t.Errorf("expected EndLine in range [2, 4], got %d", units[0].EndLine)
	}
}

func TestExtractPyAsyncClassMethod(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "async_class.py", `class Service:
    async def fetch(self, url):
        return await get(url)

    def sync(self):
        return True
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	if !names["Service"] {
		t.Error("missing class 'Service'")
	}
	if !names["Service.fetch"] {
		t.Error("missing method 'Service.fetch'")
	}
	if !names["Service.sync"] {
		t.Error("missing method 'Service.sync'")
	}

	// Verify async method signature contains "async def".
	for _, u := range units {
		if u.Name == "Service.fetch" {
			if !strings.Contains(u.Signature, "async def") {
				t.Errorf("signature of Service.fetch missing 'async def': %q", u.Signature)
			}
		}
	}
}

func TestExtractPyMethodBodyNotTruncated(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPyFile(dir, "body.py", `class Store:
    def add(self, item):
        self.items.append(item)
        return True

    def remove(self, item):
        self.items.remove(item)
        return True
`)

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, u := range units {
		if u.Name == "Store.add" {
			if !strings.Contains(u.Body, "append") {
				t.Errorf("body of 'add' should contain 'append': %q", u.Body)
			}
			if !strings.Contains(u.Body, "return True") {
				t.Errorf("body of 'add' should contain 'return True': %q", u.Body)
			}
		}
		if u.Name == "Store.remove" {
			if !strings.Contains(u.Body, "remove") {
				t.Errorf("body of 'remove' should contain 'remove': %q", u.Body)
			}
			if !strings.Contains(u.Body, "return True") {
				t.Errorf("body of 'remove' should contain 'return True': %q", u.Body)
			}
		}
	}
}
