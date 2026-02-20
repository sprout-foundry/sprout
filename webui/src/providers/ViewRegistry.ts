/**
 * View Registry
 *
 * Central registry for managing content providers for different views.
 * Providers define data sources, not hardcoded content.
 */

import { ContentProvider, ProviderContext, SidebarSection, Action, ActionResult } from './types';

class ViewRegistry {
  private providers = new Map<string, ContentProvider>();
  private context: ProviderContext | null = null;

  register(provider: ContentProvider): void {
    this.providers.set(provider.viewType, provider);
    console.log(`Registered provider: ${provider.name} for view: ${provider.viewType}`);
  }

  unregister(viewType: string): void {
    const provider = this.providers.get(viewType);
    if (provider?.cleanup) {
      provider.cleanup();
    }
    this.providers.delete(viewType);
  }

  getProvider(viewType: string): ContentProvider | undefined {
    return this.providers.get(viewType);
  }

  setContext(context: ProviderContext): void {
    this.context = context;
  }

  getContext(): ProviderContext | null {
    return this.context;
  }

  /**
   * Get sidebar sections for a view type
   * Returns section definitions with data sources (not hardcoded content)
   * @param viewType - View type
   * @returns Array of section definitions
   */
  getSections(viewType: string): SidebarSection[] {
    const provider = this.providers.get(viewType);
    if (!provider || !this.context) {
      return [];
    }
    return provider.getSections(this.context);
  }

  handleAction(viewType: string, action: Action): ActionResult {
    const provider = this.providers.get(viewType);
    if (!provider || !provider.handleAction || !this.context) {
      return {
        success: false,
        error: `No handler for action "${action.type}" in view "${viewType}"`
      };
    }
    return provider.handleAction(action, this.context);
  }

  clear(): void {
    this.providers.forEach((provider) => {
      if (provider.cleanup) {
        provider.cleanup();
      }
    });
    this.providers.clear();
  }
}

export const viewRegistry = new ViewRegistry();
