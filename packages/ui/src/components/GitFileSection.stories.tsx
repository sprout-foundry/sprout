import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { GitFileSection } from './GitFileSection';

const meta = {
  title: 'Components/GitFileSection',
  component: GitFileSection,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof GitFileSection>;

export default meta;
type Story = StoryObj<typeof meta>;

const modifiedFiles = ['src/App.tsx', 'src/utils/api.ts', 'package.json'];
const untrackedFiles = ['src/components/NewButton.tsx', 'test/new-test.spec.ts'];
const stagedFiles = ['src/index.tsx', 'README.md'];
const deletedFiles = ['src/old-file.tsx', 'config.json'];
const renamedFiles = [
  { from: 'src/components/Button.tsx', to: 'src/components/ButtonV2.tsx' },
  { from: 'utils/helpers.ts', to: 'src/utils/helpers.ts' },
];

export const Modified: Story = {
  args: {
    type: 'modified',
    title: 'Modified Files',
    files: modifiedFiles,
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const Untracked: Story = {
  args: {
    type: 'untracked',
    title: 'Untracked Files',
    files: untrackedFiles,
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const Staged: Story = {
  args: {
    type: 'staged',
    title: 'Staged Changes',
    files: stagedFiles,
    isStaged: () => true,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const Deleted: Story = {
  args: {
    type: 'deleted',
    title: 'Deleted Files',
    files: deletedFiles,
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const Renamed: Story = {
  args: {
    type: 'renamed',
    title: 'Renamed Files',
    files: [],
    renamedFiles,
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const MixedStaged: Story = {
  args: {
    type: 'modified',
    title: 'Modified Files',
    files: modifiedFiles,
    isStaged: (path) => path === 'src/App.tsx' || path === 'package.json',
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const SingleFile: Story = {
  args: {
    type: 'modified',
    title: 'Modified Files',
    files: ['src/App.tsx'],
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const ManyFiles: Story = {
  args: {
    type: 'modified',
    title: 'Modified Files',
    files: [
      'src/App.tsx',
      'src/components/Button.tsx',
      'src/components/Card.tsx',
      'src/components/Modal.tsx',
      'src/hooks/useAuth.ts',
      'src/hooks/useData.ts',
      'src/utils/api.ts',
      'src/utils/helpers.ts',
      'src/index.tsx',
      'src/styles.css',
      'package.json',
      'tsconfig.json',
    ],
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const DeepPaths: Story = {
  args: {
    type: 'modified',
    title: 'Modified Files',
    files: [
      'src/components/ui/button/Button.tsx',
      'src/components/ui/modal/Modal.tsx',
      'src/features/auth/components/LoginForm.tsx',
      'src/features/auth/services/authService.ts',
      'src/features/dashboard/components/Dashboard.tsx',
    ],
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const SpecialCharacters: Story = {
  args: {
    type: 'modified',
    title: 'Modified Files',
    files: [
      'src/components/button with spaces.tsx',
      'src/utils/file-with-dashes.ts',
      'src/test file (2).spec.ts',
    ],
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const AllStaged: Story = {
  args: {
    type: 'modified',
    title: 'Modified Files',
    files: modifiedFiles,
    isStaged: () => true,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const RenamedWithMixedStaging: Story = {
  args: {
    type: 'renamed',
    title: 'Renamed Files',
    files: [],
    renamedFiles: [
      { from: 'src/Button.tsx', to: 'src/components/Button.tsx' },
      { from: 'utils/api.ts', to: 'src/services/api.ts' },
    ],
    isStaged: (path) => path === 'src/components/Button.tsx',
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const RenamedWithOriginalFiles: Story = {
  args: {
    type: 'renamed',
    title: 'Renamed Files',
    files: ['src/Card.tsx', 'src/Input.tsx'],
    renamedFiles: [
      { from: 'src/Button.tsx', to: 'src/components/Button.tsx' },
    ],
    isStaged: () => false,
    onFileClick: (path) => console.log('Clicked file:', path),
  },
};

export const Interactive: Story = {
  render: () => {
    const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());
    const [logs, setLogs] = useState<string[]>([]);

    const addLog = (message: string) => {
      setLogs((prev) => [...prev, `[${new Date().toLocaleTimeString()}] ${message}`]);
    };

    const isStaged = (path: string) => selectedFiles.has(path);

    const handleFileClick = (path: string) => {
      setSelectedFiles((prev) => {
        const newSet = new Set(prev);
        if (newSet.has(path)) {
          newSet.delete(path);
          addLog(`Unstaged file: ${path}`);
        } else {
          newSet.add(path);
          addLog(`Staged file: ${path}`);
        }
        return newSet;
      });
    };

    return (
      <div style={{ padding: '20px', maxWidth: '600px' }}>
        <div style={{ marginBottom: '20px', paddingBottom: '10px', borderBottom: '1px solid #444' }}>
          <h3 style={{ margin: '0 0 10px 0' }}>Interactive Git File Sections</h3>
          <p style={{ margin: 0, color: '#888' }}>
            Click on files to stage/unstage them. Staged files show a minus sign.
          </p>
        </div>

        <div style={{ marginBottom: '15px' }}>
          <p style={{ margin: '0', fontSize: '14px', color: '#888' }}>
            Staged files: <strong>{selectedFiles.size}</strong>
          </p>
        </div>

        <div style={{ marginBottom: '20px', background: '#2d2d2d', padding: '15px', borderRadius: '4px' }}>
          <GitFileSection
            type="modified"
            title="Modified Files"
            files={modifiedFiles}
            isStaged={isStaged}
            onFileClick={handleFileClick}
          />
        </div>

        <div style={{ marginBottom: '20px', background: '#2d2d2d', padding: '15px', borderRadius: '4px' }}>
          <GitFileSection
            type="untracked"
            title="Untracked Files"
            files={untrackedFiles}
            isStaged={isStaged}
            onFileClick={handleFileClick}
          />
        </div>

        <div style={{ marginBottom: '20px', background: '#2d2d2d', padding: '15px', borderRadius: '4px' }}>
          <GitFileSection
            type="staged"
            title="Staged Changes"
            files={stagedFiles}
            isStaged={isStaged}
            onFileClick={handleFileClick}
          />
        </div>

        <div style={{ marginBottom: '20px', background: '#2d2d2d', padding: '15px', borderRadius: '4px' }}>
          <GitFileSection
            type="renamed"
            title="Renamed Files"
            files={[]}
            renamedFiles={renamedFiles}
            isStaged={isStaged}
            onFileClick={handleFileClick}
          />
        </div>

        <div style={{ marginBottom: '20px', background: '#2d2d2d', padding: '15px', borderRadius: '4px' }}>
          <GitFileSection
            type="deleted"
            title="Deleted Files"
            files={deletedFiles}
            isStaged={isStaged}
            onFileClick={handleFileClick}
          />
        </div>

        <div>
          <h4 style={{ margin: '0 0 10px 0' }}>Event Log:</h4>
          <div
            style={{
              background: '#1e1e1e',
              color: '#d4d4d4',
              padding: '15px',
              borderRadius: '4px',
              fontFamily: 'monospace',
              fontSize: '12px',
              maxHeight: '200px',
              overflow: 'auto',
            }}
          >
            {logs.length === 0 && <em style={{ color: '#666' }}>No events yet...</em>}
            {logs.map((log, index) => (
              <div key={index} style={{ marginBottom: '4px' }}>
                {log}
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  },
};
