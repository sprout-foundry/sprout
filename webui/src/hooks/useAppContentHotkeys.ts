import { useCallback, useEffect } from 'react';
import type { MutableRefObject, Dispatch, SetStateAction } from 'react';
import { supportsLocalTerminal } from '../config/mode';
import type { EditorBuffer } from '../types/editor';
import type { ViewType } from '../types/app';

export interface UseAppContentHotkeysParams {
  activeBufferId: string | null;
  buffersRef: MutableRefObject<Map<string, EditorBuffer>>;
  onSidebarToggle: () => void;
  onTerminalExpandedChange: (expanded: boolean) => void;
  isTerminalExpanded: boolean;
  openWorkspaceBuffer: (options: {
    kind: 'file' | 'chat';
    path: string;
    title: string;
    ext?: string;
    isClosable?: boolean;
    isPinned?: boolean;
    metadata?: Record<string, unknown>;
  }) => void;
  onViewChange: (view: ViewType) => void;
  handlePrimaryViewChange: (view: 'chat' | 'editor' | 'git') => void;
  closeBuffer: (bufferId: string) => void;
  setCommandPaletteMode: (mode: 'all' | 'files' | 'symbols') => void;
  setIsCommandPaletteOpen: Dispatch<SetStateAction<boolean>>;
  hotkeysConfigPath: string | null;
  openFile: (file: { path: string; name: string; isDir: boolean; size: number; modified: number; ext: string }) => void;
}

export interface UseAppContentHotkeysReturn {
  handleOpenHotkeysConfig: () => void;
}

export const useAppContentHotkeys = ({
  activeBufferId,
  buffersRef,
  onSidebarToggle,
  onTerminalExpandedChange,
  isTerminalExpanded,
  openWorkspaceBuffer,
  onViewChange,
  handlePrimaryViewChange,
  closeBuffer,
  setCommandPaletteMode,
  setIsCommandPaletteOpen,
  hotkeysConfigPath,
  openFile,
}: UseAppContentHotkeysParams): UseAppContentHotkeysReturn => {
  // Handler to open hotkeys config in editor
  const handleOpenHotkeysConfig = useCallback(() => {
    if (!hotkeysConfigPath) return;
    const fileName = hotkeysConfigPath.split('/').pop() || 'hotkeys.json';
    const extensionIndex = fileName.lastIndexOf('.');
    const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';

    openFile({
      path: hotkeysConfigPath,
      name: fileName,
      isDir: false,
      size: 0,
      modified: 0,
      ext: fileExt,
    });

    // Ensure we're in editor view
    onViewChange('editor');
    setIsCommandPaletteOpen(false);
  }, [hotkeysConfigPath, openFile, onViewChange, setIsCommandPaletteOpen]);

  // Listen for hotkey custom events
  useEffect(() => {
    const handleHotkey = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (!detail?.commandId) return;

      switch (detail.commandId) {
        case 'command_palette':
          setCommandPaletteMode('all');
          setIsCommandPaletteOpen((prev) => !prev);
          break;
        case 'new_file':
          openWorkspaceBuffer({
            kind: 'file',
            path: `__workspace/untitled-${Date.now()}`,
            title: 'Untitled',
            ext: '',
            isClosable: true,
          });
          onViewChange('editor');
          break;
        case 'toggle_sidebar':
          onSidebarToggle();
          break;
        case 'toggle_terminal':
          if (supportsLocalTerminal) {
            onTerminalExpandedChange(!isTerminalExpanded);
          }
          break;
        case 'toggle_explorer': {
          const activeBuffer = activeBufferId ? buffersRef.current.get(activeBufferId) : null;
          const filePath =
            activeBuffer?.file?.path && !activeBuffer.file.isDir && activeBuffer.kind === 'file'
              ? activeBuffer.file.path
              : null;

          window.dispatchEvent(new CustomEvent('sprout:reveal-in-explorer', { detail: { path: filePath ?? '' } }));
          break;
        }
        case 'quick_open':
          setCommandPaletteMode('files');
          setIsCommandPaletteOpen(true);
          break;
        case 'switch_to_chat':
          handlePrimaryViewChange('chat');
          break;
        case 'switch_to_editor':
          handlePrimaryViewChange('editor');
          break;
        case 'switch_to_git':
          handlePrimaryViewChange('git');
          break;
        case 'close_editor':
          if (activeBufferId) {
            closeBuffer(activeBufferId);
          }
          break;
        case 'editor_workspace_symbol':
          document.dispatchEvent(new CustomEvent('editor-go-to-workspace-symbol'));
          break;
        case 'editor_goto_symbol':
          setCommandPaletteMode('symbols');
          setIsCommandPaletteOpen(true);
          break;
      }
    };

    window.addEventListener('sprout:hotkey', handleHotkey);
    return () => window.removeEventListener('sprout:hotkey', handleHotkey);
  }, [
    activeBufferId,
    onSidebarToggle,
    onTerminalExpandedChange,
    isTerminalExpanded,
    openWorkspaceBuffer,
    onViewChange,
    handlePrimaryViewChange,
    closeBuffer,
    setCommandPaletteMode,
    setIsCommandPaletteOpen,
  ]);

  // Listen for open hotkeys config event — the modal listens for the
  // legacy `sprout:open-hotkeys-config` event (Help menu / Welcome tab),
  // and the explicit "Edit Keyboard Shortcuts (JSON)" button dispatches
  // a dedicated event so it doesn't accidentally open the modal.
  useEffect(() => {
    const handleOpenHotkeys = () => {
      handleOpenHotkeysConfig();
    };
    window.addEventListener('sprout:open-hotkeys-json', handleOpenHotkeys);
    return () => window.removeEventListener('sprout:open-hotkeys-json', handleOpenHotkeys);
  }, [handleOpenHotkeysConfig]);

  return { handleOpenHotkeysConfig };
};
