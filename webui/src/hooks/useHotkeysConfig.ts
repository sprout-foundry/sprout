import { useEffect, useState } from 'react';
import type { ApiService } from '../services/api';

export const useHotkeysConfig = (apiService: ApiService, isConnected: boolean): string | null => {
  const [hotkeysConfigPath, setHotkeysConfigPath] = useState<string | null>(null);

  useEffect(() => {
    if (!isConnected) return;
    apiService
      .getHotkeys()
      .then((config) => {
        if (config.path) setHotkeysConfigPath(config.path);
      })
      .catch((err) => {
        console.warn('Failed to load hotkeys config:', err);
      });
  }, [isConnected, apiService]);

  return hotkeysConfigPath;
};
