import type { Meta, StoryObj } from '@storybook/react';
import GitSidebarPanel from './GitSidebarPanel';

const meta = {
  title: 'Components/GitSidebarPanel',
  component: GitSidebarPanel,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof GitSidebarPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {},
};

export const WithWidth: Story = {
  render: () => (
    <div style={{ width: '350px', height: '600px', border: '1px solid #ccc' }}>
      <GitSidebarPanel />
    </div>
  ),
};

export const WithContext: Story = {
  render: () => (
    <div style={{ display: 'flex', height: '600px' }}>
      <div style={{ width: '300px', borderRight: '1px solid #ccc', padding: '20px', background: '#f5f5f5' }}>
        <h3>Git Panel Context</h3>
        <p>This is a placeholder component. In the full implementation, this would display:</p>
        <ul>
          <li>Current branch status</li>
          <li>Staged files</li>
          <li>Modified files</li>
          <li>Untracked files</li>
          <li>Commit message input</li>
          <li>Branch management controls</li>
        </ul>
      </div>
      <div style={{ flex: 1, borderLeft: '1px solid #ccc' }}>
        <GitSidebarPanel />
      </div>
    </div>
  ),
};
