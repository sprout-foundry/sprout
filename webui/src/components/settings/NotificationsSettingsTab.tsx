import { Bell, BellOff, CheckCircle, XCircle } from 'lucide-react';
import { useState, useCallback } from 'react';
import * as desktopNotify from '../../services/desktopNotify';

interface NotificationsSettingsTabProps {
  renderLocalToggle?: (
    checked: boolean,
    label: string,
    onChange: (next: boolean) => void,
    helpText?: string,
  ) => JSX.Element;
}

/**
 * Desktop notification settings tab.
 * Runtime-scoped — controls browser Notification API permission and behavior.
 */
export default function NotificationsSettingsTab({ renderLocalToggle }: NotificationsSettingsTabProps) {
  const [permission, setPermission] = useState(desktopNotify.getPermission());
  const [enabled, setEnabled] = useState(desktopNotify.isEnabled_());
  const [testStatus, setTestStatus] = useState<'idle' | 'sent' | 'blocked'>('idle');
  const [requesting, setRequesting] = useState(false);

  const refreshPermission = useCallback(() => {
    setPermission(desktopNotify.getPermission());
  }, []);

  const handleToggle = useCallback((next: boolean) => {
    setEnabled(next);
    desktopNotify.setEnabled(next);
    if (next && desktopNotify.getPermission() === 'default') {
      setRequesting(true);
      desktopNotify
        .requestPermission()
        .then((perm) => {
          setPermission(perm);
          setRequesting(false);
        })
        .catch(() => {
          setRequesting(false);
        });
    }
  }, []);

  const handleTest = useCallback(() => {
    setTestStatus('idle');
    if (desktopNotify.getPermission() === 'denied') {
      setTestStatus('blocked');
      return;
    }
    if (desktopNotify.getPermission() === 'default') {
      setRequesting(true);
      desktopNotify
        .requestPermission()
        .then((perm) => {
          setPermission(perm);
          setRequesting(false);
          if (perm === 'granted') {
            desktopNotify.notify('Sprout', 'Test notification — desktop notifications are working!');
            setTestStatus('sent');
          } else {
            setTestStatus('blocked');
          }
        })
        .catch(() => {
          setRequesting(false);
          setTestStatus('blocked');
        });
    } else {
      desktopNotify.notify('Sprout', 'Test notification — desktop notifications are working!');
      setTestStatus('sent');
    }
  }, []);

  const permissionLabel: Record<string, string> = {
    granted: 'Allowed',
    denied: 'Blocked by browser',
    default: 'Not asked yet',
  };

  const toggle =
    renderLocalToggle ??
    ((checked: boolean, label: string, onChange: (next: boolean) => void) => (
      <label className="styled-toggle">
        <input type="checkbox" checked={checked} onChange={() => onChange(!checked)} />
        <span className="toggle-track" />
        <span className="toggle-label">{label}</span>
      </label>
    ));

  return (
    <div className="section">
      <h4>Desktop Notifications</h4>
      <p className="config-help" style={{ marginBottom: '12px' }}>
        Get notified when tasks complete or input is required while the Sprout tab is backgrounded.
      </p>

      {toggle(
        enabled,
        'Enable desktop notifications',
        handleToggle,
        'Notifications only fire when this tab is not in focus.',
      )}

      <div className="config-item">
        <div className="config-label-row">
          <span>Permission status</span>
          <span
            className={
              permission === 'granted' ? 'status-granted' : permission === 'denied' ? 'status-denied' : 'status-default'
            }
            style={{
              fontSize: '12px',
              color:
                permission === 'granted'
                  ? 'var(--accent-success)'
                  : permission === 'denied'
                    ? 'var(--accent-error)'
                    : 'var(--text-secondary)',
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
            }}
          >
            {permission === 'granted' ? (
              <CheckCircle size={14} />
            ) : permission === 'denied' ? (
              <XCircle size={14} />
            ) : (
              <BellOff size={14} />
            )}
            {permissionLabel[permission]}
          </span>
        </div>
      </div>

      <div className="config-item">
        <button
          type="button"
          className="btn-secondary"
          onClick={handleTest}
          disabled={requesting || !enabled}
          style={{ marginTop: '8px' }}
        >
          {requesting ? (
            'Requesting...'
          ) : testStatus === 'sent' ? (
            'Sent'
          ) : testStatus === 'blocked' ? (
            'Blocked'
          ) : (
            <>
              <Bell size={14} style={{ marginRight: '6px' }} />
              Send test notification
            </>
          )}
        </button>
        {testStatus === 'blocked' && (
          <div className="config-help" style={{ color: 'var(--accent-error)' }}>
            Browser notifications are blocked. Check your browser settings to allow them.
          </div>
        )}
        {testStatus === 'sent' && (
          <div className="config-help" style={{ color: 'var(--accent-success)' }}>
            Test notification sent successfully!
          </div>
        )}
      </div>
    </div>
  );
}
