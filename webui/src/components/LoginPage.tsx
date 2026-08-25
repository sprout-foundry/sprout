import type { FormEvent } from 'react';
import { useState } from 'react';
import { useAuth } from '../contexts/AuthContext';
import './LoginPage.css';

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
    <div className="login-page">
      <form onSubmit={handleSubmit} className="login-form">
        <h2 className="login-title">Sign in to Sprout</h2>
        {error && (
          <p className="login-error" role="alert">
            {error}
          </p>
        )}
        <input
          type="password"
          value={inputToken}
          onChange={(e) => {
            setInputToken(e.target.value);
            setError('');
          }}
          placeholder="Enter auth token"
          className="login-input"
          aria-label="Auth token"
          autoFocus
        />
        <button type="submit" className="login-button">
          Sign in
        </button>
      </form>
    </div>
  );
}
