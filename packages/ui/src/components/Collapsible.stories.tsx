import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import { Bell, Settings, ChevronDown } from 'lucide-react';
import Collapsible from './Collapsible';

const meta = {
  title: 'Components/Collapsible',
  component: Collapsible,
  parameters: {
    layout: 'padded',
  },
  tags: ['autodocs'],
  argTypes: {
    title: { control: 'text' },
    defaultOpen: { control: 'boolean' },
    disabled: { control: 'boolean' },
    variant: {
      control: { type: 'select' },
      options: ['default', 'flush'],
    },
  },
} satisfies Meta<typeof Collapsible>;

export default meta;
type Story = StoryObj<typeof Collapsible>;

// ── Default (closed) ──────────────────────────────────────────────

export const Default: Story = {
  args: {
    title: 'Performance settings',
    children: (
      <p style={{ margin: 0, fontSize: 13, lineHeight: 1.5 }}>
        Configure timeouts, cost controls, and resource storage. Open this section to
        tune the agent's runtime behaviour.
      </p>
    ),
  },
};

// ── Default open ──────────────────────────────────────────────────

export const DefaultOpen: Story = {
  args: {
    ...Default.args,
    title: 'Open by default',
    defaultOpen: true,
  },
};

// ── Controlled (interactive demo) ─────────────────────────────────

export const Controlled: Story = {
  render: () => {
    const Wrapper = () => {
      const [open, setOpen] = useState(false);
      return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div style={{ display: 'flex', gap: 8 }}>
            <button
              type="button"
              onClick={() => setOpen(true)}
              style={{
                padding: '6px 12px',
                borderRadius: 6,
                border: '1px solid #555',
                background: '#1f1f23',
                color: '#fff',
                cursor: 'pointer',
              }}
            >
              Force open
            </button>
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              style={{
                padding: '6px 12px',
                borderRadius: 6,
                border: '1px solid #555',
                background: '#1f1f23',
                color: '#fff',
                cursor: 'pointer',
              }}
            >
              Toggle from outside
            </button>
          </div>
          <Collapsible
            title="Controlled section"
            open={open}
            onOpenChange={setOpen}
            summaryExtra={
              <span style={{ fontFamily: 'monospace' }}>{open ? 'open' : 'closed'}</span>
            }
          >
            <p style={{ margin: 0, fontSize: 13 }}>
              The parent owns the open state. Clicking the summary calls
              <code style={{ marginLeft: 4 }}>onOpenChange()</code>; click a button
              above to override from outside.
            </p>
          </Collapsible>
        </div>
      );
    };
    return <Wrapper />;
  },
};

// ── With icon ─────────────────────────────────────────────────────

export const WithIcon: Story = {
  args: {
    title: 'Notifications',
    icon: <Bell size={13} />,
    children: (
      <p style={{ margin: 0, fontSize: 13 }}>
        Configure desktop notifications and sound alerts when the agent is waiting
        for input.
      </p>
    ),
  },
};

// ── With summaryExtra slot ────────────────────────────────────────

export const WithSummaryExtra: Story = {
  args: {
    title: 'Pending edits',
    icon: <Settings size={13} />,
    summaryExtra: (
      <span
        style={{
          padding: '2px 8px',
          borderRadius: 999,
          background: 'rgba(152, 195, 121, 0.18)',
          color: '#98c379',
          fontWeight: 600,
          fontSize: 11,
        }}
      >
        3 changes
      </span>
    ),
    defaultOpen: true,
    children: (
      <ul style={{ margin: 0, paddingLeft: 18, fontSize: 13, lineHeight: 1.7 }}>
        <li>pkg/agent/foo.go — add helper</li>
        <li>pkg/agent/bar.go — rename type</li>
        <li>webui/src/index.tsx — wire component</li>
      </ul>
    ),
  },
};

// ── Flush variant (settings-section style) ───────────────────────

export const Flush: Story = {
  args: {
    title: 'Commit message generation',
    variant: 'flush',
    children: (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, fontSize: 13 }}>
        <p style={{ margin: 0, color: '#9aa3b2' }}>
          Configure which provider and model to use for generating commit messages.
        </p>
        <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <span style={{ fontWeight: 600 }}>Provider</span>
          <select
            style={{
              padding: '6px 8px',
              borderRadius: 6,
              border: '1px solid #555',
              background: '#1f1f23',
              color: '#fff',
            }}
          >
            <option>Default (inherit from main agent)</option>
            <option>OpenAI</option>
            <option>Anthropic</option>
          </select>
        </label>
      </div>
    ),
  },
};

// ── Disabled ─────────────────────────────────────────────────────

export const Disabled: Story = {
  args: {
    title: 'Locked section',
    icon: <ChevronDown size={13} />,
    disabled: true,
    children: <p style={{ margin: 0, fontSize: 13 }}>This section is locked.</p>,
  },
};

// ── Long body (spacing demo) ─────────────────────────────────────

export const LongBody: Story = {
  args: {
    title: 'Detailed guidance',
    icon: <Settings size={13} />,
    defaultOpen: true,
    children: (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10, fontSize: 13 }}>
        <p style={{ margin: 0 }}>
          Reasoning for the recommendation: the agent spent 24 tool calls confirming
          the hypothesis before proposing the change. The minimal patch reduces
          surface area while keeping backward-compatible behaviour for existing
          callers that use the legacy <code>ctx</code> parameter.
        </p>
        <p style={{ margin: 0 }}>
          Trade-offs considered: we could have migrated every call site in one
          sweeping change, but that would block review on a more invasive PR. The
          two-phase migration lets consumers migrate at their own pace.
        </p>
        <p style={{ margin: 0 }}>
          Risks: the <code>shim</code> function still dispatches via the legacy
          router, which adds one extra hop. Acceptable for now; revisit after
          the deprecation window closes.
        </p>
      </div>
    ),
  },
};
