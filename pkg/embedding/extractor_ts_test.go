package embedding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempTSFile creates a temp .ts/.js file with the given content and returns its path.
func writeTempTSFile(dir, name, content string) string {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		panic(err)
	}
	return path
}

// collectNames extracts just the Name field from a slice of CodeUnits.
func collectNames(units []CodeUnit) []string {
	names := make([]string, len(units))
	for i, u := range units {
		names[i] = u.Name
	}
	return names
}

func TestExtractTSFunctions(t *testing.T) {
	dir := t.TempDir()
	src := `// Basic TypeScript file
function greet(name: string): string {
	return "Hello, " + name;
}

function add(a: number, b: number): number {
	return a + b;
}

async function fetchData(url: string): Promise<string> {
	const response = await fetch(url);
	return response.text();
}
`
	path := writeTempTSFile(dir, "funcs.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 3 {
		t.Fatalf("expected 3 functions, got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	for _, expected := range []string{"greet", "add", "fetchData"} {
		if !names[expected] {
			t.Errorf("missing function %q", expected)
		}
	}

	// Verify each unit has valid line ranges.
	for _, u := range units {
		if u.StartLine <= 0 || u.EndLine < u.StartLine {
			t.Errorf("invalid line range for %s: start=%d end=%d", u.Name, u.StartLine, u.EndLine)
		}
		if u.Language != "typescript" {
			t.Errorf("expected language 'typescript', got %q for %s", u.Language, u.Name)
		}
		if u.File != path {
			t.Errorf("expected file %q, got %q for %s", path, u.File, u.Name)
		}
		if u.Hash == "" {
			t.Errorf("missing hash for %s", u.Name)
		}
	}
}

func TestExtractTSClassWithMethods(t *testing.T) {
	dir := t.TempDir()
	src := `class Counter {
	constructor(private value: number = 0) {}

	increment(): void {
		this.value++;
	}

	decrement(): void {
		this.value--;
	}

	getValue(): number {
		return this.value;
	}

	reset(): void {
		this.value = 0;
	}
}
`
	path := writeTempTSFile(dir, "counter.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// Should have the class plus its methods.
	if len(units) < 5 {
		t.Fatalf("expected at least 5 units (class + 5 methods), got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	// Class itself should be extracted.
	if !names["Counter"] {
		t.Errorf("missing class 'Counter'")
	}

	// Methods should have ClassName.methodName format.
	for _, expected := range []string{"Counter.increment", "Counter.decrement", "Counter.getValue", "Counter.reset"} {
		if !names[expected] {
			t.Errorf("missing method %q, found: %v", expected, names)
		}
	}
}

func TestExtractTSArrowFunctions(t *testing.T) {
	dir := t.TempDir()
	src := `const double = (x: number): number => {
	return x * 2;
}

const greet = (name: string) => {
	console.log("Hello, " + name);
}

let handler = async (event: any) => {
	await process(event);
	return true;
}

const noop = () => {
	// intentionally empty
}
`
	path := writeTempTSFile(dir, "arrows.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) < 3 {
		t.Fatalf("expected at least 3 arrow functions, got %d: %v", len(units), collectNames(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	for _, expected := range []string{"double", "greet", "handler", "noop"} {
		if !names[expected] {
			t.Errorf("missing arrow function %q", expected)
		}
	}
}

func TestExtractTSExportedFunctions(t *testing.T) {
	dir := t.TempDir()
	src := `export function publicAPI(x: string): string {
	return x.toUpperCase();
}

export class Config {
	constructor(public host: string) {}
}

export const version = "1.0.0"

export default function handler(req: any) {
	return { status: 200 };
}

export async function fetchUsers(): Promise<User[]> {
	const res = await fetch("/api/users");
	return res.json();
}
`
	path := writeTempTSFile(dir, "exports.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	// publicAPI and handler should be found.
	for _, expected := range []string{"publicAPI", "handler", "fetchUsers"} {
		if !names[expected] {
			t.Errorf("missing exported construct %q, found: %v", expected, names)
		}
	}

	// Config class should be found.
	if !names["Config"] {
		t.Errorf("missing exported class 'Config', found: %v", names)
	}
}

func TestExtractTSSkipTestFiles(t *testing.T) {
	dir := t.TempDir()
	src := `import { describe, it, expect } from "vitest";

describe("MyTest", () => {
	it("works", () => {
		expect(true).toBe(true);
	});
});

function underTest(): string {
	return "ok";
}
`
	// Write a .test.ts file
	testPath := writeTempTSFile(dir, "mylib.test.ts", src)
	units, err := ExtractTSFile(testPath)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// Test files should return empty by default.
	if len(units) != 0 {
		t.Errorf("expected 0 units from .test.ts file, got %d: %v", len(units), collectNames(units))
	}

	// Same for .spec.ts
	specPath := writeTempTSFile(dir, "mylib.spec.ts", src)
	units, err = ExtractTSFile(specPath)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 0 {
		t.Errorf("expected 0 units from .spec.ts file, got %d: %v", len(units), collectNames(units))
	}

	// .test.js and .spec.js should also be skipped.
	jsTestPath := writeTempTSFile(dir, "mylib.test.js", "function test() {}")
	units, err = ExtractTSFile(jsTestPath)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 0 {
		t.Errorf("expected 0 units from .test.js file, got %d", len(units))
	}

	// With IncludeTests=true, test files should be processed.
	units, err = ExtractTSFile(testPath, WithIncludeTests(true))
	if err != nil {
		t.Fatalf("extract with IncludeTests failed: %v", err)
	}

	if len(units) == 0 {
		t.Error("expected units from .test.ts file with IncludeTests=true")
	}
}

func TestExtractTSSkipDeclarationFiles(t *testing.T) {
	dir := t.TempDir()
	src := `declare module "my-module" {
	export function doSomething(): void;
	export class MyClass {
		constructor(x: number);
		method(): string;
	}
}
`
	path := writeTempTSFile(dir, "my-module.d.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 0 {
		t.Errorf("expected 0 units from .d.ts file, got %d: %v", len(units), collectNames(units))
	}
}

func TestExtractTSJavaScriptFile(t *testing.T) {
	dir := t.TempDir()
	src := `function hello() {
	return "world";
}

const add = (a, b) => {
	return a + b;
}

class Greeter {
	constructor(message) {
		this.message = message;
	}

	greet() {
		return this.message;
	}
}
`
	path := writeTempTSFile(dir, "app.js", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
		if u.Language != "javascript" {
			t.Errorf("expected language 'javascript', got %q for %s", u.Language, u.Name)
		}
	}

	if !names["hello"] {
		t.Errorf("missing function 'hello', found: %v", names)
	}
	if !names["add"] {
		t.Errorf("missing arrow function 'add', found: %v", names)
	}
	if !names["Greeter"] {
		t.Errorf("missing class 'Greeter', found: %v", names)
	}
}

func TestExtractTSMixedDeclarations(t *testing.T) {
	dir := t.TempDir()
	src := `// Mixed declarations in a module

const MAX_RETRIES = 5

function retry(fn: () => void, retries?: number): void {
	if (retries == null) retries = MAX_RETRIES;
	if (retries <= 0) return;
	try {
		fn();
	} catch (e) {
		retry(fn, retries - 1);
	}
}

export const config = {
	timeout: 5000,
	retries: 3,
	onError: (err: Error) => {
		console.error(err);
	}
}

export class Service {
	private clients: Map<string, any>;

	constructor() {
		this.clients = new Map();
	}

	connect(name: string): void {
		this.clients.set(name, {});
	}

	disconnect(name: string): void {
		this.clients.delete(name);
	}
}
`
	path := writeTempTSFile(dir, "mixed.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	// Should have the retry function, Service class, and its methods.
	if !names["retry"] {
		t.Errorf("missing function 'retry', found: %v", names)
	}
	if !names["Service"] {
		t.Errorf("missing class 'Service', found: %v", names)
	}
	if !names["Service.connect"] {
		t.Errorf("missing method 'Service.connect', found: %v", names)
	}
	if !names["Service.disconnect"] {
		t.Errorf("missing method 'Service.disconnect', found: %v", names)
	}
}

func TestExtractTSFromExtractFromFile(t *testing.T) {
	dir := t.TempDir()
	src := `export function hello() {
	return "world";
}
`
	path := writeTempTSFile(dir, "hello.ts", src)

	// ExtractFromFile should route .ts files to ExtractTSFile.
	units, err := ExtractFromFile(path)
	if err != nil {
		t.Fatalf("ExtractFromFile failed: %v", err)
	}

	if len(units) == 0 {
		t.Error("expected at least 1 unit from .ts file via ExtractFromFile")
	}
}

func TestExtractTSNonExistentFile(t *testing.T) {
	_, err := ExtractTSFile("/tmp/nonexistent_file_xyz.ts")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestExtractTSLineRanges(t *testing.T) {
	dir := t.TempDir()
	src := `// line 1
// line 2
function alpha(): string {       // line 3
	return "alpha";               // line 4
}                              // line 5
                               // line 6
function beta(): number {        // line 7
	return 42;                   // line 8
}                              // line 9
`
	path := writeTempTSFile(dir, "lines.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(units))
	}

	// Verify line ranges.
	for _, u := range units {
		if u.StartLine <= 0 {
			t.Errorf("start line <= 0 for %s", u.Name)
		}
		if u.EndLine < u.StartLine {
			t.Errorf("end line < start line for %s: %d < %d", u.Name, u.EndLine, u.StartLine)
		}
	}

	// Alpha should start at line 3, beta at line 7.
	alpha := units[0]
	beta := units[1]

	if alpha.Name != "alpha" {
		t.Errorf("expected first function to be 'alpha', got %q", alpha.Name)
	}
	if beta.Name != "beta" {
		t.Errorf("expected second function to be 'beta', got %q", beta.Name)
	}

	if alpha.StartLine < 2 || alpha.StartLine > 4 {
		t.Errorf("alpha start line %d is unexpected (expected around 3)", alpha.StartLine)
	}
	if beta.StartLine < 6 || beta.StartLine > 8 {
		t.Errorf("beta start line %d is unexpected (expected around 7)", beta.StartLine)
	}
}

func TestExtractTSSignatureAndBody(t *testing.T) {
	dir := t.TempDir()
	src := `function processItems(items: string[]): string[] {
	return items.map(item => item.trim());
}
`
	path := writeTempTSFile(dir, "sig.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	u := units[0]
	if u.Name != "processItems" {
		t.Errorf("expected name 'processItems', got %q", u.Name)
	}

	// Signature should contain the function name and parameters.
	if !strings.Contains(u.Signature, "processItems") {
		t.Errorf("signature missing function name: %q", u.Signature)
	}

	// Body should contain the return statement.
	if !strings.Contains(u.Body, "return") {
		t.Errorf("body missing return statement: %q", u.Body)
	}

	// Body should have the opening brace.
	if !strings.Contains(u.Body, "{") {
		t.Errorf("body missing opening brace: %q", u.Body)
	}
}

func TestExtractTSXFile(t *testing.T) {
	dir := t.TempDir()
	src := `import React from "react";

function Button({ label, onClick }: { label: string; onClick: () => void }) {
	return <button onClick={onClick}>{label}</button>;
}

const Header = () => {
	return <header>Welcome</header>;
}
`
	path := writeTempTSFile(dir, "components.tsx", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) < 1 {
		t.Fatalf("expected at least 1 unit from .tsx, got %d: %v", len(units), collectNames(units))
	}

	for _, u := range units {
		if u.Language != "tsx" {
			t.Errorf("expected language 'tsx', got %q for %s", u.Language, u.Name)
		}
	}
}

func TestExtractTSXFileViaExtractFromFile(t *testing.T) {
	dir := t.TempDir()
	src := `export default function App() {
	return <div>Hello</div>;
}
`
	path := writeTempTSFile(dir, "App.tsx", src)
	units, err := ExtractFromFile(path)
	if err != nil {
		t.Fatalf("ExtractFromFile failed for .tsx: %v", err)
	}

	if len(units) == 0 {
		t.Error("expected units from .tsx file via ExtractFromFile")
	}
}

func TestExtractTSWithStringsInBody(t *testing.T) {
	dir := t.TempDir()
	src := `function formatJSON(obj: any): string {
	const json = JSON.stringify(obj);
	console.log("{ this is a string with braces }");
	return "{ " + json + " }";
}
`
	path := writeTempTSFile(dir, "strings.ts", src)
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 function, got %d: %v", len(units), collectNames(units))
	}

	u := units[0]
	if u.Name != "formatJSON" {
		t.Errorf("expected name 'formatJSON', got %q", u.Name)
	}

	// Body should end with a closing brace, not be truncated by string braces.
	if !strings.Contains(u.Body, "return") {
		t.Errorf("body seems truncated: %q", u.Body)
	}
}
