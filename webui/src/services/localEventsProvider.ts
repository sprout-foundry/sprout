import type { EventsProvider, SproutEvent, SproutEventCallback } from '../types/events';
import { WebSocketService } from './websocket';

/**
 * LocalEventsProvider — implements EventsProvider by delegating to the
 * existing WebSocketService singleton. This is the concrete transport
 * used when running against the local Go backend.
 */
export class LocalEventsProvider implements EventsProvider {
  private ws(): WebSocketService {
    return WebSocketService.getInstance();
  }

  connect(): void {
    this.ws()
      .connect()
      .catch(() => {});
  }

  disconnect(): void {
    this.ws().disconnect();
  }

  onEvent(callback: SproutEventCallback): void {
    this.ws().onEvent(callback);
  }

  removeEvent(callback: SproutEventCallback): void {
    this.ws().removeEvent(callback);
  }

  sendEvent(event: SproutEvent): void {
    this.ws().sendEvent(event);
  }

  isConnected(): boolean {
    return this.ws().isConnected();
  }

  onReconnect(callback: (() => void) | null): void {
    this.ws().onReconnect(callback);
  }

  freeze(): void {
    this.ws().freeze();
  }

  resume(): void {
    this.ws().resume();
  }

  resetAndReconnect(): void {
    this.ws().resetAndReconnect();
  }

  getQueuedMessageCount(): number {
    return this.ws().getQueuedMessageCount();
  }

  flushQueuedMessages(): number {
    return this.ws().flushQueuedMessages();
  }
}
