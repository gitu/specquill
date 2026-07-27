// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api, clearStoredPat, getStoredPat, storePat } from './client';

// The stored PAT is the credential of record in forge mode: a 401 must
// trigger exactly one silent re-login, and the token may only be discarded
// when the forge itself rejects it — never on a transient failure, or an
// outage would force every user to mint a new token.

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
  // jsdom serves an opaque origin, so it exposes no localStorage of its own
  const store = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => store.clear(),
  });
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

  it('forgets the token when the forge rejects it', async () => {
    storePat('glpat-revoked');
    install((url) => (url === '/auth/pat/login'
      ? jsonResponse(401, { error: 'token rejected' })
      : jsonResponse(401, { error: 'unauthenticated' })));

    await expect(api('/api/me')).rejects.toThrow();
    expect(getStoredPat()).toBeNull();
    expect(window.location.href).toBe('/auth/login');
  });

  it('forgets the token when it loses project access', async () => {
    storePat('glpat-no-access');
    install((url) => (url === '/auth/pat/login'
      ? jsonResponse(403, { code: 'no_project_access' })
      : jsonResponse(401, {})));

    await expect(api('/api/me')).rejects.toThrow();
    expect(getStoredPat()).toBeNull();
  });

  it('keeps the token when the server is briefly unavailable', async () => {
    storePat('glpat-x');
    install((url) => (url === '/auth/pat/login'
      ? jsonResponse(503, { error: 'forge unreachable' })
      : jsonResponse(401, {})));

    await expect(api('/api/me')).rejects.toThrow();
    expect(getStoredPat()).toBe('glpat-x'); // a 5xx is "try later", not "wrong token"
  });

  it('keeps the token when the network is down', async () => {
    storePat('glpat-x');
    install((url) => {
      if (url === '/auth/pat/login') throw new TypeError('Failed to fetch');
      return jsonResponse(401, {});
    });

    await expect(api('/api/me')).rejects.toThrow();
    expect(getStoredPat()).toBe('glpat-x');
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
