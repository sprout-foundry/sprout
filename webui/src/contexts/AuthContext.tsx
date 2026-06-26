import { createContext, useContext, useState, useCallback, ReactNode } from 'react';

interface AuthContextType {
  token: string | null;
  setToken: (token: string) => void;
  clearToken: () => void;
  isAuthenticated: boolean;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(() => {
    return sessionStorage.getItem('sprout_auth_token');
  });

  const setToken = useCallback((newToken: string) => {
    sessionStorage.setItem('sprout_auth_token', newToken);
    setTokenState(newToken);
  }, []);

  const clearToken = useCallback(() => {
    sessionStorage.removeItem('sprout_auth_token');
    setTokenState(null);
  }, []);

  const isAuthenticated = token !== null && token !== '';

  return (
    <AuthContext.Provider value={{ token, setToken, clearToken, isAuthenticated }}>{children}</AuthContext.Provider>
  );
}

export function useAuth(): AuthContextType {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}
