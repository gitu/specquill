import { describe, expect, it } from 'vitest';
import { backlinkLabel, buildDashboard, buildTimed, buildGraph, buildProps, buildRefTree, buildTree, collectFieldValues, collectRefTargets, defaultDoc, docAnchorOptions, docLinkReport, driverMeta, collectBacklinks, filterRefPaths, focusGraph } from './derive';
import { buildModel } from './model';
import { BUILTIN_ENTITIES } from './entities';
import { workspaceConfig } from './config';

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

  it('nests subdirectories to any depth, files at their own level', () => {
    const nested = {
      'specs/a.md': '---\ntype: Specification\n---\n',
      'specs/venues/v1.md': '---\ntype: Specification\n---\n',
      'specs/venues/deep/v2.md': '---\ntype: Specification\n---\n',
    };
    const [specs] = buildTree(nested, undefined, {}, BUILTIN_ENTITIES);
    expect(specs.path).toBe('specs');
    expect(specs.files.map((f) => f.name)).toEqual(['a.md']);
    expect(specs.dirs.map((d) => d.path)).toEqual(['specs/venues']);
    expect(specs.dirs[0].files.map((f) => f.path)).toEqual(['specs/venues/v1.md']);
    expect(specs.dirs[0].dirs[0].path).toBe('specs/venues/deep');
    expect(specs.dirs[0].dirs[0].files[0].name).toBe('v2.md');
  });

  it('all-files mode reveals root files, dot-folders and binaries from the full listing', () => {
    const snap = { 'specs/a.md': '---\ntype: Specification\n---\n' };
    const extra = ['README.md', '.specquill/config.yml', 'diagrams/flow.excalidraw.png', 'specs/a.md'];
    const docsOnly = buildTree(snap, undefined, {}, BUILTIN_ENTITIES);
    expect(docsOnly.map((d) => d.name)).toEqual(['specs']);
    const all = buildTree(snap, undefined, {}, BUILTIN_ENTITIES, undefined, { all: true, extraPaths: extra });
    const names = all.map((d) => d.name);
    expect(names).toContain('.specquill');
    expect(names).toContain('diagrams');
    expect(names).toContain('/');
    expect(all.find((d) => d.name === '/')!.files.map((f) => f.path)).toEqual(['README.md']);
    expect(all.find((d) => d.name === 'diagrams')!.files[0].path).toBe('diagrams/flow.excalidraw.png');
  });

  it('carries classified-family meta: icon/color/type/title per file, not per folder', () => {
    const parked = { 'notes/req.md': '---\ntype: Requirement\nid: R1\ntitle: Parked req\n---\n' };
    const model = buildModel(parked);
    const [notes] = buildTree(parked, undefined, {}, BUILTIN_ENTITIES, model);
    const f = notes.files[0];
    expect(f.docType).toBe('Requirement');
    expect(f.title).toBe('Parked req');
    expect(f.icon).toBe('▤'); // the requirement family's icon, though it sits in notes/
    expect(f.color).toBe('var(--prod)');
  });
});

describe('filterRefPaths', () => {
  const files = ['docs/api/auth.md', 'docs/guide.md', 'src/main.go', 'README.md'];

  it('keeps everything without prefixes', () => {
    expect(filterRefPaths(files)).toEqual(files);
    expect(filterRefPaths(files, [])).toEqual(files);
  });

  it('matches whole path segments like the server filter, trailing slash or not', () => {
    expect(filterRefPaths(files, ['docs'])).toEqual(['docs/api/auth.md', 'docs/guide.md']);
    expect(filterRefPaths(files, ['docs/'])).toEqual(['docs/api/auth.md', 'docs/guide.md']);
    expect(filterRefPaths(files, ['doc'])).toEqual([]); // no partial-segment match
    expect(filterRefPaths(files, ['README.md'])).toEqual(['README.md']); // exact file
  });
});

describe('buildRefTree', () => {
  it('nests a flat listing into sorted directories', () => {
    const root = buildRefTree(['b/two.md', 'a/deep/one.md', 'top.md', 'a/zero.md']);
    expect(root.files.map((f) => f.name)).toEqual(['top.md']);
    expect(root.dirs.map((d) => d.name)).toEqual(['a', 'b']);
    const a = root.dirs[0];
    expect(a.path).toBe('a');
    expect(a.files.map((f) => f.path)).toEqual(['a/zero.md']);
    expect(a.dirs[0].path).toBe('a/deep');
    expect(a.dirs[0].files[0]).toEqual({ name: 'one.md', path: 'a/deep/one.md' });
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
    'requirements/R1.md': '---\nid: R1\ntitle: One\nstatus: draft\ndrivers:\n  - regulations/a.md#x\n---\n',
    'specs/s1.md': '---\nid: S1\ntitle: Spec One\nimplements:\n  - requirements/R1.md\n---\n',
    'requirements/R2.md': '---\nid: R2\ntitle: Two\nstatus: draft\ndrivers:\n  - products/p.md\n---\n',
  };
  const g = buildGraph(buildModel(files));

  it('keeps only the chain connected to the doc, up and down', () => {
    const f = focusGraph(g, 'requirements/R1.md');
    const ids = f.nodes.map((n) => n.id);
    expect(ids).toContain('doc:requirements/R1.md');
    expect(ids).toContain('doc:specs/s1.md');
    expect(ids).toContain('doc:regulations/a.md');
    expect(ids).not.toContain('doc:requirements/R2.md');
    const counts = Object.fromEntries(f.stats.map((s) => [s.key, s.count]));
    expect(counts).toEqual({ why: 1, what: 1, how: 1 });
    expect(f.edges).toHaveLength(2);
  });

  it('columns follow the group axis in order', () => {
    expect(g.cols).toEqual(['why', 'what', 'how']);
    const col = (id: string) => g.nodes.find((n) => n.id === id)!.col;
    expect(col('doc:regulations/a.md')).toBe(0);
    expect(col('doc:requirements/R1.md')).toBe(1);
    expect(col('doc:specs/s1.md')).toBe(2);
  });

  it('derives driver edge color from the referenced doc, upward links drawn left to right', () => {
    // the drivers edge starts at the WHY doc (left) and ends at the WHAT doc
    const e = g.edges.find((x) => x.a === 'doc:regulations/a.md' && x.b === 'doc:requirements/R1.md');
    expect(e).toBeTruthy();
    expect(e!.stroke).toBe('var(--reg)'); // regulation family → regulatory
    // the implements edge held by the SPEC also draws what → how
    expect(g.edges.some((x) => x.a === 'doc:requirements/R1.md' && x.b === 'doc:specs/s1.md')).toBe(true);
  });

  it('falls back to the full graph for docs no node points at', () => {
    expect(focusGraph(g, 'changes/nope.md')).toBe(g);
  });
});

describe('buildDashboard', () => {
  const files = {
    'regulations/a.md': '---\nid: REG-a\ntitle: Reg A\n---\n',
    'requirements/R1.md': '---\nid: R1\ntitle: One\nstatus: draft\ndrivers:\n  - regulations/a.md\n---\n',
    'requirements/R2.md': '---\nid: R2\ntitle: Two\nstatus: draft\n---\n',
    'specs/s1.md': '---\nid: S1\ntitle: Spec One\nimplements:\n  - requirements/R1.md\n---\n',
    'work-items/W1.md': '---\nid: WI-1\ntitle: Ship it\nstatus: backlog\ndelivers:\n  - specs/s1.md\n---\n',
    'regulations/dora.md': '---\nid: REG-dora\ntitle: DORA\nstatus: approved\nstarts: 2026-09-01\n---\n',
  };
  const d = buildDashboard(buildModel(files), '2026-08-01');

  it('computes chain health with inbound coverage for the upper levels', () => {
    const bars = Object.fromEntries(d.health.map((h) => [h.label, h.pct]));
    expect(bars['Requirements → drivers']).toBe(50);   // R1 of R1+R2
    expect(bars['Requirements ← specs']).toBe(50);     // R1 covered by s1's implements
    expect(bars['Specs ← work items']).toBe(100);      // s1 covered by W1's delivers
  });

  it('derives KPI tiles from the config', () => {
    const timed = d.tiles.find((t) => t.key === 'timed')!;
    expect(timed.label).toBe('Pending dependencies');
    expect(timed.value).toBe('1');                    // DORA starts 2026-09-01
    expect(timed.sub).toBe('0 active · 0 expiring · 0 expired');
    const what = d.tiles.find((t) => t.key === 'what')!;
    expect(what.value).toBe('2');
    expect(what.sub).toBe('1 of 2 implemented');
    expect(d.newDoc).toEqual({ kind: 'requirement', label: 'requirement' });
  });

  it('drops tiles, bars and the timeline for entities the workspace hides', () => {
    const yml = [
      'entities:',
      '  requirement: { label: "User stories" }',
      '  regulation: { hidden: true }',
      '  data_mapping: { hidden: true }',
      '  work_item: { hidden: true }',
    ].join('\n');
    const cfg = workspaceConfig(yml);
    const d2 = buildDashboard(buildModel(files, cfg), '2026-08-01');
    expect(d2.hasTimed).toBe(false);                  // the only timed doc was a regulation
    expect(d2.upcoming).toEqual([]);
    expect(d2.tiles.map((t) => t.key)).toEqual(['what']);
    expect(d2.tiles[0].label).toBe('User stories');
    expect(d2.newDoc).toEqual({ kind: 'requirement', label: 'user story' });
    // no work_item entity → no WHEN bar; no data_mapping → no data-fields bar
    expect(d2.health.map((h) => h.label)).toEqual(['User stories → drivers', 'User stories ← specs']);
  });

  it('buildTimed buckets windows, rolls up dependents and flags risk', () => {
    const tf = {
      // starts inside the horizon, its only spec still draft → at risk
      'requirements/R1.md': '---\nid: R1\ntitle: Soon\nstatus: approved\nstarts: 2026-08-20\n---\n',
      'specs/s1.md': '---\ntitle: Impl\nstatus: draft\nimplements:\n  - requirements/R1.md\n---\n',
      // starts inside the horizon with everything ready → pending, not at risk
      'requirements/R2.md': '---\nid: R2\ntitle: Ready\nstatus: approved\nstarts: 2026-08-10\n---\n',
      'specs/s2.md': '---\ntitle: Done\nstatus: approved\nimplements:\n  - requirements/R2.md\n---\n',
      // in force with the end beyond the horizon → active
      'regulations/g1.md': '---\nid: G1\ntitle: Running\nstatus: approved\nstarts: 2026-01-01\nends: 2027-06-01\n---\n',
      // ends inside the horizon → expiring; already ended → expired
      'regulations/g2.md': '---\nid: G2\ntitle: Closing\nstatus: approved\neffective_until: 2026-09-15\n---\n',
      'regulations/g3.md': '---\nid: G3\ntitle: Gone\nstatus: approved\nends: 2026-07-01\n---\n',
      // no window at all → not on the timeline
      'requirements/R9.md': '---\nid: R9\ntitle: Untimed\nstatus: draft\n---\n',
    };
    const m = buildModel(tf);
    expect(m.timed.map((t) => t.path).sort()).toEqual([
      'regulations/g1.md', 'regulations/g2.md', 'regulations/g3.md',
      'requirements/R1.md', 'requirements/R2.md',
    ]);
    const { items, counts } = buildTimed(m, '2026-08-01');
    const byId = Object.fromEntries(items.map((i) => [i.id, i]));
    expect(byId.R1.state).toBe('pending');
    expect(byId.R1.days).toBe(19);
    expect(byId.R1.atRisk).toBe(true);            // s1 still draft
    expect(byId.R1.readyCount).toBe(0);
    expect(byId.R2.atRisk).toBe(false);           // s2 approved
    expect(byId.G1.state).toBe('active');
    expect(byId.G2.state).toBe('expiring');       // effective_until is a configured end key
    expect(byId.G2.days).toBe(45);
    expect(byId.G3.state).toBe('expired');
    expect(byId.G3.days).toBe(-31);
    expect(counts).toEqual({ all: 5, pending: 2, active: 1, expiring: 1, expired: 1 });
    // pending first, soonest first; filters narrow to one bucket
    expect(items.map((i) => i.id)).toEqual(['R2', 'R1', 'G1', 'G2', 'G3']);
    expect(buildTimed(m, '2026-08-01', 'expired').items.map((i) => i.id)).toEqual(['G3']);
  });

  it('timed keys, ready statuses and horizon come from the config', () => {
    const cfg = workspaceConfig([
      'timed:',
      '  start: [go_live]',
      '  end: [sunset]',
      '  ready_statuses: [shipped]',
      '  horizon_days: 10',
      '  kinds: [requirement]',
    ].join('\n'));
    const tf = {
      'requirements/R1.md': '---\nid: R1\ntitle: Custom\nstatus: draft\ngo_live: 2026-08-05\n---\n',
      'specs/s1.md': '---\ntitle: Impl\nstatus: shipped\nimplements:\n  - requirements/R1.md\n---\n',
      // the built-in key is not configured here, and specs are out of `kinds`
      'specs/s2.md': '---\ntitle: Other\nstatus: draft\nstarts: 2026-08-02\n---\n',
    };
    const m = buildModel(tf, cfg);
    expect(m.timed.map((t) => t.path)).toEqual(['requirements/R1.md']);
    const { items } = buildTimed(m, '2026-08-01');
    expect(items[0].startKey).toBe('go_live');
    expect(items[0].deps[0].ready).toBe(true);    // shipped counts as ready here
    expect(items[0].atRisk).toBe(true);           // …but the document itself is draft
    // outside the 10-day horizon nothing is at risk yet
    expect(buildTimed(m, '2026-07-01').items[0].atRisk).toBe(false);
  });

  it('driverMeta takes custom taxonomy entries from the config', () => {
    const cfg = workspaceConfig('drivers:\n  customer: { label: "Customer", icon: "☺", color: "#aa3377" }\n');
    const m = buildModel({}, cfg);
    expect(driverMeta(m, 'customer')).toEqual({ icon: '☺', label: 'Customer', fg: '#aa3377', bg: 'var(--surface-2)' });
    // the built-in trio keeps its themed pair even when not configured
    expect(driverMeta(buildModel({}), 'regulatory').fg).toBe('var(--reg)');
  });

  it('a custom traceability section replaces the bars wholesale', () => {
    const yml = [
      'link_types:',
      '  satisfies: { from: spec, to: requirement, inverse: "satisfied by" }',
      'traceability:',
      '  - { link: satisfies, measure: to, label: "Reqs satisfied", color: "#123456" }',
    ].join('\n');
    const cfg = workspaceConfig(yml);
    const tf = {
      'requirements/R1.md': '---\nid: R1\ntitle: One\n---\n',
      'requirements/R2.md': '---\nid: R2\ntitle: Two\n---\n',
      'specs/s1.md': '---\nid: S1\ntitle: S\nsatisfies:\n  - requirements/R1.md\n---\n',
    };
    const d = buildDashboard(buildModel(tf, cfg));
    expect(d.health).toEqual([{ label: 'Reqs satisfied', pct: 50, color: '#123456' }]);
  });

  it('legacy driver maps still count toward the drivers bar', () => {
    const legacy = {
      'regulations/a.md': '---\nid: REG-a\ntitle: Reg A\n---\n',
      'requirements/R1.md': '---\nid: R1\ntitle: One\nstatus: draft\ndrivers:\n  - type: regulatory\n    ref: regulations/a.md\n---\n',
    };
    const dl = buildDashboard(buildModel(legacy));
    const bar = dl.health.find((h) => h.label === 'Requirements → drivers')!;
    expect(bar.pct).toBe(100);
  });
});

describe('docLinkReport', () => {
  const files = {
    'regulations/a.md': '---\nid: REG-a\ntitle: Reg A\n---\n',
    'requirements/R1.md': [
      '---',
      'id: R1',
      'title: One',
      'status: draft',
      'drivers:',
      '  - regulations/a.md#x',
      '  - "Ops prose driver"',
      'satisfies:',        // NOT a declared link field under the defaults
      '  - specs/s1.md',
      'verifies:',
      '  - tests/missing_spec.py',
      '---',
      '',
      'See [the spec](../specs/s1.md) and [ext](~backend/api.md).',
      '',
    ].join('\n'),
    'specs/s1.md': '---\nid: S1\ntitle: Spec One\nimplements:\n  - requirements/R1.md\n---\n',
  };
  const model = buildModel(files);
  const r = docLinkReport(files, model, 'requirements/R1.md');

  it('classifies the document and resolves every parsed link', () => {
    expect(r.classified).toBe(true);
    expect(r.kind).toBe('requirement');
    const by = (field: string, ref: string) => r.outbound.find((l) => l.field === field && l.ref === ref)!;
    expect(by('drivers', 'regulations/a.md#x')).toMatchObject({ status: 'ok', kind: 'regulation', type: 'regulatory' });
    expect(by('drivers', 'Ops prose driver').status).toBe('prose');
    expect(by('verifies', 'tests/missing_spec.py').status).toBe('missing');
    expect(by('body', 'specs/s1.md').status).toBe('ok');
    expect(by('body', '~backend/api.md').status).toBe('external');
  });

  it('flags path-carrying fields that are not declared link types', () => {
    const sat = r.outbound.find((l) => l.field === 'satisfies')!;
    expect(sat.status).toBe('undeclared');
    // …and once the workspace declares satisfies, it parses as typed
    const cfg = workspaceConfig('link_types:\n  satisfies: { from: spec, to: requirement }\n');
    const r2 = docLinkReport(files, buildModel(files, cfg), 'requirements/R1.md');
    expect(r2.outbound.find((l) => l.field === 'satisfies')!.status).toBe('ok');
  });

  it('carries the inbound side', () => {
    expect(r.inbound.some((b) => b.kind === 'implements' && b.from === 'specs/s1.md')).toBe(true);
  });

  it('resolves doc-relative and leading-slash refs across folders', () => {
    const rel = {
      'regulations/a.md': '---\nid: REG-a\ntitle: Reg A\n---\n',
      'requirements/R1.md': '---\nid: R1\ntitle: One\ndrivers:\n  - ../regulations/a.md#x\n---\n',
      'specs/s1.md': '---\nid: S1\ntitle: Spec One\nimplements:\n  - /requirements/R1.md\n---\n',
      'specs/s2.md': '---\nid: S2\ntitle: Spec Two\nimplements:\n  - s1.md\n---\n', // sibling
    };
    const m = buildModel(rel);
    // the model normalizes every spelling to the canonical root-relative path
    expect(m.docs.find((d) => d.path === 'requirements/R1.md')!.links.drivers).toEqual(['regulations/a.md#x']);
    expect(m.docs.find((d) => d.path === 'specs/s1.md')!.links.implements).toEqual(['requirements/R1.md']);
    expect(m.docs.find((d) => d.path === 'specs/s2.md')!.links.implements).toEqual(['specs/s1.md']);
    // …so backlinks and graph edges link up regardless of spelling
    const b = collectBacklinks(m);
    expect(b['requirements/R1.md']!.some((x) => x.kind === 'implements' && x.from === 'specs/s1.md')).toBe(true);
    expect(b['regulations/a.md']!.some((x) => x.kind === 'driver' && x.type === 'regulatory')).toBe(true);
    const g = buildGraph(m);
    expect(g.edges.some((e) => e.a === 'doc:requirements/R1.md' && e.b === 'doc:specs/s1.md')).toBe(true);
    // the report keeps the written form and shows the resolved target
    const rr = docLinkReport(rel, m, 'requirements/R1.md');
    const dr = rr.outbound.find((l) => l.field === 'drivers')!;
    expect(dr).toMatchObject({ ref: '../regulations/a.md#x', target: 'regulations/a.md', status: 'ok', type: 'regulatory' });
  });

  it('reports unclassified documents instead of pretending', () => {
    const loose = { 'notes/x.md': '---\ntitle: X\nimplements:\n  - specs/s1.md\n---\n', ...files };
    const rl = docLinkReport(loose, buildModel(loose), 'notes/x.md');
    expect(rl.classified).toBe(false);
    expect(rl.outbound.every((l) => l.field === 'body' || l.status === 'undeclared')).toBe(true);
  });
});

describe('backlinkLabel', () => {
  const files = { 'requirements/R1.md': '---\nid: R1\ntitle: One\n---\n' };

  it('reads relations from the target side via the inverse labels', () => {
    const m = buildModel(files);
    expect(backlinkLabel(m, 'driver')).toBe('drives');
    expect(backlinkLabel(m, 'implements')).toBe('implemented by');
    expect(backlinkLabel(m, 'delivers')).toBe('delivered by');
    expect(backlinkLabel(m, 'maps to')).toBe('mapped by');
    expect(backlinkLabel(m, 'in text')).toBe('mentioned in');
  });

  it('honors workspace-configured inverse names, falling back to the kind', () => {
    const cfg = workspaceConfig('link_types:\n  satisfies: { from: spec, to: requirement, inverse: "satisfied by" }\n  custom: { from: a, to: b }\n');
    const m = buildModel(files, cfg);
    expect(backlinkLabel(m, 'satisfies')).toBe('satisfied by');
    expect(backlinkLabel(m, 'custom')).toBe('custom');
  });
});

describe('collectBacklinks', () => {
  it('maps driver refs back to the citing requirements, deduped per doc', () => {
    const files = {
      'requirements/R1.md': '---\nid: R1\ntitle: One\nstatus: draft\ndrivers:\n  - type: regulatory\n    ref: regulations/a.md#x\n  - type: regulatory\n    ref: regulations/a.md#y\n  - type: product\n    ref: Ops prose driver\n---\n',
    };
    const b = collectBacklinks(buildModel(files));
    expect(b['regulations/a.md']).toEqual([{ from: 'requirements/R1.md', kind: 'driver', type: 'regulatory', id: 'R1', title: 'One' }]);
    expect(Object.keys(b)).toEqual(['regulations/a.md']); // prose refs backlink nowhere
  });

  it('includes typed relations and body-text mentions, typed suppressing the mention', () => {
    const files = {
      'requirements/R1.md': '---\nid: R1\ntitle: One\nstatus: draft\nimplements:\n  - specs/s1.md\n---\nSee [the spec](../specs/s1.md) and [the reg](../regulations/a.md).\n',
      'specs/s1.md': '---\nid: S1\ntitle: Spec One\n---\n',
      'regulations/a.md': '---\nid: REG-a\ntitle: Reg A\n---\n',
    };
    const b = collectBacklinks(buildModel(files));
    // typed implements wins over the in-text mention of the same pair
    expect(b['specs/s1.md']).toEqual([{ from: 'requirements/R1.md', kind: 'implements', type: undefined, id: 'R1', title: 'One' }]);
    // a pure text mention still backlinks
    expect(b['regulations/a.md']).toEqual([{ from: 'requirements/R1.md', kind: 'in text', type: undefined, id: 'R1', title: 'One' }]);
  });
});
