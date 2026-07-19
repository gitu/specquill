// Shared e2e constants: the dev server's config tenant and the tenant-scoped
// URL prefixes (REQ-022 — the tenant lives in every app and API path).
export const TENANT = 'default';
export const APP = `/t/${TENANT}`;         // SPA page URLs
export const API = `/api/t/${TENANT}`;     // API request URLs
export const H = { 'X-SpecQuill': '1' };   // CSRF header (unchanged)
