/**
 * Overlay shown while the WASM binary downloads and initializes in cloud mode.
 * The binary is ~44MB so this can take 5-30 seconds depending on connection.
 * Without this overlay, users see a blank workspace with no feedback.
 */

import { useEffect, useState } from 'react';
import { isCloud } from '../config/mode';
import './WasmLoadingOverlay.css';

interface WasmLoadingOverlayProps {
  isLoading: boolean;
  error?: string | null;
}

const LOADING_MESSAGES = [
  'Loading browser runtime...',
  'Initializing virtual filesystem...',
  'Compiling WebAssembly module...',
  'Setting up agent tools...',
  'Preparing workspace...',
];

export function WasmLoadingOverlay({ isLoading, error }: WasmLoadingOverlayProps) {
  const [messageIdx, setMessageIdx] = useState(0);
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    if (!isLoading) return;
    const msgInterval = setInterval(() => {
      setMessageIdx((prev) => (prev + 1) % LOADING_MESSAGES.length);
    }, 2500);
    const elapsedInterval = setInterval(() => {
      setElapsed((prev) => prev + 1);
    }, 1000);
    return () => {
      clearInterval(msgInterval);
      clearInterval(elapsedInterval);
    };
  }, [isLoading]);

  if (!isCloud) return null;
  if (!isLoading && !error) return null;

  return (
    <div className="wasm-loading-overlay">
      <div className="wasm-loading-content">
        {error ? (
          <>
            <div className="wasm-loading-error-icon">
              <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
                <line x1="12" y1="9" x2="12" y2="13" />
                <line x1="12" y1="17" x2="12.01" y2="17" />
              </svg>
            </div>
            <h2 className="wasm-loading-title">Runtime Error</h2>
            <p className="wasm-loading-message">{error}</p>
            <p className="wasm-loading-hint">Try refreshing the page. If the problem persists, your browser may not support WebAssembly.</p>
          </>
        ) : (
          <>
            <div className="wasm-loading-spinner" />
            <h2 className="wasm-loading-title">Starting Browser IDE</h2>
            <p className="wasm-loading-message">{LOADING_MESSAGES[messageIdx]}</p>
            {elapsed > 3 && (
              <p className="wasm-loading-hint">
                Downloading runtime ({elapsed}s)
              </p>
            )}
          </>
        )}
      </div>
    </div>
  );
}
