import { describe, expect, it } from 'vitest';
import { existingIds, generateId, hasRandomToken, hasSlugToken, idPattern, slugify, slugifyPath } from './ids';

const CONFIG = `version: 2
project: t

ids:
  requirement: { pattern: "REQ-{yy}-{seq:2}" }
  spec: { pattern: "{adj}-{word}" }

statuses: [draft]
`;

describe('idPattern', () => {
  it('falls back to built-in patterns per kind', () => {
    expect(idPattern('requirement')).toBe('REQ-{seq:3}');
    expect(idPattern('change')).toBe('CHG-{seq:3}');
    expect(idPattern('decision')).toBe('ADR-{seq:3}');
  });
  it('unknown kinds name files after the title', () => {
    expect(idPattern('glossary')).toBe('{slug}');
    expect(idPattern('custom_thing')).toBe('{slug}');
  });
  it('config ids: block overrides built-ins', () => {
    expect(idPattern('requirement', CONFIG)).toBe('REQ-{yy}-{seq:2}');
    expect(idPattern('spec', CONFIG)).toBe('{adj}-{word}');
    expect(idPattern('change', CONFIG)).toBe('CHG-{seq:3}'); // untouched
  });
  it('parses a block sitting at the end of the file', () => {
    expect(idPattern('spec', 'version: 2\nids:\n  spec: { pattern: "S-{seq:2}" }')).toBe('S-{seq:2}');
  });
  it('an entry without a pattern field falls back to the built-in', () => {
    expect(idPattern('requirement', 'ids:\n  requirement: { prefix: "X" }\n')).toBe('REQ-{seq:3}');
  });
  it('comment lines inside the block are ignored', () => {
    const yml = 'ids:\n  # sequential\n  requirement: { pattern: "R-{seq:2}" }\n  spec: { pattern: "{slug}" }\n';
    expect(idPattern('requirement', yml)).toBe('R-{seq:2}');
    expect(idPattern('spec', yml)).toBe('{slug}');
  });
  it("the legacy v1 'id_pattern:' validation key is not an ids: block", () => {
    expect(idPattern('requirement', 'id_pattern: "REQ-\\\\d{3}"\n')).toBe('REQ-{seq:3}');
  });
});

describe('existingIds', () => {
  const files = {
    'requirements/REQ-005.md': '---\nid: REQ-005\ntype: Requirement\n---\n\n# x\n',
    'requirements/auth/REQ-090.md': '---\ntype: Requirement\n---\n\n# y\n',
    'specs/venue.md': '---\nid: SPEC-venue\ntype: Specification\n---\n',
  };
  it('collects family filename stems and workspace-wide frontmatter ids', () => {
    const taken = existingIds(files, 'requirements/');
    expect(taken.has('req-005')).toBe(true);      // stem + frontmatter
    expect(taken.has('req-090')).toBe(true);      // subfolder stem
    expect(taken.has('spec-venue')).toBe(true);   // foreign-family frontmatter id
    expect(taken.has('venue')).toBe(false);       // foreign-family stem is not taken
  });
  it('lowercases frontmatter ids and handles files without frontmatter', () => {
    const taken = existingIds({
      'specs/Overview.md': 'no frontmatter here',
      'specs/x.md': '---\nid: SPEC-X\n---\n',
    }, 'specs/');
    expect(taken.has('overview')).toBe(true);
    expect(taken.has('spec-x')).toBe(true);
  });
  it('non-markdown files only shed their last extension', () => {
    const taken = existingIds({ 'diagrams/flow.excalidraw.png': '' }, 'diagrams/');
    expect(taken.has('flow.excalidraw')).toBe(true);
  });
});

describe('generateId', () => {
  it('{seq} continues after the highest matching existing id', () => {
    const taken = new Set(['req-005', 'req-090', 'index', 'notes-2024']);
    expect(generateId('REQ-{seq:3}', taken)).toEqual({ id: 'REQ-091', conflict: false });
  });
  it('{seq} ignores ids that do not match the pattern shape', () => {
    const taken = new Set(['req-999-legacy', 'adr-004']);
    expect(generateId('REQ-{seq:3}', taken).id).toBe('REQ-001');
  });
  it('pads {seq:N} and defaults to 3', () => {
    expect(generateId('A-{seq:5}', new Set()).id).toBe('A-00001');
    expect(generateId('A-{seq}', new Set()).id).toBe('A-001');
  });
  it('word tokens produce memorable lowercase pairs', () => {
    const { id, conflict } = generateId('{adj}-{word}', new Set());
    expect(conflict).toBe(false);
    expect(id).toMatch(/^[a-z]+-[a-z]+$/);
  });
  it('random tokens re-roll around conflicts', () => {
    // 9 of the 10 single-digit ids are taken — must land on the free one
    const taken = new Set(['x-0', 'x-1', 'x-2', 'x-3', 'x-4', 'x-5', 'x-6', 'x-7', 'x-8']);
    expect(generateId('X-{rand:1}', taken)).toEqual({ id: 'X-9', conflict: false });
  });
  it('reports a conflict when every candidate is taken', () => {
    const taken = new Set(Array.from({ length: 10 }, (_, i) => `x-${i}`));
    expect(generateId('X-{rand:1}', taken).conflict).toBe(true);
  });
  it('conflicts are case-insensitive', () => {
    expect(generateId('REQ-{seq:3}', new Set(['req-001'])).id).toBe('REQ-002');
  });
  it('{slug} derives from the title', () => {
    expect(generateId('{slug}', new Set(), 'Venue Matching & Routing!').id).toBe('venue-matching-routing');
  });
  it('{slug} without a title degrades to a memorable word pair, conflict-checked', () => {
    const { id, conflict } = generateId('{slug}', new Set(['venue']));
    expect(conflict).toBe(false);
    expect(id).toMatch(/^[a-z]+-[a-z]+$/);
    expect(id).not.toBe('untitled');
    // whitespace-only titles count as empty
    expect(generateId('{slug}', new Set(), '  !? ').id).toMatch(/^[a-z]+-[a-z]+$/);
  });
  it('{yyyy} stamps the current year', () => {
    expect(generateId('ADR-{yyyy}-{seq:2}', new Set()).id).toBe(`ADR-${new Date().getFullYear()}-01`);
  });
  it('{seq} treats year tokens as wildcards — the counter spans years', () => {
    expect(generateId('ADR-{yyyy}-{seq:2}', new Set(['adr-2025-07'])).id).toBe(`ADR-${new Date().getFullYear()}-08`);
  });
  it('{seq} grows past its padding without truncation', () => {
    expect(generateId('REQ-{seq:3}', new Set(['req-999'])).id).toBe('REQ-1000');
  });
  it('{hex:N} and bare {rand} expand with their widths', () => {
    expect(generateId('X-{hex:4}', new Set()).id).toMatch(/^X-[0-9a-f]{4}$/);
    expect(generateId('X-{rand}', new Set()).id).toMatch(/^X-\d{3}$/);
  });
  it('a token-free pattern is a fixed id — conflict when taken', () => {
    expect(generateId('SINGLETON', new Set())).toEqual({ id: 'SINGLETON', conflict: false });
    expect(generateId('SINGLETON', new Set(['singleton'])).conflict).toBe(true);
  });
});

describe('token predicates', () => {
  it('detect regenerable and title-tracking patterns', () => {
    expect(hasRandomToken('{adj}-{word}')).toBe(true);
    expect(hasRandomToken('REQ-{seq:3}')).toBe(false);
    expect(hasSlugToken('{slug}')).toBe(true);
    expect(hasSlugToken('REQ-{seq:3}')).toBe(false);
  });
});

describe('slugify', () => {
  it('kebab-cases and bounds titles', () => {
    expect(slugify('  Hello, World! ')).toBe('hello-world');
    expect(slugify('ÜmläutFree')).toBe('ml-utfree');
    expect(slugify('x'.repeat(80))).toHaveLength(64);
  });
  it('slugifyPath keeps nesting, slugifies per segment, drops empties', () => {
    expect(slugifyPath('Auth / Session Mgmt')).toBe('auth/session-mgmt');
    expect(slugifyPath('/a//b/')).toBe('a/b');
    expect(slugifyPath('///')).toBe('');
  });
});
