import React, { useState, useEffect, useCallback } from 'react';
import Sidebar from './components/Sidebar';
import Chat from './components/Chat';
import Status from './components/Status';
import UIManager from './components/UIManager';
import FileTree from './components/FileTree';
import CodeEditor from './components/CodeEditor';
import './App.css';
import { WebSocketService } from './services/websocket';
import { ApiService } from './services/api';

// Service Worker Registration
const registerServiceWorker = async () => {
  if ('serviceWorker' in navigator) {
    try {
      const registration = await navigator.serviceWorker.register('/sw.js');
      console.log('SW registered:', registration);

      // Check for updates periodically
      registration.addEventListener('updatefound', () => {
        const newWorker = registration.installing;
        if (newWorker) {
          newWorker.addEventListener('statechange', () => {
            if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
              // New version available, show update notification
              console.log('New service worker available');
              // You could show a toast notification here
            }
          });
        }
      });

      return registration;
    } catch (error) {
      console.log('SW registration failed:', error);
    }
  }
  return null;
};

interface AppState {
  isConnected: boolean;
  provider: string;
  model: string;
  queryCount: number;
  messages: Message[];
  logs: string[];
  files: FileItem[];
  isProcessing: boolean;
  lastError: string | null;
  currentView: 'chat' | 'editor';
  selectedFile: FileInfo | null;
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

interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
}

function App() {
  const [state, setState] = useState<AppState>({
    isConnected: false,
    provider: 'unknown',
    model: 'unknown',
    queryCount: 0,
    messages: [],
    logs: [],
    files: [],
    isProcessing: false,
    lastError: null,
    currentView: 'chat',
    selectedFile: null
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
        setState(prev => ({
          ...prev,
          isProcessing: false,
          lastError: null
        }));
        console.log('✅ Query completed');
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
    // Register Service Worker for PWA functionality
    registerServiceWorker();

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
    if (!message.trim() || state.isProcessing) return;

    // Clear any previous errors and set processing state
    setState(prev => ({
      ...prev,
      isProcessing: true,
      lastError: null
    }));

    try {
      console.log('🚀 Sending message:', message);
      await apiService.sendQuery(message);
      setInputValue('');
      console.log('✅ Message sent successfully');
    } catch (error) {
      console.error('❌ Failed to send message:', error);
      setState(prev => ({
        ...prev,
        isProcessing: false,
        lastError: `Failed to send message: ${error instanceof Error ? error.message : 'Unknown error'}`
      }));

      // Add error message to chat
      setState(prev => ({
        ...prev,
        messages: [...prev.messages, {
          id: Date.now().toString(),
          type: 'assistant',
          content: `❌ Error: ${error instanceof Error ? error.message : 'Failed to send message'}`,
          timestamp: new Date()
        }]
      }));
    }
  }, [apiService, state.isProcessing]);

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

  const handleViewChange = useCallback((view: 'chat' | 'editor') => {
    setState(prev => ({
      ...prev,
      currentView: view
    }));
  }, []);

  const handleFileSelect = useCallback((file: FileInfo) => {
    setState(prev => ({
      ...prev,
      selectedFile: file
    }));
  }, []);

  const handleFileSave = useCallback((content: string) => {
    console.log('File saved:', state.selectedFile?.path);
    // You could add additional logic here like refreshing the file tree
  }, [state.selectedFile]);

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
          currentView={state.currentView}
          onViewChange={handleViewChange}
        />
        <div className="main">
          <Status isConnected={state.isConnected} />

          {state.currentView === 'chat' ? (
            <Chat
              messages={state.messages}
              onSendMessage={handleSendMessage}
              inputValue={inputValue}
              onInputChange={setInputValue}
              isProcessing={state.isProcessing}
              lastError={state.lastError}
            />
          ) : (
            <div className="editor-view">
              <FileTree
                onFileSelect={handleFileSelect}
                selectedFile={state.selectedFile?.path}
              />
              <CodeEditor
                file={state.selectedFile}
                onSave={handleFileSave}
              />
            </div>
          )}
        </div>
      </div>
    </UIManager>
  );
}

export default App;