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

/**
 * Thrown when the host's model endpoint cannot be turned into an absolute
 * http(s) URL. The provider config normalizer accepts only absolute URLs, and
 * a config it rejects leaves the provider unregistered, so the turn surfaces
 * this message instead.
 */
export class ModelEndpointError extends Error {
  constructor(endpoint: string) {
    super(
      `The host's model endpoint "${endpoint}" is not usable: expected an absolute http(s) URL or a path such as "/proxy/chat".`,
    );
    this.name = 'ModelEndpointError';
  }
}

/**
 * Resolve a host model endpoint to an absolute URL. An absolute http(s) URL is
 * used as given; a relative one (a host may pass a path such as
 * `/proxy/chat`) is resolved against `base`, the host's API base URL (or the
 * page origin). Anything else throws a `ModelEndpointError`.
 */
export function resolveModelEndpoint(endpoint: string, base: string): string {
  const raw = endpoint.trim();
  let url: URL;
  try {
    if (!raw || /\s/.test(raw)) throw new Error('blank or contains whitespace');
    url = new URL(raw, base);
  } catch {
    throw new ModelEndpointError(endpoint);
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new ModelEndpointError(endpoint);
  return url.toString();
}

/**
 * The in-browser agent's provider config. With no host `modelEndpoint` the
 * managed model is reached through the platform proxy at `apiOrigin`; a
 * relative host endpoint resolves against `endpointBase` (the host's API base,
 * defaulting to `apiOrigin`). Throws a `ModelEndpointError` for an endpoint
 * that cannot be made absolute.
 */
export function platformProviderConfig(
  apiOrigin: string,
  contextWindow: number | undefined,
  modelEndpoint?: string,
  endpointBase: string = apiOrigin,
) {
  return {
    name: 'platform',
    // Must be an absolute URL: the provider config normalizer rejects
    // relative ones, which would leave the provider unregistered.
    endpoint: modelEndpoint ? resolveModelEndpoint(modelEndpoint, endpointBase) : `${apiOrigin}/proxy/chat`,
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
