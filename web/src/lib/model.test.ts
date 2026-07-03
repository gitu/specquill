import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { buildModel, excalidrawToSvg, parseProps, stripFrontmatter } from './model';

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
