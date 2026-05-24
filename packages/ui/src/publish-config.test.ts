import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Resolve paths relative to this test file's location
const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, '../../..'); // workspace root

const workflowPath = resolve(ROOT, '.github/workflows/publish-ui.yml');
const packageJsonPath = resolve(__dirname, '../package.json');

function readFile(path: string): string {
  return readFileSync(path, 'utf-8');
}

// ---------------------------------------------------------------------------
// Test Suite 1: Workflow file (.github/workflows/publish-ui.yml)
// ---------------------------------------------------------------------------
describe('publish-ui workflow', () => {
  const workflow = readFile(workflowPath);

  describe('file structure', () => {
    it('exists and is non-empty', () => {
      expect(workflow.length).toBeGreaterThan(0);
    });

    it('has a descriptive name', () => {
      expect(workflow).toMatch(/^name:\s+.+/m);
      expect(workflow).toContain('name: Publish @sprout/ui to npm');
    });
  });

  describe('triggers', () => {
    it('triggers on release with published type', () => {
      // YAML may have 'on:' or 'on: ' followed by 'release:' block with '- published'
      expect(workflow).toMatch(/on:.*\n.*release:/s);
      expect(workflow).toContain('- published');
    });

    it('triggers on workflow_dispatch with a version input', () => {
      expect(workflow).toContain('workflow_dispatch');
      expect(workflow).toContain('version:');
      expect(workflow).toContain('description:');
      expect(workflow).toContain('type:');
    });
  });

  describe('concurrency and permissions', () => {
    it('defines a concurrency group', () => {
      expect(workflow).toContain('concurrency');
      expect(workflow).toContain('group:');
    });

    it('restricts permissions to contents: read', () => {
      expect(workflow).toContain('permissions');
      expect(workflow).toContain('contents: read');
    });
  });

  describe('pipeline steps', () => {
    it('uses actions/checkout@v4', () => {
      expect(workflow).toContain('actions/checkout@v4');
    });

    it('uses actions/setup-node@v4', () => {
      expect(workflow).toContain('actions/setup-node@v4');
    });

    it('sets up Node.js 22', () => {
      expect(workflow).toMatch(/node-version:.*['"]?22['"]?/);
    });

    it('includes an npm ci step', () => {
      expect(workflow).toContain('npm ci');
    });

    it('includes a build step', () => {
      expect(workflow).toContain('npm run build');
    });

    it('includes a test step', () => {
      expect(workflow).toContain('npm test');
    });

    it('includes a type-check step', () => {
      expect(workflow).toContain('npm run type-check');
    });

    it('includes a publish step with npm publish', () => {
      expect(workflow).toContain('npm publish');
    });

    it('uses NPM_TOKEN secret via NODE_AUTH_TOKEN environment variable', () => {
      expect(workflow).toContain('NODE_AUTH_TOKEN');
      expect(workflow).toContain('NPM_TOKEN');
    });
  });

  describe('publish step configuration', () => {
    it('sets working-directory to packages/ui', () => {
      expect(workflow).toContain('working-directory: packages/ui');
    });

    it('publishes with --access public', () => {
      expect(workflow).toContain('--access public');
    });
  });
});

// ---------------------------------------------------------------------------
// Test Suite 2: package.json configuration
// ---------------------------------------------------------------------------
describe('package.json', () => {
  const pkg = JSON.parse(readFile(packageJsonPath));

  describe('identity', () => {
    it('name is @sprout/ui', () => {
      expect(pkg.name).toBe('@sprout/ui');
    });

    it('version is a valid semver string', () => {
      expect(pkg.version).toMatch(/^[0-9]+\.[0-9]+\.[0-9]+/);
    });

    it('license is MIT', () => {
      expect(pkg.license).toBe('MIT');
    });

    it('has a non-empty description', () => {
      expect(typeof pkg.description).toBe('string');
      expect(pkg.description.length).toBeGreaterThan(0);
    });
  });

  describe('entry points', () => {
    it('main points to dist/index.cjs.js', () => {
      expect(pkg.main).toBe('dist/index.cjs.js');
    });

    it('module points to dist/index.esm.js', () => {
      expect(pkg.module).toBe('dist/index.esm.js');
    });

    it('types points to dist/index.d.ts', () => {
      expect(pkg.types).toBe('dist/index.d.ts');
    });
  });

  describe('exports map', () => {
    it('has a top-level entry with import condition', () => {
      expect(pkg.exports['.'].import).toBe('./dist/index.esm.js');
    });

    it('has a top-level entry with require condition', () => {
      expect(pkg.exports['.'].require).toBe('./dist/index.cjs.js');
    });

    it('has a top-level entry with types condition', () => {
      expect(pkg.exports['.'].types).toBe('./dist/index.d.ts');
    });

    it('has a top-level entry with default condition', () => {
      expect(pkg.exports['.'].default).toBe('./dist/index.esm.js');
    });

    it('has a CSS export for dist/style.css', () => {
      expect(pkg.exports['./dist/style.css'].default).toBe('./dist/style.css');
    });
  });

  describe('publish configuration', () => {
    it('publishConfig.access is public', () => {
      expect(pkg.publishConfig.access).toBe('public');
    });

    it('files includes dist', () => {
      expect(pkg.files).toContain('dist');
    });

    it('sideEffects is false', () => {
      expect(pkg.sideEffects).toBe(false);
    });
  });

  describe('repository metadata', () => {
    it('has a repository object with type git', () => {
      expect(pkg.repository.type).toBe('git');
    });

    it('repository url contains github', () => {
      expect(pkg.repository.url).toContain('github');
    });

    it('repository url is a valid git+https url', () => {
      expect(pkg.repository.url).toMatch(/^git\+https:\/\//);
    });

    it('repository specifies the packages/ui directory', () => {
      expect(pkg.repository.directory).toBe('packages/ui');
    });
  });

  describe('scripts', () => {
    it('has a build script using vite', () => {
      expect(pkg.scripts.build).toContain('vite build');
    });

    it('has a test script using vitest', () => {
      expect(pkg.scripts.test).toContain('vitest');
    });

    it('has a type-check script using tsc', () => {
      expect(pkg.scripts['type-check']).toContain('tsc');
    });
  });
});
