import type { Message } from '@sprout/ui';

/**
 * A steer bubble is pending from when the user sends it mid-run until the
 * server reports it reached the model (steer_delivered). Until then the run
 * is still producing its answer to the earlier input, so that output belongs
 * above the steer.
 */
type SteerBubble = Message & { steerPending?: boolean };

export function pendingSteerBubble(id: string, content: string): Message {
  const bubble: SteerBubble = { id, type: 'user', content, timestamp: new Date(), steerPending: true };
  return bubble;
}

export function markSteerPending(message: Message): Message {
  const bubble: SteerBubble = { ...message, steerPending: true };
  return bubble;
}

export function isPendingSteer(message: Message): boolean {
  return (message as SteerBubble).steerPending === true;
}

/**
 * Applies update to the transcript without its trailing pending steers, then
 * puts them back at the end — so run output that appends to "the last
 * message" lands on the run's answer rather than after a steer the model
 * hasn't seen.
 */
export function abovePendingSteers(messages: Message[], update: (body: Message[]) => Message[]): Message[] {
  let cut = messages.length;
  while (cut > 0 && isPendingSteer(messages[cut - 1])) cut -= 1;
  if (cut === messages.length) return update(messages);
  return [...update(messages.slice(0, cut)), ...messages.slice(cut)];
}

/**
 * The oldest pending steer reached the model. Pending steers always trail
 * the transcript, so it already sits right after everything the run said
 * before it; it only stops being pending, and later output follows it.
 */
export function deliverOldestSteer(messages: Message[]): Message[] {
  const idx = messages.findIndex(isPendingSteer);
  if (idx < 0) return messages;
  const { steerPending: _delivered, ...delivered } = messages[idx] as SteerBubble;
  const next = [...messages];
  next[idx] = delivered;
  return next;
}
