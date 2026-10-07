/**
 * UserMenu — avatar menu in the cloud-mode header (SP-016 P0.5).
 *
 * First consumer of getBootstrapUser(): the account-surface identity
 * (avatar initial + email) and the exit items plus Sign out. The exit items
 * come from the host (`host.navigation.accountItems`, plus the host's admin
 * item for administrators) rather than a hard-coded list: the host owns the
 * labels and each item's destination, and Sprout renders + dispatches.
 *
 * Exit URLs are resolved by the host (`host.navigation.intentPath`); the paths
 * live only on the host side. Every exit is tagged ?from=editor (SP-016 P0.7)
 * by the host, so the platform can count editor exits.
 *
 * Sign out reuses the platform's existing server-side logout path
 * (POST /webui/auth/logout — the Kratos two-step browser logout the
 * platform SPA's signOut uses), then hard-navigates to /login. On a
 * network failure the cookie is left intact, so we surface a
 * notification and stay — the platform SPA's signOut degrades the
 * same way.
 */

import type { CSSProperties } from 'react';
import { useEffect, useRef, useState } from 'react';
import { getBootstrapUser } from '../bootstrapAdapter';
import { isLayeredLayout } from '../config/layout';
import type { HostNavItem, HostNavigationIntent } from '../host/types';
import { useHost } from '../host/useHost';
import { ADAPTER_INSTALLED_EVENT } from '../services/apiAdapter';
import { openHome } from '../services/homeView';
import { notificationBus } from '../services/notificationBus';

type BootstrapUser = NonNullable<ReturnType<typeof getBootstrapUser>>;

interface UserMenuProps {
  /** Shown under the avatar (the phone tab bar's "You"). */
  label?: string;
}

export function UserMenu({ label }: UserMenuProps = {}): JSX.Element | null {
  const host = useHost();
  const { navigation } = host;
  // Identity is captured once at bootstrap (adapter install); re-read it
  // on the install event so a late bootstrap still populates the menu —
  // the same seam PlatformNavContext uses.
  const [user, setUser] = useState<BootstrapUser | null>(() => getBootstrapUser() ?? null);
  const [open, setOpen] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const handler = () => setUser(getBootstrapUser() ?? null);
    window.addEventListener(ADAPTER_INSTALLED_EVENT, handler);
    return () => window.removeEventListener(ADAPTER_INSTALLED_EVENT, handler);
  }, []);

  const accountItems = navigation.accountItems ?? [];
  const hasAdmin = accountItems.some((i) => i.label === 'Admin');
  const items: HostNavItem[] =
    user?.admin && !hasAdmin
      ? [...accountItems, { label: 'Admin', intent: { type: 'nav', id: 'admin' } }]
      : accountItems;

  // Cloud mode only, and only when the bootstrap carried a user identity and
  // the host offers an account surface.
  if (!user || items.length === 0) {
    return null;
  }

  const initial = (user.email || user.id || '?').charAt(0).toUpperCase();

  const close = () => {
    setOpen(false);
    triggerRef.current?.focus();
  };

  const handleSignOut = async () => {
    setSigningOut(true);
    setOpen(false);
    // The host performs the sign-out (logout + redirect); Sprout only requests
    // the intent. A network failure propagates to the caller: the session
    // cookie is intact, so staying here is the honest outcome. Surface it
    // rather than stranding the user in a logged-in state pretending to be
    // signed out.
    try {
      await navigation.open({ type: 'signOut' } satisfies HostNavigationIntent);
    } catch (e) {
      setSigningOut(false);
      setOpen(true);
      notificationBus.notify('error', 'Sign out failed', e instanceof Error ? e.message : String(e));
    }
  };

  // The header bar clips overflow, so the list is fixed-positioned under the
  // trigger instead of absolutely inside it.
  const listPosition = (): CSSProperties | undefined => {
    const rect = triggerRef.current?.getBoundingClientRect();
    if (!rect) return undefined;
    // A trigger in the left half (the layered layout's rail) opens to the
    // right and upward; the header trigger opens down and right-aligned.
    if (rect.left < window.innerWidth / 2) {
      return {
        position: 'fixed',
        left: rect.right + 8,
        right: 'auto',
        top: 'auto',
        bottom: Math.max(8, window.innerHeight - rect.bottom),
      };
    }
    // A trigger at the bottom (the phone tab bar) opens upward.
    if (rect.top > window.innerHeight / 2) {
      return {
        position: 'fixed',
        top: 'auto',
        bottom: window.innerHeight - rect.top + 4,
        right: Math.max(8, window.innerWidth - rect.right),
      };
    }
    return { position: 'fixed', top: rect.bottom + 4, right: Math.max(8, window.innerWidth - rect.right) };
  };

  return (
    <div className="user-menu">
      <button
        ref={triggerRef}
        type="button"
        className="user-menu-trigger"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Account menu for ${user.email}`}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            close();
          }
        }}
      >
        <span className="user-menu-avatar" aria-hidden="true">
          {initial}
        </span>
        {label && <span className="user-menu-trigger-label">{label}</span>}
      </button>
      {open && (
        <>
          <div className="user-menu-backdrop" onClick={close} aria-hidden="true" />
          <div className="user-menu-list" role="menu" aria-label="Account" style={listPosition()}>
            <div className="user-menu-identity">
              <span className="user-menu-identity-email" title={user.email}>
                {user.email}
              </span>
              {user.tier ? <span className="user-menu-tier">{user.tier}</span> : null}
            </div>
            {items.map((item) => {
              const path = navigation.intentPath?.(item.intent) ?? undefined;
              // The host resolves the path to a full page URL (its outward
              // surface); Sprout only renders the href the host supplies.
              const href = path ? (navigation.platformPagePath?.(path) ?? path) : undefined;
              return (
                <a
                  key={item.label}
                  role="menuitem"
                  className="user-menu-item"
                  href={href}
                  onClick={(e) => {
                    setOpen(false);
                    // Layered layout: platform pages open inside the shell
                    // (Home) instead of leaving the editor. The host owns the
                    // path; Sprout decides only where to show it.
                    if (isLayeredLayout && path && !e.metaKey && !e.ctrlKey) {
                      e.preventDefault();
                      openHome(path);
                    }
                  }}
                >
                  {item.label}
                </a>
              );
            })}
            <button
              type="button"
              role="menuitem"
              className="user-menu-item user-menu-signout"
              onClick={() => void handleSignOut()}
              disabled={signingOut}
            >
              {signingOut ? 'Signing out…' : 'Sign out'}
            </button>
          </div>
        </>
      )}
    </div>
  );
}

export default UserMenu;
