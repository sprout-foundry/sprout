import { Users, Mail } from 'lucide-react';
import React, { useState, useEffect, useCallback } from 'react';
import { useSproutAdapter } from '../contexts/SproutAdapterContext';
import { useLog } from '../utils/log';
import './TeamPage.css';

interface FoundryTeamMember {
  id: string;
  email: string;
  name: string;
  role: 'owner' | 'admin' | 'member';
  joined_at: string;
}

interface FoundryTeamInvite {
  id: string;
  email: string;
  role: 'admin' | 'member';
  invited_at: string;
  expires_at: string;
}

interface FoundryTeam {
  name: string;
  members: FoundryTeamMember[];
  invites: FoundryTeamInvite[];
}

/**
 * Signature matching both the adapter's fetch and the webui's useSproutFetch.
 */
type FetchFn = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export interface TeamPageProps {
  /**
   * Optional fetch callback. When supplied, TeamPage uses this for all HTTP
   * calls instead of looking up the @sprout/ui SproutAdapterContext.
   *
   * This is the integration point for consumers (e.g. the webui) that need to
   * inject their own context-aware fetch.
   *
   * When omitted, TeamPage falls back to the @sprout/ui SproutAdapterContext
   * (i.e. useSproutAdapter from the package itself).
   */
  sproutFetch?: FetchFn;
}

const TeamPage: React.FC<TeamPageProps> = ({ sproutFetch }) => {
  const log = useLog();

  // Always call the hook unconditionally (Rules of Hooks).
  const adapter = useSproutAdapter();

  // Prefer the injected sproutFetch when provided; fall back to the adapter.
  const doFetch: FetchFn | undefined = sproutFetch ?? adapter?.fetch;
  const available = !!doFetch;

  const [team, setTeam] = useState<FoundryTeam | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchTeam = useCallback(async () => {
    if (!available) {
      setError('Not available - running in local mode');
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const response = await doFetch!('/api/foundry/team');
      if (!response.ok) {
        throw new Error(`Failed to fetch team: ${response.status} ${response.statusText}`);
      }
      const data = await response.json();
      setTeam({
        name: data?.name ?? 'Team',
        members: Array.isArray(data?.members) ? data.members : [],
        invites: Array.isArray(data?.invites) ? data.invites : [],
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load team information';
      setError(message);
      log.error(message, { title: 'Team Page Error' });
    } finally {
      setLoading(false);
    }
  }, [available, doFetch, log]);

  useEffect(() => {
    fetchTeam();
  }, [fetchTeam]);

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  };

  const getInitials = (name: string, email: string) => {
    if (name) {
      return name
        .split(' ')
        .map((n) => n[0])
        .join('')
        .toUpperCase()
        .slice(0, 2);
    }
    return email.substring(0, 2).toUpperCase();
  };

  const getMemberIcon = (member: FoundryTeamMember) => {
    const initials = getInitials(member.name, member.email);
    return (
      <div className="platform-list-item-icon">
        <span style={{ fontWeight: 600, fontSize: '14px' }}>{initials}</span>
      </div>
    );
  };

  return (
    <div className="platform-page">
      <div className="platform-page-header">
        <h2>Team</h2>
        <p>Manage team members and invitations.</p>
      </div>

      {loading && <div className="platform-page-loading">Loading team information...</div>}

      {error && (
        <div className="platform-page-error">
          <h3>Error loading team</h3>
          <p>{error}</p>
          <button
            className="platform-button platform-button-secondary platform-button-sm"
            onClick={fetchTeam}
            style={{ marginTop: '16px' }}
          >
            Retry
          </button>
        </div>
      )}

      {!loading && !error && team && (
        <>
          {/* Team Name Card */}
          <div className="platform-card">
            <div className="platform-card-header">
              <h3 className="platform-card-title">{team.name}</h3>
              <span className="platform-status-badge running">
                {team.members.length} {team.members.length === 1 ? 'member' : 'members'}
              </span>
            </div>
            <div className="platform-card-body">
              {team.members.length > 0
                ? `${team.members.length} team ${team.members.length === 1 ? 'member' : 'members'}`
                : 'No members yet'}
              {team.invites.length > 0 &&
                ` and ${team.invites.length} pending ${team.invites.length === 1 ? 'invitation' : 'invitations'}`}
            </div>
          </div>

          {/* Team Members */}
          {team.members.length > 0 && (
            <>
              <div className="platform-section">
                <div className="platform-section-header">
                  <h3 className="platform-section-title">Members</h3>
                  <span className="platform-section-count">{team.members.length}</span>
                </div>
              </div>
              <div className="platform-list">
                {team.members.map((member) => (
                  <div key={member.id} className="platform-list-item">
                    {getMemberIcon(member)}
                    <div className="platform-list-item-content">
                      <div className="platform-list-item-title">{member.name || member.email}</div>
                      <div className="platform-list-item-subtitle">{member.email}</div>
                    </div>
                    <div className="platform-list-item-meta">
                      <span className={`platform-role-badge ${member.role}`}>{member.role}</span>
                      <div className="platform-list-item-time">Joined {formatDate(member.joined_at)}</div>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}

          {/* Pending Invitations */}
          {team.invites.length > 0 && (
            <>
              <div className="platform-divider" />
              <div className="platform-section">
                <div className="platform-section-header">
                  <h3 className="platform-section-title">Pending Invitations</h3>
                  <span className="platform-section-count">{team.invites.length}</span>
                </div>
              </div>
              <div className="platform-list">
                {team.invites.map((invite) => (
                  <div key={invite.id} className="platform-list-item">
                    <div className="platform-list-item-icon">
                      <Mail size={18} />
                    </div>
                    <div className="platform-list-item-content">
                      <div className="platform-list-item-title">{invite.email}</div>
                      <div className="platform-list-item-subtitle">Expires {formatDate(invite.expires_at)}</div>
                    </div>
                    <div className="platform-list-item-meta">
                      <span className={`platform-role-badge ${invite.role}`}>{invite.role}</span>
                      <div className="platform-list-item-time">Invited {formatDate(invite.invited_at)}</div>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}

          {/* Empty State */}
          {team.members.length === 0 && team.invites.length === 0 && (
            <div className="platform-page-empty">
              <div className="platform-page-empty-icon">
                <Users size={48} />
              </div>
              <h3>No team members</h3>
              <p>Invite team members to collaborate on your workspace.</p>
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default TeamPage;
