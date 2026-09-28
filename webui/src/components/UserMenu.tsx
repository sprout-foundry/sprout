/**
 * UserMenu — avatar menu in the cloud-mode header (SP-016 P0.5).
 *
 * First consumer of getBootstrapUser(): the account-surface identity
 * (avatar initial + email) and the exit items (Dashboard / Tasks /
 * Billing / Manage Team) plus Sign out. Cloud mode only — local mode
 * renders nothing (it shows no identity surface today, and we match
 * that).
 *
 * Exit URLs are built with platformHref (SP-016 P0.3): absolute
 * (platformURL + path) when the host knows the platform base, the
 * relative path otherwise (today's behavior). Every exit is tagged
 * ?from=editor (SP-016 P0.7) so the platform can count editor exits.
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
import { isCloud } from '../config/mode';
import { getBootstrapUser, getPlatformURL } from '../bootstrapAdapter';
import { ADAPTER_INSTALLED_EVENT } from '../services/apiAdapter';
import { notificationBus } from '../services/notificationBus';
import { platformHref } from '../utils/platformUrl';

/** Account-surface exit items, the same account area the platform header's
 *  menu offers. Paths carry ?from=editor (SP-016 P0.7) and put the SPA route
 *  in the hash: Team/Runners are also flat API routes on the platform
 *  (GET /team, GET /runners), so a plain /team would return the API's JSON,
 *  not the page. */
const MENU_ITEMS: readonly { label: string; path: string }[] = [
  { label: 'Dashboard', path: '/?from=editor' },
  { label: 'Tasks', path: '/?from=editor#/tasks' },
  { label: 'Usage & billing', path: '/?from=editor#/account/billing' },
  { label: 'Team', path: '/?from=editor#/team' },
  { label: 'Runners', path: '/?from=editor#/runners' },
  { label: 'Settings', path: '/?from=editor#/settings' },
];

const ADMIN_ITEM = { label: 'Admin', path: '/?from=editor#/admin' };

type BootstrapUser = NonNullable<ReturnType<typeof getBootstrapUser>>;

export function UserMenu(): JSX.Element | null {
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

  // Cloud mode only, and only when the bootstrap carried a user identity.
  if (!isCloud || !user) {
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
    try {
      await fetch(platformHref('/webui/auth/logout'), {
        method: 'POST',
        credentials: 'include',
      });
    } catch (e) {
      // Network failure only — the session cookie is intact, so staying
      // here is the honest outcome. Surface it rather than stranding the
      // user in a logged-in state pretending to be signed out.
      setSigningOut(false);
      setOpen(true);
      notificationBus.notify('error', 'Sign out failed', e instanceof Error ? e.message : String(e));
      return;
    }
    // The server cleared the session cookie. A hard navigation drops any
    // cached client-side session state and lands on the login screen.
    window.location.href = platformHref('/login');
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
            {(user.admin ? [...MENU_ITEMS, ADMIN_ITEM] : MENU_ITEMS).map((item) => (
              <a
                key={item.label}
                role="menuitem"
                className="user-menu-item"
                href={platformHref(item.path)}
                onClick={() => setOpen(false)}
              >
                {item.label}
              </a>
            ))}
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
