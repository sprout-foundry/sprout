import { describe, it, expect } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';

// Resolve paths relative to packages/ui directory
const UI_ROOT = path.resolve(__dirname, '../..');
const STORYBOOK_DIR = path.join(UI_ROOT, '.storybook');

describe('Storybook 8 Configuration', () => {
  // ---------------------------------------------------------------------------
  // Configuration File Existence
  // ---------------------------------------------------------------------------
  describe('Configuration file existence', () => {
    it('should have .storybook/main.ts', () => {
      expect(fs.existsSync(path.join(STORYBOOK_DIR, 'main.ts'))).toBe(true);
    });

    it('should have .storybook/preview.tsx', () => {
      expect(fs.existsSync(path.join(STORYBOOK_DIR, 'preview.tsx'))).toBe(true);
    });

    it('should have tsconfig.storybook.json at project root', () => {
      expect(fs.existsSync(path.join(UI_ROOT, 'tsconfig.storybook.json'))).toBe(true);
    });
  });

  // ---------------------------------------------------------------------------
  // main.ts Configuration
  // ---------------------------------------------------------------------------
  describe('main.ts configuration', async () => {
    let config: any;

    beforeAll(async () => {
      // Dynamically import the Storybook config
      const mainModule = await import('../../.storybook/main');
      config = mainModule.default;
    });

    it('should export a default configuration object', () => {
      expect(config).toBeDefined();
      expect(typeof config).toBe('object');
    });

    it('should have the correct stories glob pattern', () => {
      expect(Array.isArray(config.stories)).toBe(true);
      expect(config.stories).toContain('../src/**/*.stories.@(js|jsx|mjs|ts|tsx)');
    });

    it('should have @storybook/addon-links in addons', () => {
      expect(Array.isArray(config.addons)).toBe(true);
      expect(config.addons).toContain('@storybook/addon-links');
    });

    it('should have @storybook/addon-essentials in addons', () => {
      expect(config.addons).toContain('@storybook/addon-essentials');
    });

    it('should have @chromatic-com/storybook addon for visual testing', () => {
      expect(config.addons).toContain('@chromatic-com/storybook');
    });

    it('should use @storybook/react-vite as the framework', () => {
      expect(config.framework).toBeDefined();
      expect(config.framework.name).toBe('@storybook/react-vite');
    });

    it('should have framework options as an object', () => {
      expect(typeof config.framework.options).toBe('object');
    });

    it('should configure react-docgen-typescript', () => {
      expect(config.typescript).toBeDefined();
      expect(config.typescript.reactDocgen).toBe('react-docgen-typescript');
    });

    it('should configure reactDocgenTypescriptOptions with propFilter', () => {
      expect(config.typescript.reactDocgenTypescriptOptions).toBeDefined();
      expect(config.typescript.reactDocgenTypescriptOptions.shouldExtractLiteralValuesFromEnum).toBe(true);
      expect(typeof config.typescript.reactDocgenTypescriptOptions.propFilter).toBe('function');
    });

    it('should have type checking disabled (check: false)', () => {
      expect(config.typescript.check).toBe(false);
    });

    it('should reference ./tsconfig.storybook.json', () => {
      expect(config.typescript.tsconfigPath).toBe('./tsconfig.storybook.json');
    });

    it('should configure autodocs with "tag"', () => {
      expect(config.docs).toBeDefined();
      expect(config.docs.autodocs).toBe('tag');
    });

    it('should have a viteFinal function', () => {
      expect(typeof config.viteFinal).toBe('function');
    });

    it('viteFinal should remove vite-plugin-dts from plugins', async () => {
      const mockConfig = {
        plugins: [
          { name: 'vite:dts' },
          { name: 'vite-plugin-dts' },
          { name: 'vite:react' },
          { name: 'other-plugin' },
        ],
        esbuild: {},
      };
      const result = await config.viteFinal(mockConfig);
      expect(result.plugins).toBeDefined();
      expect(result.plugins.length).toBe(2);
      expect(result.plugins.map((p: any) => p.name)).toContain('vite:react');
      expect(result.plugins.map((p: any) => p.name)).toContain('other-plugin');
      expect(result.plugins.map((p: any) => p.name)).not.toContain('vite:dts');
      expect(result.plugins.map((p: any) => p.name)).not.toContain('vite-plugin-dts');
    });

    it('viteFinal should handle undefined plugins gracefully', async () => {
      const mockConfig = { plugins: undefined, esbuild: {} };
      const result = await config.viteFinal(mockConfig);
      expect(result.plugins).toEqual([]);
    });

    it('viteFinal should set esbuild with rootDir "."', async () => {
      const mockConfig = { plugins: [], esbuild: {} };
      const result = await config.viteFinal(mockConfig);
      expect(result.esbuild).toBeDefined();
      const tsconfigRaw = JSON.parse(result.esbuild.tsconfigRaw);
      expect(tsconfigRaw.compilerOptions.rootDir).toBe('.');
    });

    it('viteFinal should preserve other config properties', async () => {
      const mockConfig = { plugins: [], esbuild: {}, define: { 'process.env.NODE_ENV': '"test"' } };
      const result = await config.viteFinal(mockConfig);
      expect(result.define).toEqual({ 'process.env.NODE_ENV': '"test"' });
    });
  });

  // ---------------------------------------------------------------------------
  // preview.tsx Configuration
  // ---------------------------------------------------------------------------
  describe('preview.tsx configuration', async () => {
    let preview: any;

    beforeAll(async () => {
      const previewModule = await import('../../.storybook/preview');
      preview = previewModule.default;
    });

    it('should export a default preview object', () => {
      expect(preview).toBeDefined();
      expect(typeof preview).toBe('object');
    });

    it('should have parameters defined', () => {
      expect(preview.parameters).toBeDefined();
    });

    it('should have controls configuration with color and date matchers', () => {
      expect(preview.parameters.controls).toBeDefined();
      expect(preview.parameters.controls.matchers.color).toBeInstanceOf(RegExp);
      expect(preview.parameters.controls.matchers.date).toBeInstanceOf(RegExp);
    });

    it('should have color matcher matching background and color', () => {
      const colorMatcher = preview.parameters.controls.matchers.color;
      expect(colorMatcher.test('background')).toBe(true);
      expect(colorMatcher.test('color')).toBe(true);
      expect(colorMatcher.test('backgroundColor')).toBe(true);
      // The regex (background|color)$/i matches any property containing "background" or "color"
      // including "fontColor" — this is Storybook's standard matcher behavior
      expect(colorMatcher.test('fontColor')).toBe(true);
      expect(colorMatcher.test('margin')).toBe(false);
    });

    it('should have date matcher matching Date', () => {
      const dateMatcher = preview.parameters.controls.matchers.date;
      expect(dateMatcher.test('Date')).toBe(true);
      expect(dateMatcher.test('createdAt')).toBe(false);
    });

    it('should have backgrounds configuration with default "light"', () => {
      expect(preview.parameters.backgrounds).toBeDefined();
      expect(preview.parameters.backgrounds.default).toBe('light');
    });

    it('should have both light and dark background values', () => {
      const backgrounds = preview.parameters.backgrounds.values;
      expect(Array.isArray(backgrounds)).toBe(true);
      expect(backgrounds.length).toBe(2);
      expect(backgrounds).toContainEqual({ name: 'light', value: '#ffffff' });
      expect(backgrounds).toContainEqual({ name: 'dark', value: '#1e1e1e' });
    });

    it('should have decorators array', () => {
      expect(Array.isArray(preview.decorators)).toBe(true);
      expect(preview.decorators.length).toBeGreaterThan(0);
    });

    it('should have a decorator function that wraps stories', () => {
      const decorator = preview.decorators[0];
      expect(typeof decorator).toBe('function');
    });

    it('should have tags containing autodocs', () => {
      expect(Array.isArray(preview.tags)).toBe(true);
      expect(preview.tags).toContain('autodocs');
    });
  });

  // ---------------------------------------------------------------------------
  // Supporting Files
  // ---------------------------------------------------------------------------
  describe('Supporting files', () => {
    it('should have tokens.css in .storybook/', () => {
      expect(fs.existsSync(path.join(STORYBOOK_DIR, 'tokens.css'))).toBe(true);
    });

    it('tokens.css should contain CSS custom properties', () => {
      const content = fs.readFileSync(path.join(STORYBOOK_DIR, 'tokens.css'), 'utf-8');
      expect(content).toContain(':root');
      expect(content).toContain('--');
    });

    it('should have MockAdapter in mocks directory', () => {
      expect(fs.existsSync(path.join(STORYBOOK_DIR, 'mocks/MockAdapter.ts'))).toBe(true);
    });

    it('should have fixtures.ts in mocks directory', () => {
      expect(fs.existsSync(path.join(STORYBOOK_DIR, 'mocks/fixtures.ts'))).toBe(true);
    });

    it('should have all CSS files referenced in preview.tsx', () => {
      const referencedCssFiles = [
        'ChatPanel.css',
        'CommandInput.css',
        'CommandPalette.css',
        'ContextMenu.css',
        'Editor.css',
        'FileTree.css',
        'GitPanel.css',
        'LiveLog.css',
        'Notification.css',
        'NotificationStack.css',
        'QueuedMessagesPanel.css',
        'Sidebar.css',
        'StatusBar.css',
        'Terminal.css',
        'TerminalTabBar.css',
      ];

      for (const cssFile of referencedCssFiles) {
        const cssPath = path.join(UI_ROOT, 'src', 'components', cssFile);
        expect(fs.existsSync(cssPath)).toBe(true);
      }
    });
  });

  // ---------------------------------------------------------------------------
  // MockAdapter Interface
  // ---------------------------------------------------------------------------
  describe('MockAdapter interface', async () => {
    let MockAdapter: any;

    beforeAll(async () => {
      const mocks = await import('../../.storybook/mocks/MockAdapter');
      MockAdapter = mocks.MockAdapter;
    });

    it('should export MockAdapter as a class', () => {
      expect(typeof MockAdapter).toBe('function');
    });

    it('should instantiate MockAdapter successfully', () => {
      const adapter = new MockAdapter();
      expect(adapter).toBeDefined();
    });

    it('should have the expected name property', () => {
      const adapter = new MockAdapter();
      expect(adapter.name).toBe('MockAdapter');
    });

    it('should have requiresBackendHealthCheck set to false', () => {
      const adapter = new MockAdapter();
      expect(adapter.requiresBackendHealthCheck).toBe(false);
    });

    it('should have fileOpsViaAPI set to false', () => {
      const adapter = new MockAdapter();
      expect(adapter.fileOpsViaAPI).toBe(false);
    });

    it('should have showOnboarding set to false', () => {
      const adapter = new MockAdapter();
      expect(adapter.showOnboarding).toBe(false);
    });

    it('should have supportsSSH set to false', () => {
      const adapter = new MockAdapter();
      expect(adapter.supportsSSH).toBe(false);
    });

    it('should have supportsInstances set to false', () => {
      const adapter = new MockAdapter();
      expect(adapter.supportsInstances).toBe(false);
    });

    it('should have supportsLocalTerminal set to false', () => {
      const adapter = new MockAdapter();
      expect(adapter.supportsLocalTerminal).toBe(false);
    });

    it('should have supportsSettings set to true', () => {
      const adapter = new MockAdapter();
      expect(adapter.supportsSettings).toBe(true);
    });

    it('should have platformNavItems with tasks and billing', () => {
      const adapter = new MockAdapter();
      expect(Array.isArray(adapter.platformNavItems)).toBe(true);
      expect(adapter.platformNavItems.length).toBe(2);
      expect(adapter.platformNavItems[0].id).toBe('tasks');
      expect(adapter.platformNavItems[1].id).toBe('billing');
    });

    it('should have fetch method', () => {
      const adapter = new MockAdapter();
      expect(typeof adapter.fetch).toBe('function');
    });

    it('should have getWebSocketURL method', () => {
      const adapter = new MockAdapter();
      expect(typeof adapter.getWebSocketURL).toBe('function');
    });

    it('getWebSocketURL should return null', () => {
      const adapter = new MockAdapter();
      expect(adapter.getWebSocketURL()).toBeNull();
    });

    it('should return mock workspace data for /api/workspace', async () => {
      const adapter = new MockAdapter();
      const response = await adapter.fetch('http://localhost/api/workspace');
      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.id).toBe('mock-workspace');
      expect(data.name).toBe('Demo Workspace');
    });

    it('should return mock health data for /api/health', async () => {
      const adapter = new MockAdapter();
      const response = await adapter.fetch('http://localhost/api/health');
      expect(response.status).toBe(200);
      const data = await response.json();
      expect(data.status).toBe('ok');
    });

    it('should return mock file listing for /api/files', async () => {
      const adapter = new MockAdapter();
      const response = await adapter.fetch('http://localhost/api/files');
      expect(response.status).toBe(200);
      const data = await response.json();
      expect(Array.isArray(data)).toBe(true);
      expect(data.length).toBeGreaterThan(0);
    });

    it('should return 404 for unknown endpoints', async () => {
      const adapter = new MockAdapter();
      const response = await adapter.fetch('http://localhost/api/unknown-endpoint');
      expect(response.status).toBe(404);
      const data = await response.json();
      expect(data.error).toBe('Not found');
    });
  });

  // ---------------------------------------------------------------------------
  // Package.json Scripts
  // ---------------------------------------------------------------------------
  describe('package.json scripts', () => {
    let pkg: any;

    beforeAll(() => {
      const pkgPath = path.join(UI_ROOT, 'package.json');
      const content = fs.readFileSync(pkgPath, 'utf-8');
      pkg = JSON.parse(content);
    });

    it('should have "storybook" script containing "storybook dev"', () => {
      expect(pkg.scripts.storybook).toBeDefined();
      expect(pkg.scripts.storybook).toContain('storybook dev');
    });

    it('should have "build-storybook" script containing "storybook build"', () => {
      expect(pkg.scripts['build-storybook']).toBeDefined();
      expect(pkg.scripts['build-storybook']).toContain('storybook build');
    });

    it('should have "test" script using vitest', () => {
      expect(pkg.scripts.test).toBeDefined();
      expect(pkg.scripts.test).toContain('vitest');
    });
  });

  // ---------------------------------------------------------------------------
  // Package.json Dependencies
  // ---------------------------------------------------------------------------
  describe('package.json Storybook dependencies', () => {
    let pkg: any;

    beforeAll(() => {
      const pkgPath = path.join(UI_ROOT, 'package.json');
      const content = fs.readFileSync(pkgPath, 'utf-8');
      pkg = JSON.parse(content);
    });

    it('should have storybook in devDependencies', () => {
      expect(pkg.devDependencies.storybook).toBeDefined();
    });

    it('should have @storybook/react in devDependencies', () => {
      expect(pkg.devDependencies['@storybook/react']).toBeDefined();
    });

    it('should have @storybook/react-vite in devDependencies', () => {
      expect(pkg.devDependencies['@storybook/react-vite']).toBeDefined();
    });

    it('should have @storybook/addon-essentials in devDependencies', () => {
      expect(pkg.devDependencies['@storybook/addon-essentials']).toBeDefined();
    });

    it('should have @storybook/addon-links in devDependencies', () => {
      expect(pkg.devDependencies['@storybook/addon-links']).toBeDefined();
    });

    it('should have @storybook/test in devDependencies', () => {
      expect(pkg.devDependencies['@storybook/test']).toBeDefined();
    });

    it('should use Storybook 8.x versions', () => {
      const storybookVersion = pkg.devDependencies.storybook;
      expect(storybookVersion).toMatch(/^[\^~]8\./);
    });

    it('should use @storybook/react 8.x versions', () => {
      const reactVersion = pkg.devDependencies['@storybook/react'];
      expect(reactVersion).toMatch(/^[\^~]8\./);
    });

    it('should use @storybook/react-vite 8.x versions', () => {
      const viteVersion = pkg.devDependencies['@storybook/react-vite'];
      expect(viteVersion).toMatch(/^[\^~]8\./);
    });

    it('should use @storybook/addon-essentials 8.x versions', () => {
      const essentialsVersion = pkg.devDependencies['@storybook/addon-essentials'];
      expect(essentialsVersion).toMatch(/^[\^~]8\./);
    });

    it('should use @storybook/addon-links 8.x versions', () => {
      const linksVersion = pkg.devDependencies['@storybook/addon-links'];
      expect(linksVersion).toMatch(/^[\^~]8\./);
    });

    it('should use @storybook/test 8.x versions', () => {
      const testVersion = pkg.devDependencies['@storybook/test'];
      expect(testVersion).toMatch(/^[\^~]8\./);
    });

    it('should have @chromatic-com/storybook in devDependencies', () => {
      expect(pkg.devDependencies['@chromatic-com/storybook']).toBeDefined();
    });
  });

  // ---------------------------------------------------------------------------
  // tsconfig.storybook.json
  // ---------------------------------------------------------------------------
  describe('tsconfig.storybook.json', () => {
    let tsconfig: any;

    beforeAll(() => {
      const tsconfigPath = path.join(UI_ROOT, 'tsconfig.storybook.json');
      const content = fs.readFileSync(tsconfigPath, 'utf-8');
      tsconfig = JSON.parse(content);
    });

    it('should be a valid JSON file', () => {
      expect(tsconfig).toBeDefined();
      expect(typeof tsconfig).toBe('object');
    });

    it('should have compilerOptions', () => {
      expect(tsconfig.compilerOptions).toBeDefined();
    });

    it('should use react-jsx jsx mode', () => {
      expect(tsconfig.compilerOptions.jsx).toBe('react-jsx');
    });

    it('should have rootDir set to "."', () => {
      expect(tsconfig.compilerOptions.rootDir).toBe('.');
    });

    it('should enable esModuleInterop', () => {
      expect(tsconfig.compilerOptions.esModuleInterop).toBe(true);
    });

    it('should enable strict mode', () => {
      expect(tsconfig.compilerOptions.strict).toBe(true);
    });

    it('should include both src and .storybook directories', () => {
      expect(Array.isArray(tsconfig.include)).toBe(true);
      expect(tsconfig.include).toContain('src');
      expect(tsconfig.include).toContain('.storybook');
    });

    it('should exclude node_modules and dist', () => {
      expect(Array.isArray(tsconfig.exclude)).toBe(true);
      expect(tsconfig.exclude).toContain('node_modules');
      expect(tsconfig.exclude).toContain('dist');
    });

    it('should have noEmit enabled', () => {
      expect(tsconfig.compilerOptions.noEmit).toBe(true);
    });
  });
});
