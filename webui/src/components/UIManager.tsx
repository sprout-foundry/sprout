import React, { useState, useEffect } from 'react';
import Dropdown, { DropdownItem } from './Dropdown';
import QuickPrompt from './QuickPrompt';
import Progress from './Progress';
import FileBrowser from './FileBrowser';
import { uiService, UIDropdownItem, UIDropdownOptions, UIQuickOption } from '../services/ui';

interface UIManagerProps {
  children: React.ReactNode;
}

const UIManager: React.FC<UIManagerProps> = ({ children }) => {
  const [dropdownState, setDropdownState] = useState<{
    isOpen: boolean;
    items: DropdownItem[];
    options: UIDropdownOptions;
    resolve?: (value: any) => void;
  }>({
    isOpen: false,
    items: [],
    options: { prompt: '' }
  });

  const [quickPromptState, setQuickPromptState] = useState<{
    isOpen: boolean;
    prompt: string;
    options: UIQuickOption[];
    horizontal: boolean;
    resolve?: (value: any) => void;
  }>({
    isOpen: false,
    prompt: '',
    options: [],
    horizontal: true
  });

  const [progressItems, setProgressItems] = useState<Map<string, {
    message: string;
    current: number;
    total: number;
    done: boolean;
    startTime: number;
  }>>(new Map());

  const [fileBrowserState, setFileBrowserState] = useState<{
    isOpen: boolean;
    initialPath?: string;
    allowDirectories?: boolean;
    allowedExtensions?: string[];
    resolve?: (file: any) => void;
  }>({
    isOpen: false,
    allowDirectories: false,
    allowedExtensions: []
  });

  useEffect(() => {
    // Listen for UI events from the service
    const handleDropdown = (event: CustomEvent) => {
      const { id, prompt, dropdown_items: items, dropdown_options: options } = event.detail;

      // Convert UIDropdownItem to DropdownItem
      const convertedItems: DropdownItem[] = (items || []).map((uiItem: UIDropdownItem) => ({
        id: uiItem.id,
        display: uiItem.display,
        searchText: uiItem.search_text,
        value: uiItem.value
      }));

      setDropdownState({
        isOpen: true,
        items: convertedItems,
        options: options || { prompt },
        resolve: (selected) => {
          uiService.respondToPrompt({
            id,
            type: 'dropdown',
            selected,
            cancelled: false
          });
        }
      });
    };

    const handleQuickPrompt = (event: CustomEvent) => {
      const { id, prompt, quick_options: options, horizontal } = event.detail;

      setQuickPromptState({
        isOpen: true,
        prompt,
        options: options || [],
        horizontal: horizontal !== false,
        resolve: (selected) => {
          uiService.respondToPrompt({
            id,
            type: 'quick_prompt',
            selected,
            cancelled: false
          });
        }
      });
    };

    const handleProgressStart = (event: CustomEvent) => {
      const { id, message, current = 0, total = 0 } = event.detail;

      setProgressItems(prev => new Map(prev.set(id, {
        message,
        current,
        total,
        done: false,
        startTime: Date.now()
      })));
    };

    const handleProgressUpdate = (event: CustomEvent) => {
      const { id, message, current, total, done } = event.detail;

      setProgressItems(prev => {
        const newMap = new Map(prev);
        const existing = newMap.get(id);

        if (existing) {
          newMap.set(id, {
            ...existing,
            ...(message !== undefined && { message }),
            ...(current !== undefined && { current }),
            ...(total !== undefined && { total }),
            ...(done !== undefined && { done })
          });
        }

        return newMap;
      });
    };

    document.addEventListener('ui:show_dropdown', handleDropdown as EventListener);
    document.addEventListener('ui:show_quick_prompt', handleQuickPrompt as EventListener);
    document.addEventListener('ui:progress_start', handleProgressStart as EventListener);
    document.addEventListener('ui:progress_update', handleProgressUpdate as EventListener);

    return () => {
      document.removeEventListener('ui:show_dropdown', handleDropdown as EventListener);
      document.removeEventListener('ui:show_quick_prompt', handleQuickPrompt as EventListener);
      document.removeEventListener('ui:progress_start', handleProgressStart as EventListener);
      document.removeEventListener('ui:progress_update', handleProgressUpdate as EventListener);
    };
  }, []);

  const handleDropdownSelect = (item: DropdownItem) => {
    if (dropdownState.resolve) {
      dropdownState.resolve(item.value);
    }
    setDropdownState(prev => ({ ...prev, isOpen: false }));
  };

  const handleDropdownCancel = () => {
    setDropdownState(prev => ({ ...prev, isOpen: false }));
  };

  const handleQuickPromptSelect = (option: UIQuickOption) => {
    if (quickPromptState.resolve) {
      quickPromptState.resolve(option.value);
    }
    setQuickPromptState(prev => ({ ...prev, isOpen: false }));
  };

  const handleQuickPromptCancel = () => {
    setQuickPromptState(prev => ({ ...prev, isOpen: false }));
  };

  const handleFileBrowserSelect = (file: any) => {
    if (fileBrowserState.resolve) {
      fileBrowserState.resolve(file);
    }
    setFileBrowserState(prev => ({ ...prev, isOpen: false }));
  };

  const handleFileBrowserCancel = () => {
    setFileBrowserState(prev => ({ ...prev, isOpen: false }));
  };

  // Clean up completed progress items after a delay
  useEffect(() => {
    const interval = setInterval(() => {
      setProgressItems(prev => {
        const newMap = new Map();
        let hasChanges = false;

        prev.forEach((item, id) => {
          if (item.done && Date.now() - item.startTime > 3000) {
            hasChanges = true;
          } else {
            newMap.set(id, item);
          }
        });

        return hasChanges ? newMap : prev;
      });
    }, 1000);

    return () => clearInterval(interval);
  }, []);

  return (
    <>
      {children}

      {/* Dropdown Modal */}
      <Dropdown
        isOpen={dropdownState.isOpen}
        items={dropdownState.items}
        options={dropdownState.options}
        onSelect={handleDropdownSelect}
        onCancel={handleDropdownCancel}
      />

      {/* Quick Prompt Modal */}
      <QuickPrompt
        isOpen={quickPromptState.isOpen}
        prompt={quickPromptState.prompt}
        options={quickPromptState.options}
        horizontal={quickPromptState.horizontal}
        onSelect={handleQuickPromptSelect}
        onCancel={handleQuickPromptCancel}
      />

      {/* Progress Indicators */}
      <Progress
        items={Array.from(progressItems.entries()).map(([id, item]) => ({
          id,
          message: item.message,
          current: item.current,
          total: item.total,
          done: item.done,
          startTime: item.startTime
        }))}
      />

      {/* File Browser Modal */}
      <FileBrowser
        isOpen={fileBrowserState.isOpen}
        initialPath={fileBrowserState.initialPath}
        allowDirectories={fileBrowserState.allowDirectories}
        allowedExtensions={fileBrowserState.allowedExtensions}
        onSelect={handleFileBrowserSelect}
        onCancel={handleFileBrowserCancel}
      />
    </>
  );
};

export default UIManager;