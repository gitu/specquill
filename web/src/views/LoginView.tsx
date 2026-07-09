import { FormEvent, useState } from 'react';
import { sx } from '../lib/sx';
import { api } from '../api/client';

// Fallback login page for local users; OIDC logins never see this — the
// server redirects them straight to the IdP from /auth/login.
export function LoginView() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const oidcError = new URLSearchParams(window.location.hash.split('?')[1] || '').get('error');

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api('/auth/local/login', { method: 'POST', body: JSON.stringify({ username, password }) });
      window.location.href = '/';
    } catch (err) {
      setError(String((err as Error).message || err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={sx('height:100vh;display:flex;align-items:center;justify-content:center;background:var(--bg)')}>
      <form onSubmit={submit} style={sx('width:340px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:28px 26px')}>
        <div style={sx('display:flex;align-items:center;gap:9px;margin-bottom:18px')}>
          <div style={sx('width:26px;height:26px;border-radius:7px;background:var(--text);display:flex;align-items:center;justify-content:center')}>
            <div style={sx('width:10px;height:10px;border-radius:2px;border:2px solid var(--surface);transform:rotate(45deg)')} />
          </div>
          <span style={sx('font-weight:700;font-size:17px;letter-spacing:-.2px')}>specquill</span>
        </div>
        <div style={sx('font-size:13px;color:var(--text-2);margin-bottom:18px')}>Sign in with your local account.</div>
        {oidcError && <div style={sx('margin-bottom:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>Single sign-on failed — try again or use a local account.</div>}
        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin-bottom:5px')}>Username</label>
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus
          style={sx('width:100%;height:34px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px;margin-bottom:13px')} />
        <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin-bottom:5px')}>Password</label>
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
          style={sx('width:100%;height:34px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px;margin-bottom:16px')} />
        {error && <div style={sx('margin-bottom:12px;color:var(--del);font-size:12px')}>{error}</div>}
        <button type="submit" disabled={busy}
          style={sx('width:100%;height:36px;border:none;border-radius:9px;background:var(--text);color:var(--bg);font-family:inherit;font-size:13px;font-weight:600;cursor:pointer')}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </div>
  );
}
