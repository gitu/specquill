import { describe, expect, it } from 'vitest';
import { BUILTIN_ENTITIES, parseEntities } from './entities';
import { buildTree } from './derive';
import { resolveDocHref } from './model';

describe('parseEntities', () => {
  it('returns the built-ins for an empty config', () => {
    expect(parseEntities('')).toEqual(BUILTIN_ENTITIES);
    expect(parseEntities('statuses: [draft]\n')).toEqual(BUILTIN_ENTITIES);
  });

  it('adds custom entities with defaults for omitted fields', () => {
    const yml = 'entities:\n  decision: { folder: "decisions/", label: "Decisions", icon: "◆", color: "#7c5cd6", description: "Why, with alternatives." }\n  risk: { }\n';
    const out = parseEntities(yml);
    const dec = out.find((e) => e.kind === 'decision')!;
    expect(dec).toMatchObject({ folder: 'decisions/', label: 'Decisions', icon: '◆', color: '#7c5cd6', builtin: false });
    expect(dec.description).toContain('alternatives');
    const risk = out.find((e) => e.kind === 'risk')!;
    expect(risk.folder).toBe('risks/'); // kind + s
    expect(risk.label).toBe('risk');
  });

  it('overrides only the provided fields of a built-in', () => {
    const out = parseEntities('entities:\n  requirement: { label: "User needs" }\n');
    const req = out.find((e) => e.kind === 'requirement')!;
    expect(req.label).toBe('User needs');
    expect(req.folder).toBe('requirements/'); // untouched
    expect(req.builtin).toBe(true);
  });

  it('stops at the next top-level key and normalizes a missing trailing slash', () => {
    const out = parseEntities('entities:\n  adr: { folder: "adrs" }\nstatuses: [draft]\n');
    expect(out.find((e) => e.kind === 'adr')!.folder).toBe('adrs/');
    expect(out.find((e) => e.kind === 'statuses')).toBeUndefined();
  });
});

describe('buildTree with entities', () => {
  const entities = parseEntities('entities:\n  decision: { folder: "decisions/", icon: "◆", color: "#7c5cd6", description: "ADRs." }\n');
  const files = {
    'requirements/REQ-001.md': '', 'decisions/ADR-001.md': '', 'notes/scratch.md': '',
    'index.md': '', '.specquill/config.yml': '',
  };

  it('shows entity folders first, then unknown folders; hides root files and dot-folders', () => {
    const folders = buildTree(files, undefined, {}, entities);
    expect(folders.map((f) => f.name)).toEqual(['requirements', 'decisions', 'notes']);
  });

  it('carries entity descriptions and icons; unknown folders get generic meta', () => {
    const folders = buildTree(files, undefined, {}, entities);
    expect(folders.find((f) => f.name === 'decisions')!.desc).toBe('ADRs.');
    expect(folders.find((f) => f.name === 'decisions')!.files[0].icon).toBe('◆');
    expect(folders.find((f) => f.name === 'notes')!.files[0].icon).toBe('▢');
  });
});

describe('resolveDocHref', () => {
  it('treats leading-slash links as root-relative (OKF index style)', () => {
    expect(resolveDocHref('requirements', '/requirements/REQ-011.md')).toBe('requirements/REQ-011.md');
    expect(resolveDocHref('', '/specs/urls.md')).toBe('specs/urls.md');
  });
  it('resolves explicit relative links against the document dir', () => {
    expect(resolveDocHref('specs', '../requirements/REQ-001.md')).toBe('requirements/REQ-001.md');
    expect(resolveDocHref('specs', './venue.md')).toBe('specs/venue.md');
  });
  it('resolves bare paths relative to the document dir (markdown standard)', () => {
    expect(resolveDocHref('', 'specs/venue.md')).toBe('specs/venue.md');
    expect(resolveDocHref('requirements', 'assets/shot.png')).toBe('requirements/assets/shot.png');
    expect(resolveDocHref('specs', 'venue.md')).toBe('specs/venue.md');
  });
  it('passes cross-repo targets through and strips anchors', () => {
    expect(resolveDocHref('specs', '~regulations/regulations/mifid-ii.md')).toBe('~regulations/regulations/mifid-ii.md');
    expect(resolveDocHref('specs', '/specs/urls.md#scheme')).toBe('specs/urls.md');
  });
});
