import type { CSSProperties } from 'react';

const cache = new Map<string, CSSProperties>();

/**
 * Parse an inline-style string ("display:flex;gap:8px") into a React style
 * object. Lets the components carry the design's style strings verbatim; the
 * derive layer keeps composing styles as strings exactly like the prototype.
 */
export function sx(s: string | undefined | null): CSSProperties | undefined {
  if (!s) return undefined;
  let obj = cache.get(s);
  if (obj) return obj;
  obj = {};
  for (const decl of s.split(';')) {
    const i = decl.indexOf(':');
    if (i < 0) continue;
    const prop = decl.slice(0, i).trim();
    const value = decl.slice(i + 1).trim();
    if (!prop) continue;
    const key = prop.startsWith('--') ? prop : prop.replace(/-([a-z])/g, (_, c) => c.toUpperCase());
    (obj as Record<string, string>)[key] = value;
  }
  cache.set(s, obj);
  return obj;
}
