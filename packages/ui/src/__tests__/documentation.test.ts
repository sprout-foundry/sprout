import { describe, it, expect, beforeAll } from 'vitest';
import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const README_PATH = resolve(__dirname, '../../README.md');
const CHANGELOG_PATH = resolve(__dirname, '../../CHANGELOG.md');
const INDEX_PATH = resolve(__dirname, '../index.ts');

let readmeContent: string;
let changelogContent: string;
let indexContent: string;

beforeAll(() => {
  readmeContent = readFileSync(README_PATH, 'utf-8');
  changelogContent = readFileSync(CHANGELOG_PATH, 'utf-8');
  indexContent = readFileSync(INDEX_PATH, 'utf-8');
});

describe('Documentation files', () => {
  // ── README.md ──────────────────────────────────────────────────────
  describe('README.md', () => {
    it('exists and has content', () => {
      expect(readmeContent).toBeDefined();
      expect(readmeContent.length).toBeGreaterThan(100);
    });

    it('contains the package name @sprout/ui', () => {
      expect(readmeContent).toContain('@sprout/ui');
    });

    it('contains installation instructions', () => {
      expect(readmeContent).toContain('npm install');
    });

    it('mentions peer dependencies', () => {
      expect(readmeContent).toContain('Peer Dependencies');
      expect(readmeContent).toContain('react');
      expect(readmeContent).toContain('react-dom');
      expect(readmeContent).toContain('@sprout/events');
    });

    it('contains CSS import guidance', () => {
      expect(readmeContent).toContain('style.css');
    });

    it('contains usage example sections', () => {
      expect(readmeContent).toContain('## Usage');
    });

    it('has a Chat Panel usage example', () => {
      expect(readmeContent).toContain('Chat Panel');
      expect(readmeContent).toContain('ChatPanel');
    });

    it('has a Terminal usage example', () => {
      expect(readmeContent).toContain('TerminalPane');
      expect(readmeContent).toContain('TerminalTabBar');
    });

    it('has a Notifications usage example', () => {
      expect(readmeContent).toContain('NotificationProvider');
      expect(readmeContent).toContain('useNotifications');
    });

    it('has a Fuzzy Search usage example', () => {
      expect(readmeContent).toContain('fuzzyFilter');
      expect(readmeContent).toContain('highlightMatches');
    });

    it('mentions key components: ChatPanel', () => {
      expect(readmeContent).toContain('`ChatPanel`');
    });

    it('mentions key components: Terminal', () => {
      expect(readmeContent).toContain('`Terminal`');
    });

    it('mentions key components: Editor', () => {
      expect(readmeContent).toContain('`Editor`');
    });

    it('mentions key components: FileTree', () => {
      expect(readmeContent).toContain('`FileTree`');
    });

    it('mentions key components: CommandPalette', () => {
      expect(readmeContent).toContain('`CommandPalette`');
    });

    it('contains build and development instructions', () => {
      expect(readmeContent).toContain('Build & Development');
      expect(readmeContent).toContain('npm run build');
      expect(readmeContent).toContain('npm run test');
      expect(readmeContent).toContain('npm run type-check');
      expect(readmeContent).toContain('npm run storybook');
      expect(readmeContent).toContain('npm run build-storybook');
    });

    it('references the parent Sprout project', () => {
      expect(readmeContent).toContain('sprout-foundry/sprout');
    });

    it('has proper markdown structure with headings', () => {
      expect(readmeContent).toContain('# @sprout/ui');
      expect(readmeContent).toContain('## Installation');
      expect(readmeContent).toContain('## Usage');
      expect(readmeContent).toContain('## Components');
      expect(readmeContent).toContain('## Contexts');
      expect(readmeContent).toContain('## Hooks');
      expect(readmeContent).toContain('## Utilities');
      expect(readmeContent).toContain('## Types');
      expect(readmeContent).toContain('## Build & Development');
      expect(readmeContent).toContain('## Project Structure');
      expect(readmeContent).toContain('## License');
    });

    it('has proper markdown structure with code blocks', () => {
      // Count triple-backtick code blocks
      const codeBlockCount = (readmeContent.match(/```/g) || []).length;
      expect(codeBlockCount).toBeGreaterThan(2);
    });

    it('has proper markdown structure with tables', () => {
      expect(readmeContent).toContain('|---');
    });

    it('documents the Dialogs section', () => {
      expect(readmeContent).toContain('### Dialogs');
      expect(readmeContent).toContain('showThemedAlert');
      expect(readmeContent).toContain('showThemedConfirm');
      expect(readmeContent).toContain('showThemedPrompt');
    });

    it('documents the Contexts', () => {
      expect(readmeContent).toContain('NotificationProvider');
      expect(readmeContent).toContain('SproutProvider');
      expect(readmeContent).toContain('EventsContextProvider');
    });

    it('documents the Hooks', () => {
      expect(readmeContent).toContain('useMultiSelect');
      expect(readmeContent).toContain('flattenVisibleFiles');
    });

    it('documents utility functions', () => {
      expect(readmeContent).toContain('generateUUID()');
      expect(readmeContent).toContain('copyToClipboard()');
      expect(readmeContent).toContain('fuzzyScore()');
      expect(readmeContent).toContain('fuzzyFilter()');
      expect(readmeContent).toContain('highlightMatches()');
      expect(readmeContent).toContain('stripAnsiCodes()');
      expect(readmeContent).toContain('ansiToHtml()');
      expect(readmeContent).toContain('debugLog()');
      expect(readmeContent).toContain('getStatusInfo()');
      expect(readmeContent).toContain('groupSubagentRuns()');
      expect(readmeContent).toContain('getPersonaColor()');
      expect(readmeContent).toContain('parseMessageSegments()');
    });

    it('documents command history utilities', () => {
      expect(readmeContent).toContain('createEmptyState()');
      expect(readmeContent).toContain('dedupeCommands()');
      expect(readmeContent).toContain('loadCommandHistory()');
      expect(readmeContent).toContain('persistCommandHistory()');
    });

    it('documents the Build Output', () => {
      expect(readmeContent).toContain('dist/index.esm.js');
      expect(readmeContent).toContain('dist/index.cjs.js');
      expect(readmeContent).toContain('dist/index.d.ts');
      expect(readmeContent).toContain('dist/style.css');
    });

    it('has a project structure section', () => {
      expect(readmeContent).toContain('packages/ui/');
      expect(readmeContent).toContain('src/');
      expect(readmeContent).toContain('components/');
      expect(readmeContent).toContain('contexts/');
      expect(readmeContent).toContain('hooks/');
      expect(readmeContent).toContain('utils/');
    });

    it('mentions the MIT license', () => {
      expect(readmeContent).toContain('MIT');
    });

    it('has a Contributing section that references the monorepo', () => {
      expect(readmeContent).toContain('Contributing');
      expect(readmeContent).toContain('monorepo');
    });

    it('mentions the technology stack', () => {
      expect(readmeContent).toContain('CodeMirror 6');
      expect(readmeContent).toContain('xterm.js');
      expect(readmeContent).toContain('react-virtuoso');
    });
  });

  // ── CHANGELOG.md ───────────────────────────────────────────────────
  describe('CHANGELOG.md', () => {
    it('exists and has content', () => {
      expect(changelogContent).toBeDefined();
      expect(changelogContent.length).toBeGreaterThan(100);
    });

    it('has the Changelog title', () => {
      expect(changelogContent).toContain('# Changelog');
    });

    it('contains Keep a Changelog reference', () => {
      expect(changelogContent).toContain('Keep a Changelog');
    });

    it('contains Semantic Versioning reference', () => {
      expect(changelogContent).toContain('Semantic Versioning');
    });

    it('has initial version entry ## [0.1.0]', () => {
      expect(changelogContent).toContain('## [0.1.0]');
    });

    it('contains the date 2025-07-09', () => {
      expect(changelogContent).toContain('2025-07-09');
    });

    it('has ### Added section', () => {
      expect(changelogContent).toContain('### Added');
    });

    it('lists the Editor component', () => {
      expect(changelogContent).toContain('Editor');
    });

    it('lists the Terminal component', () => {
      expect(changelogContent).toContain('Terminal');
    });

    it('lists the FileTree component', () => {
      expect(changelogContent).toContain('FileTree');
    });

    it('lists the Sidebar component', () => {
      expect(changelogContent).toContain('Sidebar');
    });

    it('lists the MenuBar component', () => {
      expect(changelogContent).toContain('MenuBar');
    });

    it('lists the StatusBar component', () => {
      expect(changelogContent).toContain('StatusBar');
    });

    it('lists the ChatPanel component', () => {
      expect(changelogContent).toContain('ChatPanel');
    });

    it('lists the CommandInput component', () => {
      expect(changelogContent).toContain('CommandInput');
    });

    it('lists the MessageBubble component', () => {
      expect(changelogContent).toContain('MessageBubble');
    });

    it('lists the MessageContent component', () => {
      expect(changelogContent).toContain('MessageContent');
    });

    it('lists the CommandPalette component', () => {
      expect(changelogContent).toContain('CommandPalette');
    });

    it('lists the ContextMenu component', () => {
      expect(changelogContent).toContain('ContextMenu');
    });

    it('lists the NotificationProvider', () => {
      expect(changelogContent).toContain('NotificationProvider');
    });

    it('lists the NotificationStack', () => {
      expect(changelogContent).toContain('NotificationStack');
    });

    it('lists the NotificationItem', () => {
      expect(changelogContent).toContain('NotificationItem');
    });

    it('lists the GitSidebarPanel', () => {
      expect(changelogContent).toContain('GitSidebarPanel');
    });

    it('lists the SelectionActionBar component', () => {
      expect(changelogContent).toContain('SelectionActionBar');
    });

    it('lists the Skeleton and SkeletonText components', () => {
      expect(changelogContent).toContain('Skeleton');
      expect(changelogContent).toContain('SkeletonText');
    });

    it('lists the TerminalTabBar component', () => {
      expect(changelogContent).toContain('TerminalTabBar');
    });

    it('lists the LiveLog component', () => {
      expect(changelogContent).toContain('LiveLog');
    });

    it('lists utility exports', () => {
      expect(changelogContent).toContain('generateUUID');
      expect(changelogContent).toContain('copyToClipboard');
      expect(changelogContent).toContain('fuzzyScore');
      expect(changelogContent).toContain('fuzzyFilter');
      expect(changelogContent).toContain('highlightMatches');
      expect(changelogContent).toContain('stripAnsiCodes');
      expect(changelogContent).toContain('ansiToHtml');
      expect(changelogContent).toContain('debugLog');
      expect(changelogContent).toContain('getStatusInfo');
      expect(changelogContent).toContain('groupSubagentRuns');
      expect(changelogContent).toContain('getPersonaColor');
      expect(changelogContent).toContain('parseMessageSegments');
      expect(changelogContent).toContain('detectLineEnding');
    });

    it('lists command history utilities', () => {
      expect(changelogContent).toContain('createEmptyState');
      expect(changelogContent).toContain('dedupeCommands');
      expect(changelogContent).toContain('loadCommandHistory');
      expect(changelogContent).toContain('persistCommandHistory');
    });

    it('lists SproutProvider and its hooks', () => {
      expect(changelogContent).toContain('SproutProvider');
      expect(changelogContent).toContain('useSproutAdapter');
      expect(changelogContent).toContain('useSproutFetch');
    });

    it('lists EventsContextProvider and its hook', () => {
      expect(changelogContent).toContain('EventsContextProvider');
      expect(changelogContent).toContain('useEvents');
    });

    it('mentions the Vite-based library build', () => {
      expect(changelogContent).toContain('ESM');
      expect(changelogContent).toContain('CJS');
      expect(changelogContent).toContain('TypeScript declaration');
    });

    it('mentions Storybook integration', () => {
      expect(changelogContent).toContain('Storybook');
    });

    it('mentions Vitest test infrastructure', () => {
      expect(changelogContent).toContain('Vitest');
      expect(changelogContent).toContain('jsdom');
    });

    it('mentions react-virtuoso integration', () => {
      expect(changelogContent).toContain('react-virtuoso');
    });

    it('mentions language support packages', () => {
      expect(changelogContent).toContain('@codemirror/lang-*');
    });

    it('mentions Emmet and Minimap support', () => {
      expect(changelogContent).toContain('@emmetio/codemirror6-plugin');
      expect(changelogContent).toContain('@replit/codemirror-minimap');
    });
  });

  // ── README cross-references actual exports ─────────────────────────
  describe('README cross-references actual exports in index.ts', () => {
    it('exports all components listed in the README component table', () => {
      // Components listed in the README's Components table
      const readmeComponents = [
        'ChatPanel',
        'CommandInput',
        'CommandPalette',
        'ContextMenu',
        'Editor',
        'FileTree',
        'GitSidebarPanel',
        'LiveLog',
        'MessageBubble',
        'MessageContent',
        'MessageSegments',
        'ChatMessageContextMenu',
        'MenuBar',
        'NotificationStack',
        'NotificationItem',
        'QueuedMessagesPanel',
        'SelectionActionBar',
        'Sidebar',
        'Skeleton',
        'SkeletonText',
        'StatusBar',
        'Terminal',
        'TerminalPane',
        'TerminalTabBar',
      ];

      readmeComponents.forEach((component) => {
        expect(indexContent).toContain(component);
      });
    });

    it('exports all dialog functions listed in the README', () => {
      expect(indexContent).toContain('showThemedAlert');
      expect(indexContent).toContain('showThemedConfirm');
      expect(indexContent).toContain('showThemedPrompt');
    });

    it('exports all contexts listed in the README', () => {
      expect(indexContent).toContain('NotificationProvider');
      expect(indexContent).toContain('useNotifications');
      expect(indexContent).toContain('SproutProvider');
      expect(indexContent).toContain('useSproutAdapter');
      expect(indexContent).toContain('useSproutFetch');
      expect(indexContent).toContain('EventsContextProvider');
      expect(indexContent).toContain('useEvents');
    });

    it('exports all hooks listed in the README', () => {
      expect(indexContent).toContain('useMultiSelect');
      expect(indexContent).toContain('flattenVisibleFiles');
    });

    it('exports all utility functions listed in the README', () => {
      expect(indexContent).toContain('generateUUID');
      expect(indexContent).toContain('copyToClipboard');
      expect(indexContent).toContain('fuzzyScore');
      expect(indexContent).toContain('fuzzyFilter');
      expect(indexContent).toContain('highlightMatches');
      expect(indexContent).toContain('stripAnsiCodes');
      expect(indexContent).toContain('ansiToHtml');
      expect(indexContent).toContain('debugLog');
      expect(indexContent).toContain('getStatusInfo');
      expect(indexContent).toContain('groupSubagentRuns');
      expect(indexContent).toContain('getPersonaColor');
      expect(indexContent).toContain('parseMessageSegments');
    });

    it('exports all command history utilities listed in the README', () => {
      expect(indexContent).toContain('createEmptyState');
      expect(indexContent).toContain('dedupeCommands');
      expect(indexContent).toContain('loadCommandHistory');
      expect(indexContent).toContain('persistCommandHistory');
    });

    it('exports the message segment types listed in the README Types section', () => {
      expect(indexContent).toContain('TextSegment');
      expect(indexContent).toContain('ToolCallSegment');
      expect(indexContent).toContain('MessageSegment');
    });

    it('exports key types from the README Types table', () => {
      expect(indexContent).toContain('ChatProps');
      expect(indexContent).toContain('Message');
      expect(indexContent).toContain('ToolExecution');
      expect(indexContent).toContain('EditorProps');
      expect(indexContent).toContain('EditorBuffer');
      expect(indexContent).toContain('PaneLayout');
      expect(indexContent).toContain('EditorState');
      expect(indexContent).toContain('FileTreeProps');
      expect(indexContent).toContain('FileInfo');
      expect(indexContent).toContain('TerminalProps');
      expect(indexContent).toContain('CommandPaletteProps');
      expect(indexContent).toContain('StatusBarProps');
      expect(indexContent).toContain('GitStatusData');
      expect(indexContent).toContain('GitFile');
      expect(indexContent).toContain('Revision');
      expect(indexContent).toContain('APIAdapter');
      expect(indexContent).toContain('SproutEvent');
      expect(indexContent).toContain('EventsProvider');
    });
  });
});
