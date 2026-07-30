import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { assemble, fmToJS, setFmValue, todayStr, touchUpdated } from './frontmatter';
import { stripFrontmatter } from './model';

const REPO = join(fileURLToPath(new URL('.', import.meta.url)), '../../../repo');

const allMd = (): [string, string][] => {
  const out: [string, string][] = [];
  for (const folder of ['regulations', 'requirements', 'specs', 'data-mappings', 'changes']) {
    for (const name of readdirSync(join(REPO, folder))) {
      if (name.endsWith('.md')) out.push([`${folder}/${name}`, readFileSync(join(REPO, folder, name), 'utf8')]);
    }
  }
  return out;
};

describe('strip + assemble is byte-identical for every repo file', () => {
  for (const [path, raw] of allMd()) {
    it(path, () => {
      const { fm, body } = stripFrontmatter(raw);
      expect(assemble(fm, body)).toBe(raw);
    });
  }
});

describe('setFmValue', () => {
  const raw = readFileSync(join(REPO, 'requirements/REQ-042.md'), 'utf8');
  const { fm } = stripFrontmatter(raw);

  it('changes one scalar and preserves everything else', () => {
    const next = setFmValue(fm, 'status', 'approved');
    expect(next).toContain('status: approved');
    // untouched neighbours keep their exact formatting
    expect(next).toContain('value_statement: "Avoids MiFID RTS 22 reporting fines');
    expect(next).toContain('  - regulations/mifid-ii.md#rts-22-art-26');
    expect(next).toContain('coverage: 0.82');
  });

  it('updates a list', () => {
    const next = setFmValue(fm, 'implements', ['specs/txn-report.md', 'specs/venue.md']);
    const js = fmToJS(next);
    expect(js.implements).toEqual(['specs/txn-report.md', 'specs/venue.md']);
    expect(js.owner).toBe('s.grant');
  });

  it('percent round-trips as a number', () => {
    const next = setFmValue(fm, 'coverage', 0.9);
    expect(fmToJS(next).coverage).toBe(0.9);
  });

  it('deletes a key when value is undefined', () => {
    const next = setFmValue(fm, 'owner', undefined);
    expect(fmToJS(next).owner).toBeUndefined();
  });

  it('appends a new key at the end, preserving the rest', () => {
    const next = setFmValue(fm, 'jurisdiction', 'EU');
    expect(next.trimEnd().endsWith('jurisdiction: EU')).toBe(true);
    expect(next).toContain('value_statement: "Avoids MiFID RTS 22 reporting fines');
    expect(next).toContain('  - regulations/mifid-ii.md#rts-22-art-26');
  });

  it('creates frontmatter from scratch for a doc without any', () => {
    const { fm: none, body } = stripFrontmatter('# Title\n\nbody\n');
    expect(none).toBe('');
    const next = setFmValue(none, 'status', 'draft');
    expect(assemble(next, body)).toBe('---\nstatus: draft\n---\n# Title\n\nbody\n');
  });
});

describe('touchUpdated', () => {
  const now = new Date(2026, 6, 29); // 2026-07-29 local

  it('formats today as local YYYY-MM-DD', () => {
    expect(todayStr(now)).toBe('2026-07-29');
  });

  it('bumps a stale updated date and preserves neighbours', () => {
    const raw = readFileSync(join(REPO, 'requirements/REQ-042.md'), 'utf8');
    const { fm } = stripFrontmatter(raw);
    const next = touchUpdated(fm, now);
    expect(fmToJS(next).updated).toBe('2026-07-29');
    expect(next).toContain('  - regulations/mifid-ii.md#rts-22-art-26');
  });

  it('adds the key when frontmatter exists without one', () => {
    expect(touchUpdated('status: draft', now)).toBe('status: draft\nupdated: 2026-07-29');
  });

  it('is byte-stable when updated is already today', () => {
    const fm = 'status: draft\nupdated:   2026-07-29   # note';
    expect(touchUpdated(fm, now)).toBe(fm);
  });

  it('leaves documents without frontmatter alone', () => {
    expect(touchUpdated('', now)).toBe('');
  });
});
