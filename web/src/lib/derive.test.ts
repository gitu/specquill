import { describe, expect, it } from 'vitest';
import { buildProps, buildTree, collectFieldValues, collectRefTargets, defaultDoc, docAnchorOptions } from './derive';
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
