/**
 * Chat View Provider
 *
 * Data-driven provider for Chat view sidebar content
 */

import { ContentProvider, ProviderContext, SidebarSection, Action, ActionResult } from './types';

export class ChatViewProvider implements ContentProvider {
  readonly id = 'chat-view';
  readonly viewType = 'chat';
  readonly name = 'Chat View Provider';

  // extractFilePath removed - activity logging moved to Activity tab in sidebar

  getSections(context: ProviderContext): SidebarSection[] {
    return [
      {
        id: 'chat-stats',
        dataSource: {
          type: 'state',
          transform: (data: ProviderContext) => ({
            queryCount: data.stats?.queryCount || 0,
            isConnected: data.isConnected
          })
        },
        renderItem: (data: any, ctx: ProviderContext) => {
          const status = ctx.isConnected ? 'connected' : 'disconnected';
          return (
            <div className="stats">
              <div className="stat-item">
                <span className={`value status ${status}`}>
                  {status === 'connected' ? 'Connected' : 'Disconnected'}
                </span>
              </div>
            </div>
          );
        },
        title: () => 'Chat Status',
        order: 1
      },
      {
        id: 'chat-history',
        dataSource: {
          type: 'state'
        },
        renderItem: () => (
          <div className="history-shortcut">
            <p className="history-shortcut-text">Open revision history and rollback points.</p>
            <button
              type="button"
              className="action-btn"
              onClick={() => {
                if (typeof window !== 'undefined') {
                  window.dispatchEvent(new CustomEvent('ledit:open-revision-history'));
                }
              }}
            >
              Revision History
            </button>
          </div>
        ),
        title: () => 'History',
        order: 2
      }
    ];
  }

  handleAction(action: Action, context: ProviderContext): ActionResult {
    switch (action.type) {
      case 'refresh-files':
        return { success: true };
      default:
        return { success: false, error: `Unknown action: ${action.type}` };
    }
  }

  cleanup(): void {}
}
