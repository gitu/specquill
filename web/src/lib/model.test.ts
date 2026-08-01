import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { buildModel, excalidrawToSvg, extractReferences, isReservedMd, parseProps, stripFrontmatter } from './model';
import { workspaceConfig } from './config';
import { buildDashboard } from './derive';

const REPO = join(fileURLToPath(new URL('.', import.meta.url)), '../../../repo');

function loadRepo(): Record<string, string> {
  const files: Record<string, string> = {};
  for (const folder of ['regulations', 'requirements', 'specs', 'data-mappings', 'products']) {
    for (const name of readdirSync(join(REPO, folder))) {
      files[`${folder}/${name}`] = readFileSync(join(REPO, folder, name), 'utf8');
    }
  }
  files['.specquill/config.yml'] = readFileSync(join(REPO, '.specquill/config.yml'), 'utf8');
  return files;
}

describe('buildModel over the demo repo', () => {
  const files = loadRepo();
  const model = buildModel(files, workspaceConfig(files['.specquill/config.yml']));

  it('finds all entities', () => {
    expect(model.regs).toHaveLength(3);
    expect(model.requirements).toHaveLength(6);
    expect(model.specs).toHaveLength(2);
    expect(model.maps).toHaveLength(2);
    // documents with a validity window join the timeline, whatever family
    expect(model.timed.map((t) => t.path).sort()).toEqual([
      'products/ops-t1-settlement-sla.md',
      'regulations/mifid-ii.md',
      'requirements/REQ-042.md',
      'requirements/REQ-063.md',
      'requirements/REQ-070.md',
      'requirements/REQ-090.md',
      'requirements/REQ-095.md',
    ]);
    expect(model.fields.length).toBeGreaterThanOrEqual(5);
  });

  it('detects the executionTimestamp drift', () => {
    const drifts = model.fields.filter((f) => f.drift);
    expect(drifts).toHaveLength(1);
    expect(drifts[0].name).toBe('trade.executionTimestamp');
  });

  it('parses requirement links and derives driver types from the targets', () => {
    const req = model.requirements.find((r) => r.id === 'REQ-042')!;
    // flat path list; regulations/* derives regulatory (entity driver), the
    // products/* target derives product (custom entity's driver key)
    expect(req.drivers.map((d) => d.ref)).toEqual(['regulations/mifid-ii.md#rts-22-art-26', 'products/ops-t1-settlement-sla.md']);
    expect(req.drivers.map((d) => d.type)).toEqual(['regulatory', 'product']);
    expect(req.coverage).toBeCloseTo(0.82);
    // the upward chain: the SPEC carries implements -> requirement
    const spec = model.specs.find((s) => s.path === 'specs/txn-report.md')!;
    expect(spec.implements).toContain('requirements/REQ-042.md');
    expect(model.requirements.every((r) => r.implements.length === 0)).toBe(true);
  });

  it('the demo workspace reads healthy along the chain', () => {
    // guards the fixture migration: with upward links, no chain bar sits at
    // 0% for want of direction (the old bug this rework fixes)
    const d = buildDashboard(model);
    const bars = Object.fromEntries(d.health.map((h) => [h.label, h.pct]));
    expect(bars['Requirements → drivers']).toBe(100);
    expect(bars['Requirements ← specs']).toBeGreaterThan(50); // 4 of 6 implemented
    expect(bars['Specs → data fields']).toBe(100);
  });

  it('classifies by frontmatter type first, folder only as fallback', () => {
    const files = {
      // type wins over location: a requirement parked outside its folder
      'inbox/parked.md': '---\ntype: Requirement\nid: R-9\ntitle: Parked\ndrivers:\n  - regulations/a.md\n---\n',
      // normalized forms all land: kind, spaced, cased
      'notes/a.md': '---\ntype: spec\ntitle: A\n---\n',
      'notes/b.md': '---\ntype: work_item\ntitle: B\n---\n',
      // a family the workspace declares itself (change records are no longer built in)
      'notes/c.md': '---\ntype: Change Record\ntitle: C\nsource: product\nstatus: triage\n---\n',
      // no recognizable type → the folder decides
      'specs/typeless.md': '---\ntitle: T\n---\n',
      // neither type nor folder → unclassified
      'misc/loose.md': '---\ntype: Guide\ntitle: L\n---\n',
      'regulations/a.md': '---\nid: REG-a\ntitle: Reg A\n---\n',
    };
    const cfg = workspaceConfig('entities:\n  change: { doc_type: "Change Record", group: why, folder: "changes/", label: "Changes" }\n');
    const m = buildModel(files, cfg);
    const kindOf = (p: string) => m.docs.find((d) => d.path === p)?.kind;
    expect(kindOf('inbox/parked.md')).toBe('requirement');
    expect(kindOf('notes/a.md')).toBe('spec');
    expect(kindOf('notes/b.md')).toBe('work_item');
    expect(kindOf('notes/c.md')).toBe('change');
    expect(kindOf('specs/typeless.md')).toBe('spec');
    expect(kindOf('misc/loose.md')).toBeUndefined();
    // the parked requirement fully participates: backlinks reach its driver
    expect(buildDashboard(m).health.find((h) => h.label === 'Requirements → drivers')!.pct).toBe(100);
  });

  it('classifies docs by the configured entities, custom families included', () => {
    const prod = model.docs.find((d) => d.path === 'products/ops-t1-settlement-sla.md')!;
    expect(prod.kind).toBe('product_driver');
    expect(prod.group).toBe('why');
  });

  it('reads the validity window from the first configured key present', () => {
    const req = model.timed.find((t) => t.path === 'requirements/REQ-042.md')!;
    expect(req).toMatchObject({ startKey: 'starts', start: '2026-09-01', end: '', status: 'in_review' });
    // regulatory wording resolves through the same config
    const reg = model.timed.find((t) => t.path === 'regulations/mifid-ii.md')!;
    expect(reg.startKey).toBe('effective_from');
    expect(reg.start).toBe('2026-01-03');
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
