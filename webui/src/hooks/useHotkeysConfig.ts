import { useCallback, useState, useEffect } from 'react';
import { type ApiService } from '../services/api';
import { parseFilePath } from '../utils/filePath';
import { notificationBus } from '../services/notificationBus';

interface UseHotkeysConfigOptions {
  isConnected: boolean;
  apiService: ApiService;
  openFile: (file: { path: string; name: string; isDir: boolean; size: number; modified: number; ext: string }) => void;
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  onCloseCommandPalette: () => void;
}

/**
 * Loads the hotkeys config path from the API and exposes a handler to
 * open the hotkeys config file in the editor. Also listens for
 * `ledit:open-hotkeys-config` custom events.
 */
export function useHotkeysConfig({
  isConnected,
  apiService,
  openFile,
  onViewChange,
  onCloseCommandPalette,
}: UseHotkeysConfigOptions): void {
  const [hotkeysConfigPath, setHotkeysConfigPath] = useState<string | null>(null);

  // Load hotkeys config path when connected
  useEffect(() => {
    if (!isConnected) return;
    apiService
      .getHotkeys()
      .then((config) => {
        if (config.path) setHotkeysConfigPath(config.path);
      })
      .catch((err) => {
        notificationBus.notify('warning', 'Hotkeys', 'Failed to load hotkeys config: ' + String(err));
      });
  }, [isConnected, apiService]);

  const handleOpenHotkeysConfig = useCallback(() => {
    if (!hotkeysConfigPath) return;
    const { fileName, fileExt } = parseFilePath(hotkeysConfigPath);
    openFile({
      path: hotkeysConfigPath,
      name: fileName,
      isDir: false,
      size: 0,
      modified: 0,
      ext: fileExt,
    });
    onViewChange('editor');
    onCloseCommandPalette();
  }, [hotkeysConfigPath, openFile, onViewChange, onCloseCommandPalette]);

  // Listen for external requests to open the hotkeys config
  useEffect(() => {
    const handler = () => {
      handleOpenHotkeysConfig();
    };
    window.addEventListener('ledit:open-hotkeys-config', handler);
    return () => window.removeEventListener('ledit:open-hotkeys-config', handler);
  }, [handleOpenHotkeysConfig]);
}
