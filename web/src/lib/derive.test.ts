import { describe, expect, it } from 'vitest';
import { buildGraph, buildProps, buildTree, collectFieldValues, collectRefTargets, defaultDoc, docAnchorOptions, driverBacklinks, focusGraph } from './derive';
import { buildModel } from './model';
import { BUILTIN_ENTITIES } from './entities';

describe('buildTree', () => {
  const files = {
    'index.md': '# Index\n',
    'requirements/REQ-001.md': '---\ntype: Requirement\n---\n',
    'requirements/index.md': '# requirements\n',
    'requirements/log.md': '# Log\n',
  };

  it('marks OKF reserved files as generated', () => {
    const [reqs] = buildTree(files, undefined, {}, BUILTIN_ENTITIES);
    const by = Object.fromEntries(reqs.files.map((f) => [f.path, f.generated]));
    expect(by['requirements/REQ-001.md']).toBe(false);
    expect(by['requirements/index.md']).toBe(true);
    expect(by['requirements/log.md']).toBe(true);
  });
});

describe('defaultDoc', () => {
  it('never lands on a generated file when a concept doc exists', () => {
    const files = {
      'requirements/index.md': '# requirements\n',
      'requirements/REQ-001.md': '---\ntype: Requirement\n---\n',
    };
    expect(defaultDoc(files, BUILTIN_ENTITIES)).toBe('requirements/REQ-001.md');
  });

  it('falls back to the root index for repos with no concept docs', () => {
    expect(defaultDoc({ 'index.md': '# Index\n' }, BUILTIN_ENTITIES)).toBe('index.md');
  });
});

describe('collectFieldValues', () => {
  const files = {
    'requirements/REQ-001.md': '---\ntype: Requirement\nstatus: draft\nowner: flo\nvalue_statement: "A statement well past the forty character cutoff for options."\nimplements:\n  - specs/a.md\n---\nbody\n',
    'requirements/REQ-002.md': '---\ntype: Requirement\nstatus: approved\nowner: sam\n---\n',
    'requirements/index.md': '---\nstatus: generated\n---\n',
    'notes.txt': 'status: not-markdown',
  };
  const vals = collectFieldValues(files);

  it('collects distinct scalar values per key, sorted', () => {
    expect(vals.status).toEqual(['approved', 'draft']);
    expect(vals.owner).toEqual(['flo', 'sam']);
    expect(vals.type).toEqual(['Requirement']);
  });

  it('keeps list and prose keys as known keys with no values', () => {
    expect(vals.implements).toEqual([]);
    expect(vals.value_statement).toEqual([]);
  });

  it('skips reserved files and non-markdown', () => {
    expect(vals.status).not.toContain('generated');
    expect(vals.status).not.toContain('not-markdown');
  });
});

describe('buildProps', () => {
  it('badge-styles any values-map field, enum or badge typed (product schema shape)', () => {
    const schema = {
      order: ['status'],
      fields: { status: { label: 'Status', type: 'badge', values: { approved: 'data' } } },
    };
    const [row] = buildProps('status: approved', schema);
    expect(row.items[0].style).toContain('var(--data)');
    expect(row.items[0].style).toContain('border-radius:20px');
  });

  it('leaves plain text fields unstyled', () => {
    const [row] = buildProps('owner: flo', { order: [], fields: { owner: { label: 'Owner' } } });
    expect(row.items[0].style).not.toContain('border-radius:20px');
  });
});

describe('collectRefTargets', () => {
  const files = {
    'regulations/mifid-ii.md': '---\ntitle: "MiFID II"\nanchors: [rts-22-art-26]\n---\n## RTS 22 {#rts-22-art-26}\n\n## Art 9 {#art-9}\n',
    'diagrams/flow.mermaid': 'graph TD;',
    'requirements/index.md': '# generated\n',
    '.specquill/schema.json': '{}',
  };
  const field = { name: 'trade.venue', source: 'oms.venue_mic', transform: '', status: 'ok', drift: false, map: 'data-mappings/trade.md' };

  it('offers paths, declared + heading anchors, non-md files, and data fields', () => {
    const vals = collectRefTargets(files, [field]).map((t) => t.value);
    expect(vals).toContain('regulations/mifid-ii.md');
    expect(vals).toContain('regulations/mifid-ii.md#rts-22-art-26');
    expect(vals).toContain('regulations/mifid-ii.md#art-9');
    expect(vals).toContain('diagrams/flow.mermaid');
    expect(vals).toContain('data-mappings/trade.md#venue');
    expect(vals).not.toContain('requirements/index.md');
    expect(vals.some((v) => v.startsWith('.specquill'))).toBe(false);
  });

  it('dedupes anchors both declared and carried by a heading, and hints titles', () => {
    const targets = collectRefTargets(files);
    expect(targets.filter((t) => t.value === 'regulations/mifid-ii.md#rts-22-art-26')).toHaveLength(1);
    expect(targets.find((t) => t.value === 'regulations/mifid-ii.md')!.hint).toBe('MiFID II');
    expect(targets.find((t) => t.value === 'regulations/mifid-ii.md#art-9')!.hint).toBe('MiFID II');
  });
});

describe('docAnchorOptions', () => {
  it('uses explicit {#id} attributes verbatim and slugs plain headings', () => {
    const opts = docAnchorOptions('# Doc Title\n\n## Reporting window {#reporting-window}\n\n## Data Quality Rules\n');
    expect(opts.map((o) => o.value)).toEqual(['doc-title', 'reporting-window', 'data-quality-rules']);
    expect(opts[1].hint).toBe('Reporting window');
    expect(opts[2].hint).toContain('no {#id}');
  });

  it('dedupes repeated ids', () => {
    expect(docAnchorOptions('## A {#x}\n## B {#x}\n')).toHaveLength(1);
  });
});

describe('focusGraph', () => {
  const files = {
    'regulations/a.md': '---\nid: REG-a\ntitle: Reg A\n---\n',
    'requirements/R1.md': '---\nid: R1\ntitle: One\nstatus: draft\ndrivers:\n  - type: regulatory\n    ref: regulations/a.md#x\nimplements:\n  - specs/s1.md\n---\n',
    'specs/s1.md': '---\nid: S1\ntitle: Spec One\n---\n',
    'requirements/R2.md': '---\nid: R2\ntitle: Two\nstatus: draft\ndrivers:\n  - type: product\n    ref: products/p.md\n---\n',
  };
  const g = buildGraph(buildModel(files));

  it('keeps only the chain connected to the doc, up and down', () => {
    const f = focusGraph(g, 'requirements/R1.md');
    const ids = f.nodes.map((n) => n.id);
    expect(ids).toContain('req:requirements/R1.md');
    expect(ids).toContain('spec:specs/s1.md');
    expect(ids).toContain('src:regulatory|regulations/a.md#x');
    expect(ids).not.toContain('req:requirements/R2.md');
    expect(f.stats).toEqual({ s: 1, r: 1, sp: 1, f: 0 });
    expect(f.edges).toHaveLength(2);
  });

  it('falls back to the full graph for docs no node points at', () => {
    expect(focusGraph(g, 'changes/nope.md')).toBe(g);
  });
});

describe('driverBacklinks', () => {
  it('maps driver refs back to the citing requirements, deduped per doc', () => {
    const files = {
      'requirements/R1.md': '---\nid: R1\ntitle: One\nstatus: draft\ndrivers:\n  - type: regulatory\n    ref: regulations/a.md#x\n  - type: regulatory\n    ref: regulations/a.md#y\n  - type: product\n    ref: Ops prose driver\n---\n',
    };
    const b = driverBacklinks(buildModel(files));
    expect(b['regulations/a.md']).toEqual([{ from: 'requirements/R1.md', type: 'regulatory', id: 'R1', title: 'One' }]);
    expect(Object.keys(b)).toEqual(['regulations/a.md']); // prose refs backlink nowhere
  });
});
