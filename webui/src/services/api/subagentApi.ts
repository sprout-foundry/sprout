/**
 * Subagent CRUD-ish endpoints. Currently a single op (cancel) but kept in
 * its own module so the surface can grow alongside SP-059 without padding
 * the more general apiService.
 */

export interface CancelSubagentResponse {
  accepted?: boolean;
  status?: string;
  already_completed?: boolean;
  id: string;
  mode?: string;
  timestamp?: number;
}

/**
 * Cancel a single running subagent by its runner ID (`subagent-<unixnano>`
 * for serial calls, `task-N` for parallel ones).
 *
 * Returns the server payload on success. 200 with `already_completed: true`
 * is treated as a normal outcome — the UI should still remove the row.
 *
 * Throws when the request itself failed (network, 4xx/5xx that isn't 200).
 */
export async function cancelSubagent(
  fetchFn: typeof fetch,
  subagentId: string,
  chatId?: string,
): Promise<CancelSubagentResponse> {
  const params = chatId ? `?chat_id=${encodeURIComponent(chatId)}` : '';
  const response = await fetchFn(`/api/subagent/${encodeURIComponent(subagentId)}/cancel${params}`, {
    method: 'POST',
  });
  if (!response.ok) {
    throw new Error(`Failed to cancel subagent ${subagentId}: ${response.status} ${response.statusText}`);
  }
  return (await response.json()) as CancelSubagentResponse;
}
