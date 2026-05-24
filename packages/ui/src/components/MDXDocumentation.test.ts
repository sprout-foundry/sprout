import { describe, it, expect, beforeAll } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';

// Resolve the components directory relative to this file
const componentsDir = path.resolve(__dirname);

// Define expected MDX files and their validation requirements
interface MDXFileConfig {
  filename: string;
  title: string;
  expectedSections: string[];
  storiesModule: string;
}

const mdxFiles: MDXFileConfig[] = [
  {
    filename: 'FileTree.mdx',
    title: 'Components/FileTree',
    expectedSections: ['Overview', 'Props Reference', 'Usage Examples', 'Integration'],
    storiesModule: './FileTree.stories',
  },
  {
    filename: 'ChatPanel.mdx',
    title: 'Components/ChatPanel',
    expectedSections: ['Overview', 'Props Reference', 'Streaming Message Handling', 'Usage Examples'],
    storiesModule: './ChatPanel.stories',
  },
  {
    filename: 'GitPanel.mdx',
    title: 'Components/GitPanel',
    expectedSections: ['Overview', 'Props Reference', 'File Change List', 'Workflow'],
    storiesModule: './GitPanel.stories',
  },
];

function readMDXFile(filename: string): string {
  const filePath = path.resolve(componentsDir, filename);
  return fs.readFileSync(filePath, 'utf-8');
}

describe('MDX Documentation Files', () => {
  mdxFiles.forEach((config) => {
    describe(`${config.filename}`, () => {
      let content: string;

      beforeAll(() => {
        content = readMDXFile(config.filename);
      });

      it('exists on disk and is readable', () => {
        const filePath = path.resolve(componentsDir, config.filename);
        expect(fs.existsSync(filePath)).toBe(true);
        expect(content.length).toBeGreaterThan(0);
      });

      it('imports Meta from @storybook/blocks', () => {
        expect(content).toMatch(/import\s*{[^}]*Meta[^}]*}\s*from\s*['"]@storybook\/blocks['"]/);
      });

      it('contains a <Meta> tag with correct title', () => {
        expect(content).toMatch(
          new RegExp(`<Meta\\s+title=["']${config.title}["'][^>]*\\s*/?>`)
        );
      });

      it('imports the corresponding stories module', () => {
        // MDX files use default imports: import FileTreeStories from './FileTree.stories'
        // Extract the story file name (e.g., 'FileTree.stories' from './FileTree.stories')
        const storyFile = config.storiesModule.replace(/^\.\//, '');
        expect(content).toMatch(
          new RegExp(`import\\s+\\w+\\s+from\\s+['"].*${storyFile}['"]`)
        );
      });

      it('contains all expected content sections', () => {
        config.expectedSections.forEach((section) => {
          // MDX headings use # syntax, so check for heading markers
          expect(content).toMatch(
            new RegExp(`#{1,3}\\s+${section}`),
            `Expected section "${section}" to be present as a heading`
          );
        });
      });

      it('contains a <Controls> or <Props> documentation block', () => {
        const hasControls = content.includes('<Controls') || content.includes('<Controls ');
        const hasProps = content.includes('<Props') || content.includes('<Props ');
        expect(hasControls || hasProps).toBe(
          true,
          `Expected ${config.filename} to contain a <Controls> or <Props> block for props documentation`
        );
      });

      it('contains fenced code blocks for usage examples', () => {
        const codeBlockRegex = /```[a-zA-Z+]*\n[\s\S]*?\n```/;
        expect(codeBlockRegex.test(content)).toBe(
          true,
          `Expected ${config.filename} to contain at least one fenced code block`
        );
      });
    });
  });

  // Storybook config validation
  describe('Storybook main.ts config', () => {
    const configPath = path.resolve(componentsDir, '../../.storybook/main.ts');

    it('includes MDX files in stories pattern', () => {
      const configContent = fs.readFileSync(configPath, 'utf-8');
      expect(configContent).toContain('../src/**/*.mdx');
    });
  });
});
