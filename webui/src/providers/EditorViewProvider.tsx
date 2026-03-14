import React from 'react';
import FileTree from '../components/FileTree';
import { ContentProvider, ProviderContext, SidebarSection, Action, ActionResult } from './types';

export class EditorViewProvider implements ContentProvider {
  readonly id = 'editor-view';
  readonly viewType = 'editor';
  readonly name = 'Editor View Provider';

  getSections(_context: ProviderContext): SidebarSection[] {
    return [
      {
        id: 'workspace-files',
        dataSource: { type: 'state' },
        renderItem: (_data: unknown, ctx: ProviderContext) => (
          <FileTree
            rootPath="."
            onFileSelect={(file) => ctx.onFileClick?.(file.path)}
          />
        ),
        title: () => 'Workspace',
        order: 1
      }
    ];
  }

  handleAction(action: Action, context: ProviderContext): ActionResult {
    switch (action.type) {
      case 'open-file':
        if (context.onFileClick && action.payload?.filePath) {
          context.onFileClick(action.payload.filePath);
          return { success: true };
        }
        return { success: false, error: 'No onFileClick handler' };
      default:
        return { success: false, error: `Unknown action: ${action.type}` };
    }
  }

  cleanup(): void {}
}
