import type { Meta, StoryObj } from '@storybook/react';
import PreviewPane from './PreviewPane';

const meta = {
  title: 'Components/PreviewPane',
  component: PreviewPane,
  parameters: {
    layout: 'padded',
  },
  tags: ['autodocs'],
  argTypes: {
    status: {
      control: { type: 'select' },
      options: ['starting', 'running', 'stopped', 'failed'],
    },
    url: { control: 'text' },
    error: { control: 'text' },
    title: { control: 'text' },
    restartDisabled: { control: 'boolean' },
    reloadKey: { control: 'number' },
  },
} satisfies Meta<typeof PreviewPane>;

export default meta;
type Story = StoryObj<typeof PreviewPane>;

// ── Running (the iframe embed) ────────────────────────────────────

export const Running: Story = {
  args: {
    status: 'running',
    url: 'https://example.com',
    title: 'My App',
  },
};

// ── Starting (spinner) ────────────────────────────────────────────

export const Starting: Story = {
  args: {
    status: 'starting',
    title: 'My App',
  },
};

// ── Stopped (placeholder + restart hint) ──────────────────────────

export const Stopped: Story = {
  args: {
    status: 'stopped',
  },
};

// ── Failed (error reason) ─────────────────────────────────────────

export const Failed: Story = {
  args: {
    status: 'failed',
    error: 'dev server exited with code 1',
  },
};

// ── Hosted preview (restart disabled by the parent) ───────────────

export const HostedPreview: Story = {
  args: {
    status: 'running',
    url: 'https://preview.example.com',
    title: 'Hosted Preview',
    restartDisabled: true,
  },
};

// ── With close affordance ─────────────────────────────────────────

export const Closable: Story = {
  args: {
    status: 'stopped',
    title: 'My App',
    onClose: () => undefined,
  },
};
