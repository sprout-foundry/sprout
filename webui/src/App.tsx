import React, { useState, useEffect, useCallback } from 'react';
import Sidebar from './components/Sidebar';
import Chat from './components/Chat';
import Status from './components/Status';
import UIManager from './components/UIManager';
import './App.css';
import { WebSocketService } from './services/websocket';
import { ApiService } from './services/api';

interface AppState {
  isConnected: boolean;
  provider: string;
  model: string;
  queryCount: number;
  messages: Message[];
  logs: string[];
  files: FileItem[];
}

interface Message {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
}

interface FileItem {
  path: string;
  modified: boolean;
  content?: string;
}

function App() {
  const [state, setState] = useState<AppState>({
    isConnected: false,
    provider: 'unknown',
    model: 'unknown',
    queryCount: 0,
    messages: [],
    logs: [],
    files: []
  });

  const [inputValue, setInputValue] = useState('');
  const wsService = WebSocketService.getInstance();
  const apiService = ApiService.getInstance();

  const handleEvent = useCallback((event: any) => {
    console.log('Received event:', event);

    switch(event.type) {
      case 'connection_status':
        setState(prev => ({
          ...prev,
          isConnected: event.data.connected
        }));
        console.log('🔗 Connection status updated:', event.data.connected);
        break;

      case 'query_started':
        setState(prev => ({
          ...prev,
          queryCount: prev.queryCount + 1,
          messages: [...prev.messages, {
            id: Date.now().toString(),
            type: 'user',
            content: event.data.query,
            timestamp: new Date()
          }]
        }));
        break;

      case 'stream_chunk':
        setState(prev => {
          const newMessages = [...prev.messages];
          const lastMessage = newMessages[newMessages.length - 1];
          if (lastMessage && lastMessage.type === 'assistant') {
            lastMessage.content += event.data.chunk;
          } else {
            newMessages.push({
              id: Date.now().toString(),
              type: 'assistant',
              content: event.data.chunk,
              timestamp: new Date()
            });
          }
          return { ...prev, messages: newMessages };
        });
        break;

      case 'query_completed':
        // Query completed, already handled in stream_chunk
        break;

      case 'error':
        setState(prev => ({
          ...prev,
          messages: [...prev.messages, {
            id: Date.now().toString(),
            type: 'assistant',
            content: `❌ Error: ${event.data.message}`,
            timestamp: new Date()
          }]
        }));
        break;

      case 'metrics_update':
        // Update provider/model info if available
        if (event.data.provider && event.data.model) {
          setState(prev => ({
            ...prev,
            provider: event.data.provider,
            model: event.data.model
          }));
        }
        break;
    }
  }, []);

  useEffect(() => {
    // Initialize WebSocket connection
    wsService.connect();
    wsService.onEvent(handleEvent);

    // Load initial stats
    apiService.getStats().then((stats: any) => {
      setState(prev => ({
        ...prev,
        provider: stats.provider,
        model: stats.model
      }));
    }).catch(console.error);

    // Cleanup
    return () => {
      wsService.disconnect();
    };
  }, [handleEvent, wsService, apiService]);

  const handleSendMessage = useCallback(async (message: string) => {
    if (!message.trim()) return;

    try {
      await apiService.sendQuery(message);
      setInputValue('');
    } catch (error) {
      console.error('Failed to send message:', error);
    }
  }, [apiService]);

  const handleProviderChange = useCallback((provider: string) => {
    console.log('Provider changed to:', provider);
    // Send provider change to backend
    wsService.sendEvent({
      type: 'provider_change',
      data: { provider }
    });

    setState(prev => ({
      ...prev,
      provider
    }));
  }, [wsService]);

  const handleModelChange = useCallback((model: string) => {
    console.log('Model changed to:', model);
    // Send model change to backend
    wsService.sendEvent({
      type: 'model_change',
      data: { model }
    });

    setState(prev => ({
      ...prev,
      model
    }));
  }, [wsService]);

  return (
    <UIManager>
      <div className="app">
        <Sidebar
          isConnected={state.isConnected}
          provider={state.provider}
          model={state.model}
          queryCount={state.queryCount}
          logs={state.logs}
          files={state.files}
          onProviderChange={handleProviderChange}
          onModelChange={handleModelChange}
        />
        <div className="main">
          <Status isConnected={state.isConnected} />
          <Chat
            messages={state.messages}
            onSendMessage={handleSendMessage}
            inputValue={inputValue}
            onInputChange={setInputValue}
          />
        </div>
      </div>
    </UIManager>
  );
}

export default App;