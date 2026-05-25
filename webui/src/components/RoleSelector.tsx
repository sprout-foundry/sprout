import { useState, useEffect, useCallback, useRef } from 'react';
import { listRoles } from '../services/api/rolesApi';
import type { RoleConfig } from '../services/api/rolesApi';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import './RoleSelector.css';

interface RoleSelectorProps {
  selectedRole: string | null;
  onRoleChange: (roleId: string | null) => void;
}

export function RoleSelector({ selectedRole, onRoleChange }: RoleSelectorProps) {
  const fetchFn = useSproutFetch();
  const [roles, setRoles] = useState<RoleConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    return () => { mountedRef.current = false; };
  }, []);

  const loadRoles = useCallback(async () => {
    if (!fetchFn) return;
    if (!mountedRef.current) return;
    setLoading(true);
    setError(null);
    try {
      const data = await listRoles(fetchFn);
      if (!mountedRef.current) return;
      setRoles(data);
    } catch (err) {
      if (!mountedRef.current) return;
      setError('Failed to load roles. Click refresh to try again.');
      if (err instanceof Error) {
        console.error('Role fetch failed:', err.message);
      }
      setRoles([]);
    } finally {
      if (mountedRef.current) {
        setLoading(false);
      }
    }
  }, [fetchFn]);

  useEffect(() => {
    loadRoles();
  }, [loadRoles]);

  const handleRoleChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const value = e.target.value;
    onRoleChange(value === '' ? null : value);
  };

  return (
    <div className="role-selector">
      <div className="role-selector-header">
        <label htmlFor="role-select">Role:</label>
        <button
          type="button"
          className="role-selector-refresh"
          title="Refresh roles"
          onClick={loadRoles}
          disabled={loading}
          aria-label="Refresh roles"
        >
          &#x21BB;
        </button>
      </div>
      <select
        id="role-select"
        value={selectedRole || ''}
        onChange={handleRoleChange}
        disabled={loading}
        className="styled-select"
      >
        <option value="">None</option>
        <optgroup label="Custom Roles">
          {loading
            ? <option value="" disabled>Loading roles...</option>
            : roles.length === 0 && error
              ? <option value="" disabled>Error: {error}</option>
              : roles.length === 0
                ? <option value="" disabled>No custom roles defined</option>
                : roles.map((role) => (
                  <option key={role.name} value={role.name}>
                    {role.name}{role.description ? ` — ${role.description}` : ''}
                  </option>
                ))}
        </optgroup>
      </select>
    </div>
  );
}

export default RoleSelector;
