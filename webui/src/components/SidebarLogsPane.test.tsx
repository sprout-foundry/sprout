import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import SidebarLogsPane from './SidebarLogsPane';
import type { ProviderLogEntry } from '../providers/types';

const entry = (
  type: string,
  data: Record<string, unknown>,
  level: 'info' | 'error' | 'warning' | 'success' = 'info',
): ProviderLogEntry => ({
  id: `${type}-${Math.random()}`,
  type,
  timestamp: new Date('2026-09-03T15:34:18Z'),
  data,
  level,
  category: 'system',
});

describe('SidebarLogsPane error rendering', () => {
  it('shows the error cause when the payload carries one', () => {
    render(
      <SidebarLogsPane
        logs={[entry('error', { message: 'chat failed', error: 'dial tcp: provider unreachable' }, 'error')]}
      />,
    );
    expect(screen.getByText(/chat failed — dial tcp: provider unreachable/)).toBeTruthy();
  });

  it('shows only the label when no cause is present', () => {
    render(<SidebarLogsPane logs={[entry('error', { message: 'boom' }, 'error')]} />);
    expect(screen.getByText(/Error: boom/)).toBeTruthy();
  });

  it('renders model and provider from the metrics payload', () => {
    render(<SidebarLogsPane logs={[entry('metrics_update', { model: 'qwen3.6-27b', provider: 'ai-worker' })]} />);
    expect(screen.getByText(/Model: qwen3\.6-27b \| Provider: ai-worker/)).toBeTruthy();
  });
});
