import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import FileTree from './FileTree';
import {
  mockFileTree,
  emptyFileTree,
  mockFileHandlers,
} from '../../.storybook/mocks/fixtures';

const meta = {
  title: 'Components/FileTree',
  component: FileTree,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof FileTree>;

export default meta;
type Story = StoryObj<typeof FileTree>;

export const Default: Story = {
  args: {
    files: mockFileTree,
    rootPath: '.',
    workspaceRoot: '/home/user/demo',
    ...mockFileHandlers,
  },
};

export const Empty: Story = {
  args: {
    files: emptyFileTree,
    rootPath: '.',
    workspaceRoot: '/home/user/demo',
    ...mockFileHandlers,
  },
};

export const WithSelectedFile: Story = {
  render: () => {
    const [selectedFile, setSelectedFile] = useState('src/App.tsx');

    return (
      <div style={{ height: '500px', width: '300px' }}>
        <FileTree
          files={mockFileTree}
          rootPath="."
          workspaceRoot="/home/user/demo"
          selectedFile={selectedFile}
          onFileSelect={(file) => setSelectedFile(file.path)}
          onRefresh={mockFileHandlers.onRefresh}
          onItemCreated={mockFileHandlers.onItemCreated}
          onDeleteItem={mockFileHandlers.onDeleteItem}
          onCreateFile={mockFileHandlers.onCreateFile}
          onCreateFolder={mockFileHandlers.onCreateFolder}
          onDeletePath={mockFileHandlers.onDeletePath}
          onRenamePath={mockFileHandlers.onRenamePath}
          onOpenInFileBrowser={mockFileHandlers.onOpenInFileBrowser}
        />
      </div>
    );
  },
};

export const WithFilter: Story = {
  render: () => {
    const [filterQuery, setFilterQuery] = useState('');

    return (
      <div style={{ padding: '20px' }}>
        <input
          type="text"
          placeholder="Filter files (try 'ts' or 'Button')"
          value={filterQuery}
          onChange={(e) => setFilterQuery(e.target.value)}
          style={{ marginBottom: '20px', padding: '8px 12px', width: '300px' }}
        />
        <div style={{ height: '500px', width: '300px' }}>
          <FileTree
            files={mockFileTree}
            rootPath="."
            workspaceRoot="/home/user/demo"
            onFileSelect={mockFileHandlers.onFileSelect}
            onRefresh={mockFileHandlers.onRefresh}
            onItemCreated={mockFileHandlers.onItemCreated}
            onDeleteItem={mockFileHandlers.onDeleteItem}
            onCreateFile={mockFileHandlers.onCreateFile}
            onCreateFolder={mockFileHandlers.onCreateFolder}
            onDeletePath={mockFileHandlers.onDeletePath}
            onRenamePath={mockFileHandlers.onRenamePath}
            onOpenInFileBrowser={mockFileHandlers.onOpenInFileBrowser}
          />
        </div>
        <p style={{ padding: '10px', color: '#666' }}>
          In a real scenario, FileTree has an internal filter input.
          This demo shows the tree with various files.
        </p>
      </div>
    );
  },
};

export const WithGitIgnored: Story = {
  args: {
    files: mockFileTree,
    rootPath: '.',
    workspaceRoot: '/home/user/demo',
    ...mockFileHandlers,
  },
  parameters: {
    docs: {
      description: {
        story: 'The .gitignore file in the mock data has gitStatus: "ignored" and is shown/hidden with the toggle button.',
      },
    },
  },
};

export const InteractiveDemo: Story = {
  render: () => {
    const [selectedFile, setSelectedFile] = useState<string | undefined>();

    return (
      <div style={{ padding: '20px' }}>
        <h3>File Tree Demo</h3>
        <p>Click on files to select them. Click folders to expand/collapse.</p>
        {selectedFile && <p style={{ marginBottom: '20px' }}>Selected: <strong>{selectedFile}</strong></p>}
        <div style={{ height: '500px', width: '320px', border: '1px solid #ccc', borderRadius: '8px' }}>
          <FileTree
            files={mockFileTree}
            rootPath="."
            workspaceRoot="/home/user/demo"
            selectedFile={selectedFile}
            onFileSelect={(file) => setSelectedFile(file.path)}
            onRefresh={mockFileHandlers.onRefresh}
            onItemCreated={mockFileHandlers.onItemCreated}
            onDeleteItem={mockFileHandlers.onDeleteItem}
            onCreateFile={mockFileHandlers.onCreateFile}
            onCreateFolder={mockFileHandlers.onCreateFolder}
            onDeletePath={mockFileHandlers.onDeletePath}
            onRenamePath={mockFileHandlers.onRenamePath}
            onOpenInFileBrowser={mockFileHandlers.onOpenInFileBrowser}
          />
        </div>
      </div>
    );
  },
};

export const NestedDirectories: Story = {
  render: () => {
    const nestedTree = [
      {
        name: 'project',
        path: 'project',
        isDir: true,
        size: 4096,
        modified: Date.now(),
        children: [
          {
            name: 'src',
            path: 'project/src',
            isDir: true,
            size: 4096,
            modified: Date.now(),
            children: [
              {
                name: 'features',
                path: 'project/src/features',
                isDir: true,
                size: 4096,
                modified: Date.now(),
                children: [
                  {
                    name: 'auth',
                    path: 'project/src/features/auth',
                    isDir: true,
                    size: 4096,
                    modified: Date.now(),
                    children: [
                      {
                        name: 'login.tsx',
                        path: 'project/src/features/auth/login.tsx',
                        isDir: false,
                        size: 1234,
                        modified: Date.now(),
                        ext: '.tsx',
                      },
                    ],
                  },
                ],
              },
              {
                name: 'App.tsx',
                path: 'project/src/App.tsx',
                isDir: false,
                size: 2345,
                modified: Date.now(),
                ext: '.tsx',
              },
            ],
          },
          {
            name: 'tests',
            path: 'project/tests',
            isDir: true,
            size: 4096,
            modified: Date.now(),
            children: [
              {
                name: 'unit',
                path: 'project/tests/unit',
                isDir: true,
                size: 4096,
                modified: Date.now(),
                children: [
                  {
                    name: 'auth.test.tsx',
                    path: 'project/tests/unit/auth.test.tsx',
                    isDir: false,
                    size: 567,
                    modified: Date.now(),
                    ext: '.tsx',
                  },
                ],
              },
            ],
          },
        ],
      },
    ];

    return (
      <div style={{ padding: '20px' }}>
        <h3>Deeply Nested Structure</h3>
        <div style={{ height: '500px', width: '300px' }}>
          <FileTree
            files={nestedTree}
            rootPath="project"
            workspaceRoot="/home/user/demo"
            {...mockFileHandlers}
          />
        </div>
      </div>
    );
  },
};

export const MixedFileTypes: Story = {
  render: () => {
    const mixedFiles = [
      {
        name: 'styles.css',
        path: 'styles.css',
        isDir: false,
        size: 1234,
        modified: Date.now(),
        ext: '.css',
      },
      {
        name: 'script.js',
        path: 'script.js',
        isDir: false,
        size: 2345,
        modified: Date.now(),
        ext: '.js',
      },
      {
        name: 'component.tsx',
        path: 'component.tsx',
        isDir: false,
        size: 3456,
        modified: Date.now(),
        ext: '.tsx',
      },
      {
        name: 'types.ts',
        path: 'types.ts',
        isDir: false,
        size: 567,
        modified: Date.now(),
        ext: '.ts',
      },
      {
        name: 'data.json',
        path: 'data.json',
        isDir: false,
        size: 890,
        modified: Date.now(),
        ext: '.json',
      },
      {
        name: 'README.md',
        path: 'README.md',
        isDir: false,
        size: 2345,
        modified: Date.now(),
        ext: '.md',
      },
      {
        name: 'config.yml',
        path: 'config.yml',
        isDir: false,
        size: 456,
        modified: Date.now(),
        ext: '.yml',
      },
      {
        name: 'app.sh',
        path: 'app.sh',
        isDir: false,
        size: 678,
        modified: Date.now(),
        ext: '.sh',
      },
      {
        name: 'logo.png',
        path: 'logo.png',
        isDir: false,
        size: 15000,
        modified: Date.now(),
        ext: '.png',
      },
    ];

    return (
      <div style={{ padding: '20px' }}>
        <h3>Mixed File Types</h3>
        <p>Demonstrates different file icons for various extensions</p>
        <div style={{ height: '500px', width: '300px' }}>
          <FileTree
            files={mixedFiles}
            rootPath="."
            workspaceRoot="/home/user/demo"
            {...mockFileHandlers}
          />
        </div>
      </div>
    );
  },
};
