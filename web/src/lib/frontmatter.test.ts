import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { assemble, fmToJS, setFmValue } from './frontmatter';
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
    expect(next).toContain('  - type: regulatory');
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
});
