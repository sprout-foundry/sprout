import { useState, FormEvent } from 'react';
import { useAuth } from '../contexts/AuthContext';

export default function LoginPage() {
  const [inputToken, setInputToken] = useState('');
  const [error, setError] = useState('');
  const { setToken } = useAuth();

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!inputToken.trim()) {
      setError('Please enter a token');
      return;
    }
    setToken(inputToken.trim());
  };

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh', background: '#1a1a2e' }}>
      <form onSubmit={handleSubmit} style={{ background: '#16213e', padding: '2rem', borderRadius: '8px', width: '360px' }}>
        <h2 style={{ color: '#e0e0e0', marginBottom: '1rem' }}>Sign in to Sprout</h2>
        {error && <p style={{ color: '#ff6b6b', marginBottom: '0.5rem' }}>{error}</p>}
        <input
          type="password"
          value={inputToken}
          onChange={(e) => { setInputToken(e.target.value); setError(''); }}
          placeholder="Enter auth token"
          style={{ width: '100%', padding: '0.5rem', marginBottom: '1rem', borderRadius: '4px', border: '1px solid #0f3460', background: '#0f3460', color: '#e0e0e0' }}
          autoFocus
        />
        <button type="submit" style={{ width: '100%', padding: '0.5rem', background: '#533483', color: '#e0e0e0', border: 'none', borderRadius: '4px', cursor: 'pointer' }}>
          Sign in
        </button>
      </form>
    </div>
  );
}
