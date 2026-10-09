/**
 * The in-browser agent's provider: an OpenAI-compatible custom provider that
 * routes to the platform's managed model.
 */

/**
 * Window used when the platform doesn't report one. Above the 132K line where
 * the agent switches to Low-Context Mode: that mode is for models known to have
 * a small window, not a default for an unknown one.
 */
export const UNREPORTED_CONTEXT_WINDOW = 200_000;

let reported: number | undefined;
let reportedVision: boolean | undefined;

/**
 * Asks the platform for the managed model's context window (context_length on
 * /proxy/chat/models) and keeps it for the agent's provider config. Called
 * once when the in-browser agent is wired up; the window changes only with a
 * model redeploy, which a page load picks up. Unreported or unreachable leaves
 * it unknown.
 */
export async function loadManagedContextWindow(apiOrigin: string): Promise<void> {
  try {
    const resp = await fetch(`${apiOrigin}/proxy/chat/models`, { credentials: 'include' });
    if (!resp.ok) return;
    const body = (await resp.json()) as {
      data?: Array<{ id?: string; context_length?: unknown; supports_vision?: unknown }>;
    };
    const managed = body.data?.find((m) => m.id === 'managed');
    const window = managed?.context_length;
    if (typeof window === 'number' && Number.isFinite(window) && window > 0) reported = window;
    if (typeof managed?.supports_vision === 'boolean') reportedVision = managed.supports_vision;
  } catch {
    // Unknown: the provider config uses UNREPORTED_CONTEXT_WINDOW.
  }
}

/** The managed model's reported context window, if the platform gave one. */
export function reportedManagedContextWindow(): number | undefined {
  return reported;
}

/** Forget the reported window (tests). */
export function resetManagedContextWindow(): void {
  reported = undefined;
  reportedVision = undefined;
}

export function platformProviderConfig(apiOrigin: string, contextWindow: number | undefined, modelEndpoint?: string) {
  return {
    name: 'platform',
    // Must be an absolute URL: the provider config normalizer rejects
    // relative ones. A host that supplies its own endpoint (the agent backend's
    // model endpoint) overrides the platform proxy path; absent, the managed
    // model is reached through the platform's own proxy.
    endpoint: modelEndpoint || `${apiOrigin}/proxy/chat`,
    model_name: 'managed',
    context_size: contextWindow ?? UNREPORTED_CONTEXT_WINDOW,
    requires_api_key: false,
    // Images go to the model unless the platform says it cannot take them.
    // A model that rejects them anyway is learned on first contact: the
    // provider strips the images, retries text-only and remembers.
    supports_vision: reportedVision ?? true,
    // Streams then end with a usage chunk (the platform forwards it only when
    // asked), so the footer's token count isn't stuck at 0.
    include_usage: true,
    message_conversion: {
      include_tool_call_id: true,
      convert_tool_role_to_user: false,
    },
  };
}
