import { FormEvent, useEffect, useState } from 'react';
import { sx } from '../lib/sx';
import { api } from '../api/client';

interface Providers { oidc: boolean; github: boolean; local: boolean }

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
      .catch(() => setProviders({ oidc: false, github: false, local: true }));
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

  const errText = authError === 'forbidden'
    ? 'This GitHub account is not on the allow-list for this workspace — ask an administrator to add you.'
    : authError === 'github'
    ? 'GitHub sign-in failed — try again.'
    : authError
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

        {providers?.github && (
          <a href="/auth/github/login"
            style={sx('display:flex;align-items:center;justify-content:center;gap:9px;width:100%;height:38px;border:1px solid var(--border-2);border-radius:9px;background:var(--text);color:var(--bg);font-size:13px;font-weight:600;text-decoration:none;margin-bottom:14px')}>
            <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden>
              <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.6 7.6 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
            </svg>
            Continue with GitHub
          </a>
        )}

        {providers?.local && (
          <>
            {providers.github && (
              <div style={sx('display:flex;align-items:center;gap:10px;margin-bottom:14px;color:var(--text-3);font-size:11px')}>
                <span style={sx('flex:1;height:1px;background:var(--border)')} />or<span style={sx('flex:1;height:1px;background:var(--border)')} />
              </div>
            )}
            <label style={sx('display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin-bottom:5px')}>Username</label>
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus={!providers.github}
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

        {providers && !providers.local && !providers.github && !providers.oidc && (
          <div style={sx('font-size:12.5px;color:var(--text-2)')}>No login methods are enabled — check the server configuration.</div>
        )}
      </form>
    </div>
  );
}
