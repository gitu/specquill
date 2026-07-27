// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { LoginView } from './LoginView';

// With both providers enabled each form must own its error: a bad password
// may not surface under the token field (it reads as "your token is wrong"),
// and a local failure must be visible at all — it used to be suppressed
// whenever the forge provider was present.

const PROVIDERS = {
  local: true,
  forge: { kind: 'gitlab', tokenCreateUrl: 'https://gitlab.example.com/-/user_settings/personal_access_tokens', scopes: ['api'] },
};

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null, setItem: () => {}, removeItem: () => {}, clear: () => {},
  });
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (url === '/auth/providers') {
      return new Response(JSON.stringify(PROVIDERS), { headers: { 'Content-Type': 'application/json' } });
    }
    if (url === '/auth/local/login') {
      return new Response(JSON.stringify({ error: 'invalid credentials' }), {
        status: 401, headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url === '/auth/pat/login') {
      return new Response(JSON.stringify({ error: 'token rejected by gitlab' }), {
        status: 401, headers: { 'Content-Type': 'application/json' },
      });
    }
    throw new Error('unexpected ' + url);
  }));
  Object.defineProperty(window, 'location', { writable: true, value: { href: '' } });
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

async function renderLogin() {
  await act(async () => { root.render(<LoginView />); });
  // let the providers fetch resolve
  await act(async () => { await Promise.resolve(); });
}

function forms() {
  const all = container.querySelectorAll('form');
  return { pat: all[0], local: all[1] };
}

async function submit(form: Element) {
  await act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
  await act(async () => { await Promise.resolve(); });
}

describe('LoginView error reporting', () => {
  it('offers both forms when the deployment enables both', async () => {
    await renderLogin();
    const { pat, local } = forms();
    expect(pat).toBeTruthy();
    expect(local).toBeTruthy();
    expect(container.textContent).toContain('personal access token');
  });

  it('shows a local-login failure under the local form only', async () => {
    await renderLogin();
    const { pat, local } = forms();
    const input = local.querySelector('input') as HTMLInputElement;
    await act(async () => {
      input.value = 'flo';
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await submit(local);

    expect(local.textContent).toContain('invalid credentials');
    expect(pat.textContent).not.toContain('invalid credentials');
  });

  it('shows a token failure under the token form only', async () => {
    await renderLogin();
    const { pat, local } = forms();
    const input = pat.querySelector('input') as HTMLInputElement;
    await act(async () => {
      input.value = 'glpat-bad';
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await submit(pat);

    expect(pat.textContent).toContain('token rejected');
    expect(local.textContent).not.toContain('token rejected');
  });
});
