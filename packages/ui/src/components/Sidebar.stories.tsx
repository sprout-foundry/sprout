import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import Sidebar from './Sidebar';
import { mockFileTree, mockGitStatus, mockGitBranches, mockFileHandlers, mockGitHandlers } from '../../.storybook/mocks/fixtures';

const meta = {
  title: 'Components/Sidebar',
  component: Sidebar,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof Sidebar>;

export default meta;
type Story = StoryObj<typeof meta>;

export const FilesSection: Story = {
  args: {
    activeSection: 'files',
    fileTreeProps: {
      files: mockFileTree,
      rootPath: '.',
      workspaceRoot: '/home/user/demo',
      ...mockFileHandlers,
    },
  },
};

export const GitSection: Story = {
  args: {
    activeSection: 'git',
    gitPanelProps: {
      gitStatus: mockGitStatus,
      gitBranches: mockGitBranches,
      selectedFiles: new Set(),
      activeDiffSelectionKey: null,
      commitMessage: 'Update components',
      isLoading: false,
      isActing: false,
      isGeneratingCommitMessage: false,
      isReviewLoading: false,
      actionError: null,
      actionWarning: null,
      workspaceRoot: '/home/user/demo',
      ...mockGitHandlers,
    },
  },
};

export const SearchSection: Story = {
  args: {
    activeSection: 'search',
    searchContent: (
      <div style={{ padding: '20px', color: '#666' }}>
        <p>Search functionality would go here.</p>
        <p>This is a placeholder for the search section content.</p>
      </div>
    ),
  },
};

export const SettingsSection: Story = {
  args: {
    activeSection: 'settings',
    settingsContent: (
      <div style={{ padding: '20px' }}>
        <h4>Settings</h4>
        <div style={{ marginBottom: '10px' }}>
          <label>
            <input type="checkbox" /> Enable autosave
          </label>
        </div>
        <div>
          <label>
            <input type="checkbox" /> Show line numbers
          </label>
        </div>
      </div>
    ),
  },
};

export const Collapsed: Story = {
  args: {
    collapsed: true,
  },
};

export const WithCustomBranding: Story = {
  args: {
    activeSection: 'files',
    branding: (
      <div style={{ padding: '15px', fontWeight: 'bold', fontSize: '16px' }}>
        🌱 Sprout IDE
      </div>
    ),
    fileTreeProps: {
      files: mockFileTree,
      rootPath: '.',
      workspaceRoot: '/home/user/demo',
      ...mockFileHandlers,
    },
  },
};

export const Interactive: Story = {
  render: () => {
    const [activeSection, setActiveSection] = useState<'files' | 'git' | 'search' | 'settings'>('files');
    const [collapsed, setCollapsed] = useState(false);

    return (
      <div style={{ display: 'flex', gap: '20px', height: '600px' }}>
        <Sidebar
          activeSection={activeSection}
          onSectionChange={setActiveSection}
          collapsed={collapsed}
          onToggleCollapse={() => setCollapsed(!collapsed)}
          fileTreeProps={{
            files: mockFileTree,
            rootPath: '.',
            workspaceRoot: '/home/user/demo',
            ...mockFileHandlers,
          }}
          gitPanelProps={{
            gitStatus: mockGitStatus,
            gitBranches: mockGitBranches,
            selectedFiles: new Set(),
            activeDiffSelectionKey: null,
            commitMessage: 'Update components',
            isLoading: false,
            isActing: false,
            isGeneratingCommitMessage: false,
            isReviewLoading: false,
            actionError: null,
            actionWarning: null,
            workspaceRoot: '/home/user/demo',
            ...mockGitHandlers,
          }}
          branding={
            <div style={{ padding: '15px', fontWeight: 'bold', fontSize: '16px' }}>
              🌱 Sprout IDE
            </div>
          }
        />
        <div style={{ flex: 1, padding: '20px' }}>
          <h2>Interactive Sidebar Demo</h2>
          <p>Click on tabs to switch sections. Toggle collapse button to expand/collapse.</p>
          <p>Current section: <strong>{activeSection}</strong></p>
          <p>Collapsed: <strong>{collapsed ? 'Yes' : 'No'}</strong></p>
        </div>
      </div>
    );
  },
};

export const NarrowWidth: Story = {
  args: {
    activeSection: 'files',
    width: 200,
    fileTreeProps: {
      files: mockFileTree,
      rootPath: '.',
      workspaceRoot: '/home/user/demo',
      ...mockFileHandlers,
    },
  },
};

export const WideWidth: Story = {
  args: {
    activeSection: 'files',
    width: 350,
    fileTreeProps: {
      files: mockFileTree,
      rootPath: '.',
      workspaceRoot: '/home/user/demo',
      ...mockFileHandlers,
    },
  },
};
