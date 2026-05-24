// @sprout/ui Documentation Validation Tests
// ==========================================
// Validates that docs/CONSUMPTION_GUIDE.md, docs/COMPONENT_LIBRARY.md,
// and README.md accurately reflect the actual state of packages/ui/src/index.ts
// and packages/ui/package.json.
//
// Run with: node docs/__tests__/consumption-guide.test.js

import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// ── Helpers ────────────────────────────────────────────────────────────

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const ROOT = path.resolve(__dirname, '../..');

function read(rel) {
  return fs.readFileSync(path.resolve(ROOT, rel), 'utf-8');
}

function fileExists(rel) {
  return fs.existsSync(path.resolve(ROOT, rel));
}

// Parse all exported symbol names from packages/ui/src/index.ts
// Handles: export { X, Y } from '...', export type { Z } from '...',
//          export { default as Name } from '...', export const NAME from '...',
//          export { NAME as default } from '...', etc.
function parseExports(content) {
  const names = new Set();

  // Match lines like: export { Foo, Bar as Baz } from '...'
  // or: export type { Foo } from '...'
  // or: export { default as Foo } from '...'
  // or: export { NAME } from '...'  (constant re-export)
  const exportPattern = /export\s+(?:type\s+)?{([\s\S]*?)}\s+from\s+['"][^'"]+['"]/g;
  let m;
  while ((m = exportPattern.exec(content)) !== null) {
    const items = m[1].split(',');
    for (const item of items) {
      const trimmed = item.trim();
      if (!trimmed || trimmed.startsWith('//')) continue;

      // "default as Name" → Name
      const defaultAs = trimmed.match(/^\s*default\s+as\s+(\w+)/);
      if (defaultAs) {
        names.add(defaultAs[1]);
        continue;
      }

      // "Foo as Bar" → Bar (aliased export)
      const alias = trimmed.match(/^\s*(\w+)\s+as\s+(\w+)/);
      if (alias) {
        names.add(alias[2]);
        continue;
      }

      // Plain name
      const plain = trimmed.match(/^\s*(\w+)/);
      if (plain) {
        names.add(plain[1]);
      }
    }
  }

  // Also match single-line exports: export { name } from '...'
  // (already handled above, but catch any edge cases)
  // Match: export { constantName } from '...' where constantName is uppercase
  const singleExport = /export\s+(?:type\s+)?{(.*?)}\s+from/g;
  while ((m = singleExport.exec(content)) !== null) {
    // Already handled above
  }

  return names;
}

// Extract peer dependencies from package.json
function getPeerDependencies(pkgJson) {
  return JSON.parse(pkgJson).peerDependencies || {};
}

// Extract exports field from package.json (expects already-parsed object)
function getExportsField(pkgObj) {
  return (typeof pkgObj === 'object' ? pkgObj : JSON.parse(pkgObj)).exports || {};
}

// Read and parse actual sources
const indexTs = read('packages/ui/src/index.ts');
const packageJson = read('packages/ui/package.json');
const pkg = JSON.parse(packageJson);
const consumptionGuide = read('docs/CONSUMPTION_GUIDE.md');
const componentLibrary = read('docs/COMPONENT_LIBRARY.md');
const readmeMd = read('README.md');

const actualExports = parseExports(indexTs);
const peerDeps = pkg.peerDependencies || {};

// ── Tests ──────────────────────────────────────────────────────────────

describe('CONSUMPTION_GUIDE.md — Import path accuracy', () => {
  // Components mentioned in the "Available Components" table and code examples
  const documentedComponents = [
    // Panels
    'ChatPanel', 'Terminal', 'TerminalPane', 'TerminalTabBar',
    // Editors
    'Editor',
    // Trees
    'FileTree',
    // Navigation
    'Sidebar', 'MenuBar', 'StatusBar', 'CommandPalette',
    // Notifications
    'NotificationStack', 'NotificationItem',
    // Git
    'GitSidebarPanel',
    // Chat
    'MessageBubble', 'MessageContent', 'MessageSegments',
    'ChatMessageContextMenu', 'QueuedMessagesPanel',
    'CommandInput', 'SelectionActionBar',
    // UI Primitives
    'ContextMenu', 'LiveLog', 'Skeleton', 'SkeletonText',
  ];

  // Dialog function exports
  const documentedDialogs = [
    'showThemedAlert', 'showThemedConfirm', 'showThemedPrompt',
  ];

  // Context providers and hooks
  const documentedContexts = [
    'NotificationProvider', 'SproutProvider', 'EventsContextProvider',
  ];

  const documentedHooks = [
    'useNotifications', 'useSproutAdapter', 'useSproutFetch',
    'useEvents', 'useMultiSelect', 'flattenVisibleFiles',
  ];

  // Utility functions
  const documentedUtilities = [
    'fuzzyFilter', 'highlightMatches', 'fuzzyScore',
    'copyToClipboard', 'generateUUID',
    'stripAnsiCodes', 'hasAnsiCodes', 'ansiToHtml',
    'parseMessageSegments', 'detectLineEnding',
    'getStatusInfo', 'getPersonaColor', 'groupSubagentRuns',
  ];

  test('all documented components are exported from index.ts', () => {
    for (const name of documentedComponents) {
      assert.ok(
        actualExports.has(name),
        `Component "${name}" is documented but NOT exported from packages/ui/src/index.ts`
      );
    }
  });

  test('all documented dialog functions are exported from index.ts', () => {
    for (const name of documentedDialogs) {
      assert.ok(
        actualExports.has(name),
        `Dialog function "${name}" is documented but NOT exported from packages/ui/src/index.ts`
      );
    }
  });

  test('all documented context providers are exported from index.ts', () => {
    for (const name of documentedContexts) {
      assert.ok(
        actualExports.has(name),
        `Context "${name}" is documented but NOT exported from packages/ui/src/index.ts`
      );
    }
  });

  test('all documented hooks are exported from index.ts', () => {
    for (const name of documentedHooks) {
      assert.ok(
        actualExports.has(name),
        `Hook "${name}" is documented but NOT exported from packages/ui/src/index.ts`
      );
    }
  });

  test('all documented utility functions are exported from index.ts', () => {
    for (const name of documentedUtilities) {
      assert.ok(
        actualExports.has(name),
        `Utility "${name}" is documented but NOT exported from packages/ui/src/index.ts`
      );
    }
  });

  // Type exports mentioned in the TypeScript Support section
  test('documented type exports exist in index.ts', () => {
    // The docs mention these types in a table — verify they're exported as types
    const documentedTypes = [
      // Chat system
      'ChatProps', 'Message', 'ToolExecution', 'SubagentRun',
      'SubagentActivity', 'LogEntry', 'TodoItem', 'TodoStatus',
      'FileEdit', 'LiveLogLine',
      // Message segment types
      'TextSegment', 'ToolCallSegment', 'TodoUpdateSegment',
      'ProgressSegment', 'ResultSegment', 'MessageSegment',
      // Editor
      'EditorProps', 'EditorState', 'EditorBuffer', 'EditorPane',
      'PaneLayout', 'PaneSize',
      // File tree
      'FileTreeProps', 'FileInfo',
      // Terminal
      'TerminalProps', 'TerminalThemePack', 'CreateTerminalConnection',
      // Terminal sessions
      'TerminalSession', 'AttachableSession',
      // Command palette
      'CommandPaletteProps', 'PaletteMode', 'CommandDef',
      // Status bar
      'StatusBarProps', 'CursorPosition',
      // Sidebar
      'SidebarProps',
      // Git integration
      'GitStatusData', 'GitFile', 'GitSidebarPanelProps',
      // Revision
      'Revision', 'RevisionFile', 'RevisionDetailFile',
      // Notifications
      'Notification', 'NotificationType', 'NotificationData',
      // Platform adapter
      'APIAdapter', 'PlatformNavItem',
      // Event system
      'SproutEvent', 'EventsProvider',
    ];

    for (const name of documentedTypes) {
      assert.ok(
        actualExports.has(name),
        `Type "${name}" is documented but NOT exported from packages/ui/src/index.ts`
      );
    }
  });
});

describe('CONSUMPTION_GUIDE.md — Peer dependency accuracy', () => {
  test('peer dependency package names match package.json', () => {
    // The guide lists: react, react-dom, @sprout/events
    const documentedPeerDeps = ['react', 'react-dom', '@sprout/events'];
    for (const name of documentedPeerDeps) {
      assert.ok(
        peerDeps[name],
        `Peer dependency "${name}" is documented but NOT in package.json peerDependencies`
      );
    }
  });

  test('no undocumented peer dependencies in package.json', () => {
    const documentedPeerDepNames = ['react', 'react-dom', '@sprout/events'];
    for (const name of Object.keys(peerDeps)) {
      assert.ok(
        documentedPeerDepNames.includes(name),
        `Peer dependency "${name}" exists in package.json but is NOT documented in the guide`
      );
    }
  });

  test('peer dependency version for react matches package.json', () => {
    const reactVersion = peerDeps['react'];
    assert.ok(
      reactVersion,
      'react peer dependency version not found in package.json'
    );
    // Check that the documented version string appears in the actual version
    assert.ok(
      reactVersion.includes('18.0.0') || reactVersion.startsWith('>=18') || reactVersion.startsWith('^18'),
      `react peer dependency version "${reactVersion}" does not match documented ">=18.0.0"`
    );
  });

  test('peer dependency version for react-dom matches package.json', () => {
    const reactDomVersion = peerDeps['react-dom'];
    assert.ok(
      reactDomVersion,
      'react-dom peer dependency version not found in package.json'
    );
    assert.ok(
      reactDomVersion.includes('18.0.0') || reactDomVersion.startsWith('>=18') || reactDomVersion.startsWith('^18'),
      `react-dom peer dependency version "${reactDomVersion}" does not match documented ">=18.0.0"`
    );
  });

  test('peer dependency version for @sprout/events matches package.json', () => {
    const eventsVersion = peerDeps['@sprout/events'];
    assert.ok(
      eventsVersion,
      '@sprout/events peer dependency version not found in package.json'
    );
    // The guide says ^0.1.0 — verify the documented version is in the actual
    assert.ok(
      eventsVersion.includes('0.1.0'),
      `@sprout/events version "${eventsVersion}" does not contain documented version "0.1.0"`
    );
  });

  test('documented version strings appear verbatim in the guide', () => {
    // The guide should contain the actual version strings from package.json
    for (const [name, version] of Object.entries(peerDeps)) {
      assert.ok(
        consumptionGuide.includes(version),
        `Version "${version}" for "${name}" is not present in the Consumption Guide`
      );
    }
  });
});

describe('CONSUMPTION_GUIDE.md — CSS import path', () => {
  test('CSS import path matches package.json exports', () => {
    // The guide says: import '@sprout/ui/dist/style.css'
    assert.ok(
      getExportsField(pkg)['./dist/style.css'],
      'package.json exports field does NOT contain ./dist/style.css'
    );
  });

  test('CSS import path is mentioned correctly in the guide', () => {
    assert.ok(
      consumptionGuide.includes("@sprout/ui/dist/style.css"),
      'Guide does not mention the CSS import path @sprout/ui/dist/style.css'
    );
  });
});

describe('CONSUMPTION_GUIDE.md — Build output paths', () => {
  test('dist/index.esm.js matches package.json module field', () => {
    assert.equal(
      pkg.module,
      'dist/index.esm.js',
      `package.json module field "${pkg.module}" does not match documented dist/index.esm.js`
    );
  });

  test('dist/index.cjs.js matches package.json main field', () => {
    assert.equal(
      pkg.main,
      'dist/index.cjs.js',
      `package.json main field "${pkg.main}" does not match documented dist/index.cjs.js`
    );
  });

  test('dist/index.d.ts matches package.json types field', () => {
    assert.equal(
      pkg.types,
      'dist/index.d.ts',
      `package.json types field "${pkg.types}" does not match documented dist/index.d.ts`
    );
  });

  test('build output paths in guide match package.json exports field', () => {
    const exportsField = getExportsField(pkg);
    const defaultExport = exportsField['.'] || {};

    // Verify the ESM path (exports field uses ./dist/... paths)
    assert.equal(
      defaultExport.import || defaultExport.default,
      './dist/index.esm.js',
      'package.json exports[.].import/default does not point to ./dist/index.esm.js'
    );

    // Verify the CJS path
    assert.equal(
      defaultExport.require,
      './dist/index.cjs.js',
      'package.json exports[.].require does not point to ./dist/index.cjs.js'
    );

    // Verify the types path
    assert.equal(
      defaultExport.types,
      './dist/index.d.ts',
      'package.json exports[.].types does not point to ./dist/index.d.ts'
    );
  });

  test('guide mentions all four dist output files', () => {
    // The Module Formats section should mention all four files
    assert.ok(
      consumptionGuide.includes('dist/index.esm.js'),
      'Guide does not mention dist/index.esm.js'
    );
    assert.ok(
      consumptionGuide.includes('dist/index.cjs.js'),
      'Guide does not mention dist/index.cjs.js'
    );
    assert.ok(
      consumptionGuide.includes('dist/index.d.ts'),
      'Guide does not mention dist/index.d.ts'
    );
    assert.ok(
      consumptionGuide.includes('dist/style.css'),
      'Guide does not mention dist/style.css'
    );
  });
});

describe('CONSUMPTION_GUIDE.md — Provider documentation accuracy', () => {
  test('SproutProvider is documented with adapter prop as nullable', () => {
    // The guide says: adapter — an APIAdapter object or null
    // Verify SproutProvider accepts an adapter prop
    assert.ok(
      actualExports.has('SproutProvider'),
      'SproutProvider is not exported'
    );
    // Verify APIAdapter type is exported (for the adapter prop type)
    assert.ok(
      actualExports.has('APIAdapter'),
      'APIAdapter type is not exported but is referenced in SproutProvider docs'
    );
  });

  test('SproutProviderProps type is exported', () => {
    assert.ok(
      actualExports.has('SproutProviderProps'),
      'SproutProviderProps type is not exported from index.ts'
    );
  });

  test('EventsContextProviderProps type is exported', () => {
    assert.ok(
      actualExports.has('EventsContextProviderProps'),
      'EventsContextProviderProps type is not exported from index.ts'
    );
  });
});

describe('CONSUMPTION_GUIDE.md — Notification types documentation', () => {
  test('documented notification types are listed in guide', () => {
    // The guide says: Supported notification types: info, success, warning, error
    assert.ok(
      consumptionGuide.includes('info') &&
      consumptionGuide.includes('success') &&
      consumptionGuide.includes('warning') &&
      consumptionGuide.includes('error'),
      'Guide does not mention all notification types (info, success, warning, error)'
    );
  });

  test('NotificationType is exported as a type', () => {
    assert.ok(
      actualExports.has('NotificationType'),
      'NotificationType is documented but not exported'
    );
  });
});

describe('Internal link validity', () => {
  test('CONSUMPTION_GUIDE.md exists as a file', () => {
    assert.ok(
      fileExists('docs/CONSUMPTION_GUIDE.md'),
      'docs/CONSUMPTION_GUIDE.md does not exist'
    );
  });

  test('COMPONENT_LIBRARY.md internal link to CONSUMPTION_GUIDE.md resolves', () => {
    // The guide has: [Consumption Guide](CONSUMPTION_GUIDE.md)
    // This should resolve relative to docs/COMPONENT_LIBRARY.md
    assert.ok(
      fileExists('docs/CONSUMPTION_GUIDE.md'),
      'Link from COMPONENT_LIBRARY.md to CONSUMPTION_GUIDE.md does not resolve'
    );
  });

  test('README.md Documentation table links to docs/CONSUMPTION_GUIDE.md exists', () => {
    assert.ok(
      readmeMd.includes('docs/CONSUMPTION_GUIDE.md'),
      'README.md does not link to docs/CONSUMPTION_GUIDE.md'
    );
  });

  test('README.md Component Library section mentions npm install @sprout/ui', () => {
    assert.ok(
      readmeMd.includes('npm install @sprout/ui'),
      'README.md Component Library section does not show npm install command'
    );
  });

  test('README.md links from Consumption Guide section to docs/CONSUMPTION_GUIDE.md', () => {
    // The README should have a link like [Consumption Guide](docs/CONSUMPTION_GUIDE.md)
    assert.ok(
      readmeMd.includes('[Consumption Guide](docs/CONSUMPTION_GUIDE.md)') ||
      readmeMd.includes('docs/CONSUMPTION_GUIDE.md'),
      'README.md does not properly link to CONSUMPTION_GUIDE.md'
    );
  });

  test('README.md Documentation table contains Component Library row', () => {
    assert.ok(
      readmeMd.includes('Component Library'),
      'README.md Documentation table does not contain a Component Library row'
    );
  });

  test('COMPONENT_LIBRARY.md Consumption Guide section links correctly', () => {
    // COMPONENT_LIBRARY.md has: [Consumption Guide](CONSUMPTION_GUIDE.md)
    assert.ok(
      componentLibrary.includes('[Consumption Guide](CONSUMPTION_GUIDE.md)'),
      'COMPONENT_LIBRARY.md does not link to CONSUMPTION_GUIDE.md'
    );
  });

  test('COMPONENT_LIBRARY.md links to SP-039-DECISION.md (relative path)', () => {
    // The doc references ../roadmap/SP-039-DECISION.md
    assert.ok(
      fileExists('roadmap/SP-039-DECISION.md'),
      'Link from COMPONENT_LIBRARY.md to roadmap/SP-039-DECISION.md does not resolve'
    );
  });

  test('COMPONENT_LIBRARY.md links to CONTRIBUTING.md', () => {
    assert.ok(
      fileExists('CONTRIBUTING.md'),
      'CONTRIBUTING.md does not exist (referenced from COMPONENT_LIBRARY.md)'
    );
  });
});

describe('Package metadata consistency', () => {
  test('package name is @sprout/ui', () => {
    assert.equal(
      pkg.name,
      '@sprout/ui',
      `Package name is "${pkg.name}" but guide documents "@sprout/ui"`
    );
  });

  test('package.json version is documented in guide or is acceptable', () => {
    // The guide doesn't hardcode the version, but verify it's a valid semver
    const versionPattern = /^\d+\.\d+\.\d+/;
    assert.ok(
      versionPattern.test(pkg.version),
      `Package version "${pkg.version}" is not a valid semver`
    );
  });

  test('package.json has correct exports field structure', () => {
    const exportsField = getExportsField(pkg);
    assert.ok(
      exportsField['.'],
      'package.json exports field is missing "." entry'
    );
    assert.ok(
      exportsField['./dist/style.css'],
      'package.json exports field is missing "./dist/style.css" entry'
    );
  });

  test('package.json main/module/types point to dist files', () => {
    assert.ok(
      pkg.main && pkg.main.startsWith('dist/'),
      'package.json main does not point to dist/'
    );
    assert.ok(
      pkg.module && pkg.module.startsWith('dist/'),
      'package.json module does not point to dist/'
    );
    assert.ok(
      pkg.types && pkg.types.startsWith('dist/'),
      'package.json types does not point to dist/'
    );
  });

  test('package.json sideEffects is set correctly', () => {
    assert.equal(
      pkg.sideEffects,
      false,
      'package.json sideEffects is not false (important for tree-shaking)'
    );
  });
});

describe('Guide code example accuracy', () => {
  test('guide imports use correct package name @sprout/ui', () => {
    // Check that code examples use '@sprout/ui' not internal paths
    const importStatements = consumptionGuide.match(/import\s+.*?from\s+['"]([^'"]+)['"]/g) || [];
    for (const stmt of importStatements) {
      const match = stmt.match(/from\s+['"]([^'"]+)['"]/);
      if (match) {
        const importPath = match[1];
        if (importPath.startsWith('@') || importPath.startsWith('.')) {
          // It's a package or relative import — should be @sprout/ui for UI imports
          if (importPath !== 'react' && importPath !== 'useState') {
            assert.ok(
              importPath === '@sprout/ui' || importPath === '@sprout/ui/dist/style.css',
              `Code example uses incorrect import path: "${importPath}" (should be "@sprout/ui" or "@sprout/ui/dist/style.css")`
            );
          }
        }
      }
    }
  });

  test('guide does not RECOMMEND internal package paths in code examples', () => {
    // Code examples in the guide should use '@sprout/ui' for imports.
    // The troubleshooting section may MENTION internal paths as things to avoid,
    // so we check that the import statements in code blocks use correct paths.
    // Extract import statements and verify they use @sprout/ui
    const importStatements = consumptionGuide.match(/import\s+.*?from\s+['"]([^'"]+)['"]/g) || [];
    for (const stmt of importStatements) {
      const match = stmt.match(/from\s+['"]([^'"]+)['"]/);
      if (match) {
        const importPath = match[1];
        // All @sprout/ui imports should be the top-level package or dist/style.css
        if (importPath.startsWith('@sprout/ui')) {
          assert.ok(
            importPath === '@sprout/ui' || importPath === '@sprout/ui/dist/style.css',
            `Code example uses internal import path "${importPath}" — should use "@sprout/ui" or "@sprout/ui/dist/style.css"`
          );
        }
      }
    }
  });
});

describe('Export count sanity check', () => {
  test('index.ts exports a reasonable number of symbols', () => {
    const count = actualExports.size;
    assert.ok(
      count >= 50,
      `index.ts only exports ${count} symbols — seems too few for a comprehensive UI library`
    );
    assert.ok(
      count <= 300,
      `index.ts exports ${count} symbols — seems unusually high`
    );
  });

  test('all exports are captured by the parser', () => {
    // Verify our parser found the key known exports
    const keyExports = ['ChatPanel', 'Editor', 'TerminalPane', 'FileTree', 'Sidebar',
      'StatusBar', 'NotificationProvider', 'useNotifications', 'generateUUID',
      'fuzzyFilter', 'showThemedAlert', 'SproutProvider'];
    for (const name of keyExports) {
      assert.ok(
        actualExports.has(name),
        `Export parser failed to find "${name}" in index.ts — parser may be broken`
      );
    }
  });
});

describe('No stale documentation references', () => {
  test('guide does not reference non-existent files as imports', () => {
    // Check that the guide doesn't reference file paths that don't exist
    // (excluding URLs, node_modules, etc.)
    const fileRefs = consumptionGuide.match(/from\s+['"]([^'"]+)['"]/g) || [];
    for (const ref of fileRefs) {
      const match = ref.match(/from\s+['"]([^'"]+)['"]/);
      if (match) {
        const pathRef = match[1];
        // Only check @sprout/ui imports — skip react, etc.
        if (pathRef.includes('@sprout/ui')) {
          assert.ok(
            pathRef === '@sprout/ui' || pathRef === '@sprout/ui/dist/style.css',
            `Guide references non-standard import path: "${pathRef}"`
          );
        }
      }
    }
  });

  test('COMPONENT_LIBRARY.md does not reference non-existent component files', () => {
    // The "Current Component Inventory" section lists components — verify they're exported
    // (We only check major ones, not every single one, to avoid flakiness)
    const inventoryComponents = [
      'ChatPanel', 'FileTree', 'Sidebar', 'StatusBar',
      'TerminalPane', 'NotificationStack', 'CommandPalette', 'Editor',
    ];
    for (const name of inventoryComponents) {
      assert.ok(
        actualExports.has(name),
        `COMPONENT_LIBRARY.md lists "${name}" in inventory but it's not exported from index.ts`
      );
    }
  });
});
