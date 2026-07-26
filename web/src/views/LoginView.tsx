import { FormEvent, useEffect, useState } from 'react';
import { sx } from '../lib/sx';
import { api } from '../api/client';

interface Providers { oidc: boolean; local: boolean }

// Login page: offers whatever /auth/providers reports. Pure-OIDC setups
// never see it — the server redirects them straight to the IdP.
export function LoginView() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [providers, setProviders] = useState<Providers | null>(null);
  const authError = new URLSearchParams(window.location.hash.split('?')[1] || '').get('error');

  useEffect(() => {
    api<Providers>('/auth/providers')
      .then(setProviders)
      .catch(() => setProviders({ oidc: false, local: true }));
  }, []);

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

  const errText = authError
    ? 'Single sign-on failed — try again or use a local account.'
    : '';

  return (
    <div style={sx('height:100vh;display:flex;align-items:center;justify-content:center;background:var(--bg)')}>
      <form onSubmit={submit} style={sx('width:340px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:28px 26px')}>
        <div style={sx('display:flex;align-items:center;gap:9px;margin-bottom:18px')}>
          <div style={sx('width:26px;height:26px;border-radius:7px;background:var(--text);display:flex;align-items:center;justify-content:center')}>
            <div style={sx('width:10px;height:10px;border-radius:2px;border:2px solid var(--surface);transform:rotate(45deg)')} />
          </div>
          <span style={sx('font-weight:700;font-size:17px;letter-spacing:-.2px')}>specquill</span>
        </div>
        {errText && <div style={sx('margin-bottom:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>{errText}</div>}

        {providers?.local && (
          <>
            <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin-bottom:5px')}>Username</label>
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus
              style={sx('width:100%;height:34px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px;margin-bottom:13px')} />
            <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin-bottom:5px')}>Password</label>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
              style={sx('width:100%;height:34px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px;margin-bottom:16px')} />
            {error && <div style={sx('margin-bottom:12px;color:var(--del);font-size:12px')}>{error}</div>}
            <button type="submit" disabled={busy}
              style={sx('width:100%;height:36px;border:1px solid var(--border-2);border-radius:9px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px;font-weight:600;cursor:pointer')}>
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </>
        )}

        {providers && !providers.local && !providers.oidc && (
          <div style={sx('font-size:12.5px;color:var(--text-2)')}>No login methods are enabled — check the server configuration.</div>
        )}
      </form>
    </div>
  );
}
