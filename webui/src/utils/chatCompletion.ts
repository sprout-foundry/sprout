export type ChatMessageLike = {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
};

export const ensureCompletedAssistantMessage = <T extends ChatMessageLike>(
  messages: T[],
  response: unknown,
  createAssistantMessage: (responseText: string) => T,
): T[] => {
  if (typeof response !== 'string' || !response.trim()) {
    return messages;
  }

  const updatedMessages = [...messages];
  let lastUserIndex = -1;

  for (let i = updatedMessages.length - 1; i >= 0; i -= 1) {
    if (updatedMessages[i].type === 'user') {
      lastUserIndex = i;
      break;
    }
  }

  let assistantIndex = -1;
  for (let i = updatedMessages.length - 1; i > lastUserIndex; i -= 1) {
    if (updatedMessages[i].type === 'assistant') {
      assistantIndex = i;
      break;
    }
  }

  if (assistantIndex === -1) {
    updatedMessages.push(createAssistantMessage(response));
    return updatedMessages;
  }

  const assistantMessage = updatedMessages[assistantIndex];
  if ((assistantMessage.content || '').trim()) {
    return messages;
  }

  updatedMessages[assistantIndex] = {
    ...assistantMessage,
    content: response,
  };
  return updatedMessages;
};