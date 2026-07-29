// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api, clearStoredPat, getStoredPat, storePat, takeLoginError } from './client';

// The stored PAT is the credential of record in forge mode: a 401 must
// trigger exactly one silent re-login, and the token is NEVER discarded by
// code — a failed re-login parks its reason for the login page and leaves
// the token alone; only an explicit logout removes it.

type Handler = (url: string, init?: RequestInit) => Response | Promise<Response>;

function jsonResponse(status: number, body: unknown = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

let calls: string[] = [];

function install(handler: Handler) {
  calls = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? 'GET'} ${url}`);
    return handler(url, init);
  }));
}

beforeEach(() => {
  // jsdom serves an opaque origin, so it exposes no web storage of its own
  const mapStorage = () => {
    const store = new Map<string, string>();
    return {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
      clear: () => store.clear(),
    };
  };
  vi.stubGlobal('localStorage', mapStorage());
  vi.stubGlobal('sessionStorage', mapStorage());
  // jsdom refuses real navigation; the 401 fallback only sets href
  Object.defineProperty(window, 'location', { writable: true, value: { href: '' } });
});
afterEach(() => vi.unstubAllGlobals());

describe('PAT re-authentication', () => {
  it('re-logs-in once on 401 and retries the original request', async () => {
    storePat('glpat-x');
    let apiCalls = 0;
    install((url) => {
      if (url === '/auth/pat/login') return jsonResponse(200, { id: 1 });
      apiCalls++;
      return apiCalls === 1 ? jsonResponse(401, { error: 'token_gone' }) : jsonResponse(200, { ok: true });
    });

    await expect(api<{ ok: boolean }>('/api/me')).resolves.toEqual({ ok: true });
    expect(calls.filter((c) => c.includes('/auth/pat/login'))).toHaveLength(1);
    expect(getStoredPat()).toBe('glpat-x');
  });

  it('keeps the token when the forge rejects it, and parks the reason', async () => {
    storePat('glpat-revoked');
    install((url) => (url === '/auth/pat/login'
      ? jsonResponse(401, { error: 'token rejected by gitlab: 401' })
      : jsonResponse(401, { error: 'unauthenticated' })));

    await expect(api('/api/me')).rejects.toThrow();
    expect(getStoredPat()).toBe('glpat-revoked'); // surfaced, never deleted
    expect(window.location.href).toBe('/auth/login');
    expect(takeLoginError()).toBe('token rejected by gitlab: 401');
    expect(takeLoginError()).toBeNull(); // read-once
  });

  it('keeps the token when it loses project access, and parks the reason', async () => {
    storePat('glpat-no-access');
    install((url) => (url === '/auth/pat/login'
      ? jsonResponse(403, { error: 'this token has no access' })
      : jsonResponse(401, {})));

    await expect(api('/api/me')).rejects.toThrow();
    expect(getStoredPat()).toBe('glpat-no-access');
    expect(takeLoginError()).toBe('this token has no access');
  });

  it('keeps the token when the forge is briefly unavailable', async () => {
    storePat('glpat-x');
    install((url) => (url === '/auth/pat/login'
      ? jsonResponse(502, { error: 'gitlab could not verify the token' })
      : jsonResponse(401, {})));

    await expect(api('/api/me')).rejects.toThrow();
    expect(getStoredPat()).toBe('glpat-x'); // a 502 is "try later", not "wrong token"
    expect(takeLoginError()).toBe('gitlab could not verify the token');
  });

  it('keeps the token when the network is down, and parks a generic reason', async () => {
    storePat('glpat-x');
    install((url) => {
      if (url === '/auth/pat/login') throw new TypeError('Failed to fetch');
      return jsonResponse(401, {});
    });

    await expect(api('/api/me')).rejects.toThrow();
    expect(getStoredPat()).toBe('glpat-x');
    expect(takeLoginError()).toMatch(/could not be reached/);
  });

  it('clears a stale parked reason once a re-login succeeds', async () => {
    storePat('glpat-x');
    let apiCalls = 0;
    install((url) => {
      if (url === '/auth/pat/login') return jsonResponse(200, { id: 1 });
      apiCalls++;
      return apiCalls === 1 ? jsonResponse(401, {}) : jsonResponse(200, { ok: true });
    });
    sessionStorage.setItem('specquill-login-error', 'old outage');

    await expect(api<{ ok: boolean }>('/api/me')).resolves.toEqual({ ok: true });
    expect(takeLoginError()).toBeNull(); // recovery removed the stale reason
  });

  it('does not re-login when no token is stored', async () => {
    clearStoredPat();
    install(() => jsonResponse(401, {}));
    await expect(api('/api/me')).rejects.toThrow();
    expect(calls.filter((c) => c.includes('/auth/pat/login'))).toHaveLength(0);
  });

  it('coalesces a burst of 401s into a single re-login', async () => {
    storePat('glpat-x');
    const seen = new Set<string>();
    install((url) => {
      if (url === '/auth/pat/login') return jsonResponse(200, {});
      if (!seen.has(url)) { // every endpoint 401s once, then succeeds
        seen.add(url);
        return jsonResponse(401, {});
      }
      return jsonResponse(200, { ok: true });
    });

    await Promise.all([api('/api/me'), api('/api/repos'), api('/api/projects')]);
    expect(calls.filter((c) => c.includes('/auth/pat/login'))).toHaveLength(1);
  });
});
