import { X, Download } from 'lucide-react';
import React, { useState, useEffect, useCallback } from 'react';
import './InstallPromptBanner.css';

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

const DISMISSAL_KEY = 'sprout:install-prompt-dismissed';

/**
 * PWA install prompt banner for the editor.
 *
 * Listens for the browser's `beforeinstallprompt` event and shows a
 * dismissible banner prompting the user to install the app. Only active
 * in cloud mode (the banner is rendered conditionally by the parent).
 *
 * The banner is shown at most once per session after dismissal (persisted
 * in localStorage).
 */
const InstallPromptBanner: React.FC = () => {
  const [deferredPrompt, setDeferredPrompt] = useState<BeforeInstallPromptEvent | null>(null);
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const dismissed = localStorage.getItem(DISMISSAL_KEY);
    if (dismissed) return;

    const handler = (e: Event) => {
      e.preventDefault();
      setDeferredPrompt(e as BeforeInstallPromptEvent);
      setVisible(true);
    };

    window.addEventListener('beforeinstallprompt', handler);
    return () => window.removeEventListener('beforeinstallprompt', handler);
  }, []);

  const handleInstall = useCallback(async () => {
    if (!deferredPrompt) return;
    await deferredPrompt.prompt();
    const choice = await deferredPrompt.userChoice;
    if (choice.outcome === 'accepted') {
      localStorage.setItem(DISMISSAL_KEY, Date.now().toString());
    }
    setDeferredPrompt(null);
    setVisible(false);
  }, [deferredPrompt]);

  const handleDismiss = useCallback(() => {
    localStorage.setItem(DISMISSAL_KEY, Date.now().toString());
    setVisible(false);
  }, []);

  if (!visible || !deferredPrompt) return null;

  return (
    <div className="install-prompt-banner" role="banner">
      <Download size={16} className="install-prompt-icon" />
      <span className="install-prompt-text">Install Sprout Foundry for offline access and a native app experience.</span>
      <button className="install-prompt-btn install-prompt-btn-primary" onClick={handleInstall}>
        Install
      </button>
      <button className="install-prompt-btn install-prompt-btn-dismiss" onClick={handleDismiss} aria-label="Dismiss">
        <X size={14} />
      </button>
    </div>
  );
};

export default InstallPromptBanner;
