import { render } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

const seen = vi.hoisted(() => ({ stats: undefined as unknown }));

vi.mock('../contexts/EditorManagerContext', () => ({
  useEditorManager: () => ({
    panes: [{ id: 'pane-2', bufferId: 'buf-b' }],
    buffers: new Map([['buf-b', { id: 'buf-b', kind: 'chat', metadata: { chatId: 'chat-b' } }]]),
  }),
}));
vi.mock('./ChatView', () => ({
  default: (props: { stats?: unknown }) => {
    seen.stats = props.stats;
    return null;
  },
}));
vi.mock('./CompareTab', () => ({ default: () => null }));
vi.mock('./DiffWorkspaceTab', () => ({ default: () => null }));
vi.mock('./EditorPane', () => ({ default: () => null }));

import WorkspacePane from './WorkspacePane';

describe('inactive chat pane metrics', () => {
  it("shows the chat's model before it has reported any metrics", () => {
    render(
      <WorkspacePane
        paneId="pane-2"
        activeChatId="chat-a"
        perChatCache={{}}
        chatSessions={[{ id: 'chat-b', provider: 'deepinfra', model: 'flash' }]}
        chatProps={{ stats: { provider: 'other', total_tokens: 999 } } as never}
        reviewProps={{} as never}
        diffState={{} as never}
      />,
    );
    expect(seen.stats).toEqual({ provider: 'deepinfra', model: 'flash' });
  });

  it("falls back to the default provider and model, without the active chat's figures", () => {
    render(
      <WorkspacePane
        paneId="pane-2"
        activeChatId="chat-a"
        perChatCache={{}}
        chatSessions={[{ id: 'chat-b' }]}
        chatProps={{ stats: { provider: 'deepinfra', model: 'flash', total_tokens: 999, total_cost: 1.5 } } as never}
        reviewProps={{} as never}
        diffState={{} as never}
      />,
    );
    expect(seen.stats).toEqual({ provider: 'deepinfra', model: 'flash' });
  });
});
