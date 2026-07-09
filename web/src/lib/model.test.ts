import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { buildModel, excalidrawToSvg, extractReferences, isReservedMd, parseProps, stripFrontmatter } from './model';

const REPO = join(fileURLToPath(new URL('.', import.meta.url)), '../../../repo');

function loadRepo(): Record<string, string> {
  const files: Record<string, string> = {};
  for (const folder of ['regulations', 'requirements', 'specs', 'data-mappings', 'changes']) {
    for (const name of readdirSync(join(REPO, folder))) {
      files[`${folder}/${name}`] = readFileSync(join(REPO, folder, name), 'utf8');
    }
  }
  return files;
}

describe('buildModel over the demo repo', () => {
  const model = buildModel(loadRepo());

  it('finds all entities', () => {
    expect(model.regs).toHaveLength(3);
    expect(model.requirements).toHaveLength(6);
    expect(model.specs).toHaveLength(2);
    expect(model.maps).toHaveLength(2);
    expect(model.changes).toHaveLength(4);
    expect(model.fields.length).toBeGreaterThanOrEqual(5);
  });

  it('detects the executionTimestamp drift', () => {
    const drifts = model.fields.filter((f) => f.drift);
    expect(drifts).toHaveLength(1);
    expect(drifts[0].name).toBe('trade.executionTimestamp');
  });

  it('parses requirement links and drivers', () => {
    const req = model.requirements.find((r) => r.id === 'REQ-042')!;
    expect(req.drivers.map((d) => d.type)).toEqual(['regulatory', 'product']);
    expect(req.implements).toContain('specs/txn-report.md');
    expect(req.coverage).toBeCloseTo(0.82);
  });

  it('extracts change impact and diff', () => {
    const chg = model.changes.find((c) => c.id === 'CHG-2026-06-mifid-rts22')!;
    expect(chg.impReqs).toEqual(['REQ-042', 'REQ-051']);
    expect(chg.diff).toContain('microsecond');
    expect(chg.summary).toContain('microseconds');
  });
});

describe('parseProps', () => {
  it('keeps frontmatter order and folds lists', () => {
    const raw = readFileSync(join(REPO, 'requirements/REQ-042.md'), 'utf8');
    const props = parseProps(stripFrontmatter(raw).fm);
    const keys = props.map((p) => p.key);
    expect(keys[0]).toBe('id');
    expect(keys).toContain('drivers');
    const drivers = props.find((p) => p.key === 'drivers')!;
    expect(drivers.type).toBe('list');
  });
});

describe('excalidrawToSvg', () => {
  it('renders the demo sketch', () => {
    const raw = readFileSync(join(REPO, 'diagrams/data-flow.excalidraw'), 'utf8');
    const svg = excalidrawToSvg(JSON.parse(raw));
    expect(svg.startsWith('<svg')).toBe(true);
    expect(svg).toContain('Transform');
  });
});

describe('OKF support', () => {
  it('reserved files are not concepts', () => {
    expect(isReservedMd('index.md')).toBe(true);
    expect(isReservedMd('requirements/index.md')).toBe(true);
    expect(isReservedMd('log.md')).toBe(true);
    expect(isReservedMd('requirements/REQ-001.md')).toBe(false);
    // the fixture now carries generated index.md files — entity counts must
    // not absorb them (checked by the counts above), and buildModel must not
    // create reference edges from them either
    const model = buildModel({ ...loadRepo(), 'requirements/index.md': '# requirements\n\n- [x](/specs/venue.md)\n' });
    expect(model.references.every((r) => !isReservedMd(r.from) && !isReservedMd(r.to))).toBe(true);
  });

  it('extracts body links as untyped references', () => {
    const refs = extractReferences({
      'specs/a.md': '---\ntype: Specification\n---\n\nSee [the req](../requirements/REQ-1.md#sec) and [ext](https://x.test/y.md).\n\n```md\n[not a link](../requirements/REQ-2.md)\n```\n',
      'requirements/REQ-1.md': '---\ntype: Requirement\n---\n\nAbsolute link to [a](/specs/a.md), self [me](REQ-1.md), broken [b](gone.md).\n',
      'requirements/REQ-2.md': '---\ntype: Requirement\n---\n\nno links\n',
    });
    expect(refs).toEqual([
      { from: 'specs/a.md', to: 'requirements/REQ-1.md' },   // relative, #anchor stripped
      { from: 'requirements/REQ-1.md', to: 'specs/a.md' },   // bundle-absolute
    ]);
  });
});

describe('cross-repo references', () => {
  it('captures ~source links as external references, tolerantly', () => {
    const refs = extractReferences({
      'specs/a.md': '---\ntype: Specification\n---\n\nSee [MiFID II](~regulations/regulations/mifid-ii.md) and [again](~regulations/regulations/mifid-ii.md#art-26).\n',
    });
    expect(refs).toEqual([{ from: 'specs/a.md', to: '~regulations/regulations/mifid-ii.md', external: true }]);
  });
});
