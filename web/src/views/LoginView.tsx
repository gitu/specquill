import { FormEvent, useEffect, useState } from 'react';
import { sx } from '../lib/sx';
import { api, storePat, takeLoginError } from '../api/client';

interface ForgeProvider { kind: 'github' | 'gitlab'; baseUrl?: string; tokenCreateUrl: string; scopes: string[] }
interface Providers { local: boolean; forge?: ForgeProvider }

const input = 'width:100%;height:34px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px';
const label = 'display:block;font-size:11.5px;font-weight:600;color:var(--text-2);margin-bottom:5px';
const button = 'width:100%;height:36px;border:1px solid var(--border-2);border-radius:9px;background:var(--surface-2);color:var(--text);font-family:inherit;font-size:13px;font-weight:600;cursor:pointer';

// Login page: offers whatever /auth/providers reports — a forge personal
// access token (the token stays in this browser; the server keeps it in RAM
// per session only) and/or a local account.
export function LoginView() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [token, setToken] = useState('');
  // one error state per form: a failed local login must not surface under the
  // PAT field (and vice versa), and each form must show its own failure
  const [patError, setPatError] = useState('');
  const [localError, setLocalError] = useState('');
  const [warning, setWarning] = useState('');
  const [busy, setBusy] = useState(false);
  const [providers, setProviders] = useState<Providers | null>(null);
  // why the silent re-login bounced here — the saved token is still in this
  // browser; signing in again (same or new token) simply overwrites it
  const [autoError] = useState(() => takeLoginError());

  useEffect(() => {
    api<Providers>('/auth/providers')
      .then(setProviders)
      .catch(() => setProviders({ local: true }));
  }, []);

  const submitPat = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setPatError('');
    try {
      const resp = await api<{ warning?: string }>('/auth/pat/login', {
        method: 'POST', body: JSON.stringify({ token: token.trim() }),
      });
      storePat(token.trim());
      if (resp.warning) {
        // scope shortfall: sign-in worked, but pushing may not — say so
        // before leaving the page where the user can still mint a new token
        setWarning(resp.warning);
        setTimeout(() => { window.location.href = '/'; }, 3500);
      } else {
        window.location.href = '/';
      }
    } catch (err) {
      setPatError(String((err as Error).message || err));
    } finally {
      setBusy(false);
    }
  };

  const submitLocal = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setLocalError('');
    try {
      await api('/auth/local/login', { method: 'POST', body: JSON.stringify({ username, password }) });
      window.location.href = '/';
    } catch (err) {
      setLocalError(String((err as Error).message || err));
    } finally {
      setBusy(false);
    }
  };

  const forge = providers?.forge;
  const forgeName = forge?.kind === 'github' ? 'GitHub' : 'GitLab';

  return (
    <div style={sx('height:100vh;display:flex;align-items:center;justify-content:center;background:var(--bg)')}>
      <div style={sx('width:360px;background:var(--surface);border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow-lg);padding:28px 26px')}>
        <div style={sx('display:flex;align-items:center;gap:9px;margin-bottom:18px')}>
          <div style={sx('width:26px;height:26px;border-radius:7px;background:var(--text);display:flex;align-items:center;justify-content:center')}>
            <div style={sx('width:10px;height:10px;border-radius:2px;border:2px solid var(--surface);transform:rotate(45deg)')} />
          </div>
          <span style={sx('font-weight:700;font-size:17px;letter-spacing:-.2px')}>specquill</span>
        </div>

        {autoError && (
          <div style={sx('margin-bottom:12px;padding:9px 12px;border:1px solid var(--del);border-radius:8px;color:var(--del);font-size:12px;line-height:1.5')}>
            Signing in with the saved token failed — {autoError}
          </div>
        )}

        {warning && (
          <div style={sx('margin-bottom:12px;padding:9px 12px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:8px;color:var(--reg);font-size:12px')}>
            Signed in — {warning}
          </div>
        )}

        {forge && (
          <form onSubmit={submitPat}>
            <label style={sx(label)}>{forgeName} personal access token</label>
            <input type="password" value={token} onChange={(e) => setToken(e.target.value)} autoFocus
              placeholder={forge.kind === 'github' ? 'ghp_…' : 'glpat-…'}
              style={sx(input + ';margin-bottom:8px')} />
            <div style={sx('font-size:11.5px;color:var(--text-3);margin-bottom:14px;line-height:1.5')}>
              Needs the <code style={sx("font-family:'JetBrains Mono',monospace")}>{forge.scopes.join(', ')}</code> scope
              {forge.scopes.length === 1 ? '' : 's'}.{' '}
              <a href={forge.tokenCreateUrl} target="_blank" rel="noreferrer" style={sx('color:var(--brand,var(--text))')}>
                Create one on {forgeName} ↗
              </a>
              <br />The token stays in this browser — the server never stores it.
            </div>
            {patError && <div style={sx('margin-bottom:12px;color:var(--del);font-size:12px')}>{patError}</div>}
            <button type="submit" disabled={busy || !token.trim()} style={sx(button)}>
              {busy ? 'Signing in…' : 'Sign in with ' + forgeName}
            </button>
          </form>
        )}

        {forge && providers?.local && (
          <div style={sx('display:flex;align-items:center;gap:10px;margin:16px 0;color:var(--text-3);font-size:11px')}>
            <div style={sx('flex:1;height:1px;background:var(--border)')} />or<div style={sx('flex:1;height:1px;background:var(--border)')} />
          </div>
        )}

        {providers?.local && (
          <form onSubmit={submitLocal}>
            <label style={sx(label)}>Username</label>
            <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus={!forge}
              style={sx(input + ';margin-bottom:13px')} />
            <label style={sx(label)}>Password</label>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
              style={sx(input + ';margin-bottom:16px')} />
            {localError && <div style={sx('margin-bottom:12px;color:var(--del);font-size:12px')}>{localError}</div>}
            <button type="submit" disabled={busy} style={sx(button)}>
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </form>
        )}

        {providers && !providers.local && !providers.forge && (
          <div style={sx('font-size:12.5px;color:var(--text-2)')}>No login methods are enabled — check the server configuration.</div>
        )}
      </div>
    </div>
  );
}
