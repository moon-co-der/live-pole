import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { apiRequest, tokenStorageKey } from '../api/client.js';

const AuthContext = createContext(null);

function storedToken() {
  try { return localStorage.getItem(tokenStorageKey) || ''; }
  catch { return ''; }
}

export function AuthProvider({ children }) {
  const [token, setToken] = useState(storedToken);
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    if (!token) {
      setUser(null);
      setLoading(false);
      return () => { active = false; };
    }
    setLoading(true);
    apiRequest('/api/auth/me')
      .then(async (response) => {
        if (response.status === 401) {
          localStorage.removeItem(tokenStorageKey);
          if (active) { setToken(''); setUser(null); }
          return;
        }
        if (!response.ok) return;
        const result = await response.json();
        if (active) setUser(result.user);
      })
      .catch(() => {})
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [token]);

  const login = useCallback(async (email, password) => {
    const response = await apiRequest('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(result.error || 'Unable to log in. Please try again.');
    localStorage.setItem(tokenStorageKey, result.token);
    setToken(result.token);
    setUser(result.user);
    return result.user;
  }, []);

  const logout = useCallback(() => {
    localStorage.removeItem(tokenStorageKey);
    setToken('');
    setUser(null);
  }, []);

  const value = useMemo(() => ({ token, user, loading, login, logout }), [token, user, loading, login, logout]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth must be used within AuthProvider');
  return context;
}
