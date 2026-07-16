/**
 * Cross-tab sync via BroadcastChannel — editor side.
 *
 * Listens for task_update messages from the dashboard's WebSocket so the
 * editor's task list refreshes in real time without its own WS connection.
 *
 * Also publishes file_change and session_change events so the dashboard
 * can know what the editor is doing (if it ever needs to).
 *
 * Same channel name + message shape as the platform dashboard's crossTabSync.ts.
 */

const CHANNEL_NAME = 'sprout-foundry-sync';

export type SyncSource = 'dashboard' | 'editor';

export type SyncMessageType =
  | 'task_update'
  | 'file_change'
  | 'usage_update'
  | 'session_change';

export interface SyncMessage {
  source: SyncSource;
  type: SyncMessageType;
  data: Record<string, unknown>;
  timestamp: number;
}

type SyncHandler = (msg: SyncMessage) => void;

class CrossTabSync {
  private channel: BroadcastChannel | null = null;
  private handlers = new Set<SyncHandler>();
  private source: SyncSource;

  constructor(source: SyncSource) {
    this.source = source;
    if (typeof BroadcastChannel !== 'undefined') {
      this.channel = new BroadcastChannel(CHANNEL_NAME);
      this.channel.onmessage = (event: MessageEvent) => {
        const msg = event.data as SyncMessage;
        if (!msg || msg.source === this.source) return;
        this.handlers.forEach((h) => h(msg));
      };
    }
  }

  publish(type: SyncMessageType, data: Record<string, unknown>): void {
    if (!this.channel) return;
    this.channel.postMessage({
      source: this.source,
      type,
      data,
      timestamp: Date.now(),
    });
  }

  subscribe(handler: SyncHandler): () => void {
    this.handlers.add(handler);
    return () => { this.handlers.delete(handler); };
  }

  close(): void {
    this.channel?.close();
    this.channel = null;
    this.handlers.clear();
  }
}

let instance: CrossTabSync | null = null;

export function getEditorSync(): CrossTabSync {
  if (!instance) instance = new CrossTabSync('editor');
  return instance;
}
