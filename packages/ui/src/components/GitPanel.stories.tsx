import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import GitSidebarPanel from './GitPanel';
import {
  mockGitStatus,
  cleanGitStatus,
  mockGitBranches,
  mockGitHandlers,
} from '../../.storybook/mocks/fixtures';

const meta = {
  title: 'Components/GitPanel',
  component: GitSidebarPanel,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof GitSidebarPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WithChanges: Story = {
  args: {
    gitStatus: mockGitStatus,
    gitBranches: mockGitBranches,
    selectedFiles: new Set(),
    activeDiffSelectionKey: null,
    commitMessage: 'Add new components and update dependencies',
    isLoading: false,
    isActing: false,
    isGeneratingCommitMessage: false,
    isReviewLoading: false,
    actionError: null,
    actionWarning: null,
    workspaceRoot: '/home/user/demo',
    ...mockGitHandlers,
  },
};

export const Clean: Story = {
  args: {
    gitStatus: cleanGitStatus,
    gitBranches: mockGitBranches,
    selectedFiles: new Set(),
    activeDiffSelectionKey: null,
    commitMessage: '',
    isLoading: false,
    isActing: false,
    isGeneratingCommitMessage: false,
    isReviewLoading: false,
    actionError: null,
    actionWarning: null,
    workspaceRoot: '/home/user/demo',
    ...mockGitHandlers,
  },
};

export const Loading: Story = {
  args: {
    gitStatus: null,
    gitBranches: mockGitBranches,
    selectedFiles: new Set(),
    activeDiffSelectionKey: null,
    commitMessage: '',
    isLoading: true,
    isActing: false,
    isGeneratingCommitMessage: false,
    isReviewLoading: false,
    actionError: null,
    actionWarning: null,
    workspaceRoot: '/home/user/demo',
    ...mockGitHandlers,
  },
};

export const WithError: Story = {
  args: {
    gitStatus: mockGitStatus,
    gitBranches: mockGitBranches,
    selectedFiles: new Set(),
    activeDiffSelectionKey: null,
    commitMessage: 'Fix bug in authentication flow',
    isLoading: false,
    isActing: false,
    isGeneratingCommitMessage: false,
    isReviewLoading: false,
    actionError: 'Failed to stage files: Permission denied',
    actionWarning: null,
    workspaceRoot: '/home/user/demo',
    ...mockGitHandlers,
  },
};

export const WithWarning: Story = {
  args: {
    gitStatus: mockGitStatus,
    gitBranches: mockGitBranches,
    selectedFiles: new Set(),
    activeDiffSelectionKey: null,
    commitMessage: 'Update styles',
    isLoading: false,
    isActing: false,
    isGeneratingCommitMessage: false,
    isReviewLoading: false,
    actionError: null,
    actionWarning: 'Some files contain merge conflicts. Please resolve them before committing.',
    workspaceRoot: '/home/user/demo',
    ...mockGitHandlers,
  },
};

export const WithSelectedFiles: Story = {
  render: () => {
    const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set(['modified:src/components/Card.tsx']));

    const handleToggleFileSelection = (section: 'staged' | 'modified' | 'untracked' | 'deleted', path: string) => {
      const key = `${section}:${path}`;
      const newSet = new Set(selectedFiles);
      if (newSet.has(key)) {
        newSet.delete(key);
      } else {
        newSet.add(key);
      }
      setSelectedFiles(newSet);
    };

    return (
      <div style={{ width: '400px' }}>
        <GitSidebarPanel
          gitStatus={mockGitStatus}
          gitBranches={mockGitBranches}
          selectedFiles={selectedFiles}
          activeDiffSelectionKey="modified:src/components/Card.tsx"
          commitMessage="Update Card component styles"
          isLoading={false}
          isActing={false}
          isGeneratingCommitMessage={false}
          isReviewLoading={false}
          actionError={null}
          actionWarning={null}
          workspaceRoot="/home/user/demo"
          onToggleFileSelection={handleToggleFileSelection}
          onClearSelection={() => setSelectedFiles(new Set())}
          {...mockGitHandlers}
        />
      </div>
    );
  },
};

export const GeneratingCommitMessage: Story = {
  args: {
    gitStatus: mockGitStatus,
    gitBranches: mockGitBranches,
    selectedFiles: new Set(),
    activeDiffSelectionKey: null,
    commitMessage: '',
    isLoading: false,
    isActing: false,
    isGeneratingCommitMessage: true,
    isReviewLoading: false,
    actionError: null,
    actionWarning: null,
    workspaceRoot: '/home/user/demo',
    ...mockGitHandlers,
  },
};

export const Reviewing: Story = {
  args: {
    gitStatus: mockGitStatus,
    gitBranches: mockGitBranches,
    selectedFiles: new Set(),
    activeDiffSelectionKey: null,
    commitMessage: 'Add new features',
    isLoading: false,
    isActing: false,
    isGeneratingCommitMessage: false,
    isReviewLoading: true,
    actionError: null,
    actionWarning: null,
    workspaceRoot: '/home/user/demo',
    ...mockGitHandlers,
  },
};

export const InteractiveDemo: Story = {
  render: () => {
    const [commitMessage, setCommitMessage] = useState('');
    const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());

    const handleToggleFileSelection = (section: 'staged' | 'modified' | 'untracked' | 'deleted', path: string) => {
      const key = `${section}:${path}`;
      const newSet = new Set(selectedFiles);
      if (newSet.has(key)) {
        newSet.delete(key);
      } else {
        newSet.add(key);
      }
      setSelectedFiles(newSet);
    };

    return (
      <div style={{ padding: '20px' }}>
        <h3>Git Panel Demo</h3>
        <p>Click files to select them, stage/unstage, and write commit messages.</p>
        <div style={{ width: '400px' }}>
          <GitSidebarPanel
            gitStatus={mockGitStatus}
            gitBranches={mockGitBranches}
            selectedFiles={selectedFiles}
            activeDiffSelectionKey={null}
            commitMessage={commitMessage}
            isLoading={false}
            isActing={false}
            isGeneratingCommitMessage={false}
            isReviewLoading={false}
            actionError={null}
            actionWarning={null}
            workspaceRoot="/home/user/demo"
            onCommitMessageChange={setCommitMessage}
            onToggleFileSelection={handleToggleFileSelection}
            onClearSelection={() => setSelectedFiles(new Set())}
            {...mockGitHandlers}
          />
        </div>
      </div>
    );
  },
};
