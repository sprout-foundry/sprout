import { WebSocketService } from './websocket';

export interface UIDropdownItem {
  id: string;
  display: string;
  search_text: string;
  value: any;
}

export interface UIDropdownOptions {
  prompt: string;
  search_prompt?: string;
  max_height?: number;
  show_counts?: boolean;
}

export interface UIQuickOption {
  label: string;
  value: string;
  hotkey?: string;
}

export interface UIPromptRequest {
  id: string;
  type: 'dropdown' | 'quick_prompt' | 'input';
  prompt: string;
  dropdown_options?: UIDropdownOptions;
  dropdown_items?: UIDropdownItem[];
  quick_options?: UIQuickOption[];
  horizontal?: boolean;
  default_value?: string;
  mask?: boolean;
  context?: string;
}

export interface UIPromptResponse {
  id: string;
  type: string;
  selected?: any;
  value?: string;
  confirmed?: boolean;
  cancelled?: boolean;
}

export interface UIProgressStart {
  id: string;
  message: string;
  current?: number;
  total?: number;
}

export interface UIProgressUpdate {
  id: string;
  message?: string;
  current?: number;
  total?: number;
  done?: boolean;
}

class UIService {
  private static instance: UIService;
  private wsService: WebSocketService;
  private pendingPrompts: Map<string, {
    resolve: (value: any) => void;
    reject: (error: Error) => void;
    type: string;
  }> = new Map();

  private constructor() {
    this.wsService = WebSocketService.getInstance();
    this.setupEventHandlers();
  }

  static getInstance(): UIService {
    if (!UIService.instance) {
      UIService.instance = new UIService();
    }
    return UIService.instance;
  }

  private setupEventHandlers() {
    this.wsService.onEvent((event) => {
      if (event.type === 'ui_prompt_response') {
        this.handlePromptResponse(event.data as UIPromptResponse);
      }
    });
  }

  private handlePromptResponse(response: UIPromptResponse) {
    const pending = this.pendingPrompts.get(response.id);
    if (!pending) return;

    this.pendingPrompts.delete(response.id);

    if (response.cancelled) {
      pending.reject(new Error('User cancelled'));
    } else {
      switch (pending.type) {
        case 'dropdown':
          if (response.selected !== undefined) {
            pending.resolve(response.selected);
          } else {
            pending.reject(new Error('No selection made'));
          }
          break;
        case 'quick_prompt':
          if (response.selected !== undefined) {
            pending.resolve(response.selected);
          } else {
            pending.reject(new Error('No selection made'));
          }
          break;
        case 'input':
          if (response.value !== undefined) {
            pending.resolve(response.value);
          } else {
            pending.reject(new Error('No input provided'));
          }
          break;
        default:
          pending.reject(new Error(`Unknown prompt type: ${pending.type}`));
      }
    }
  }

  async showDropdown(
    items: UIDropdownItem[],
    options: UIDropdownOptions
  ): Promise<any> {
    return new Promise((resolve, reject) => {
      const id = `dropdown-${Date.now()}-${Math.random()}`;

      this.pendingPrompts.set(id, { resolve, reject, type: 'dropdown' });

      // Send prompt request to backend which will forward to UI
      const request: UIPromptRequest = {
        id,
        type: 'dropdown',
        prompt: options.prompt,
        dropdown_options: options,
        dropdown_items: items
      };

      // In a real implementation, this would be sent via WebSocket
      // For now, we'll emit a custom event that components can listen to
      this.emitUIEvent('show_dropdown', request);
    });
  }

  async showQuickPrompt(
    prompt: string,
    options: UIQuickOption[],
    horizontal: boolean = true
  ): Promise<any> {
    return new Promise((resolve, reject) => {
      const id = `quick-${Date.now()}-${Math.random()}`;

      this.pendingPrompts.set(id, { resolve, reject, type: 'quick_prompt' });

      const request: UIPromptRequest = {
        id,
        type: 'quick_prompt',
        prompt,
        quick_options: options,
        horizontal
      };

      this.emitUIEvent('show_quick_prompt', request);
    });
  }

  async showInput(
    prompt: string,
    defaultValue: string = '',
    mask: boolean = false
  ): Promise<string> {
    return new Promise((resolve, reject) => {
      const id = `input-${Date.now()}-${Math.random()}`;

      this.pendingPrompts.set(id, { resolve, reject, type: 'input' });

      const request: UIPromptRequest = {
        id,
        type: 'input',
        prompt,
        default_value: defaultValue,
        mask
      };

      this.emitUIEvent('show_input', request);
    });
  }

  startProgress(id: string, message: string, current: number = 0, total: number = 0) {
    const progress: UIProgressStart = {
      id,
      message,
      current,
      total
    };

    this.emitUIEvent('progress_start', progress);
  }

  updateProgress(id: string, message?: string, current?: number, total?: number) {
    const progress: UIProgressUpdate = {
      id,
      message,
      current,
      total
    };

    this.emitUIEvent('progress_update', progress);
  }

  completeProgress(id: string) {
    const progress: UIProgressUpdate = {
      id,
      done: true
    };

    this.emitUIEvent('progress_update', progress);
  }

  private emitUIEvent(type: string, data: any) {
    // Emit custom DOM event for React components to listen to
    const event = new CustomEvent(`ui:${type}`, { detail: data });
    document.dispatchEvent(event);
  }

  // Method to send response back to pending prompt
  respondToPrompt(response: UIPromptResponse) {
    // Send response via WebSocket to backend
    this.wsService.sendEvent({
      type: 'ui_prompt_response',
      data: response
    });
  }
}

export const uiService = UIService.getInstance();