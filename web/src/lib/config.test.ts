import { describe, expect, it } from 'vitest';
import {
  BUILTIN_ENTITIES, DEFAULT_DRIVERS, DEFAULT_LINK_TYPES, DEFAULT_PROPERTIES,
  DEFAULT_STATUSES, idSchemes, rawTaxonomy, workspaceConfig,
} from './config';

describe('workspaceConfig', () => {
  it('runs entirely on the built-in WHY/WHAT/HOW/WHEN model without a config', () => {
    for (const yml of [undefined, '', 'version: 2\nproject: p\n']) {
      const cfg = workspaceConfig(yml);
      expect(cfg.entities).toEqual(BUILTIN_ENTITIES);
      expect(cfg.drivers).toEqual(DEFAULT_DRIVERS);
      expect(cfg.statuses).toEqual(DEFAULT_STATUSES);
      expect(cfg.linkTypes).toEqual(DEFAULT_LINK_TYPES);
      expect(cfg.properties).toEqual(DEFAULT_PROPERTIES);
      expect(cfg.hasProperties).toBe(false);
    }
    expect(BUILTIN_ENTITIES.map((e) => e.group)).toEqual(['why', 'what', 'how', 'how', 'how', 'why', 'when']);
  });

  it('a present section replaces its default wholesale', () => {
    const cfg = workspaceConfig('statuses: [open, closed]\ndrivers:\n  cost: { label: "Cost", icon: "€" }\n');
    expect(cfg.statuses).toEqual(['open', 'closed']);
    expect(cfg.drivers).toEqual([{ key: 'cost', label: 'Cost', icon: '€', color: 'var(--text-2)' }]);
    expect(cfg.linkTypes).toEqual(DEFAULT_LINK_TYPES); // untouched section keeps its default
  });

  it('entities merge: overrides change only provided fields, customs get defaults, hidden removes', () => {
    const cfg = workspaceConfig([
      'entities:',
      '  requirement: { label: "User needs" }',
      '  risk: { group: why }',
      '  diagram: { hidden: true }',
    ].join('\n'));
    const req = cfg.entities.find((e) => e.kind === 'requirement')!;
    expect(req.label).toBe('User needs');
    expect(req.folder).toBe('requirements/');
    expect(req.group).toBe('what');
    expect(req.builtin).toBe(true);
    const risk = cfg.entities.find((e) => e.kind === 'risk')!;
    expect(risk).toMatchObject({ folder: 'risks/', label: 'risk', group: 'why', builtin: false });
    expect(cfg.entities.find((e) => e.kind === 'diagram')).toBeUndefined();
  });

  it('link_types normalize scalar and list endpoints', () => {
    const cfg = workspaceConfig('link_types:\n  blocks: { from: [work_item, change], to: work_item }\n');
    expect(cfg.linkTypes).toEqual([{ name: 'blocks', from: 'work_item, change', to: 'work_item' }]);
  });

  it('a declared properties section replaces the default schema', () => {
    const cfg = workspaceConfig('properties:\n  order: [id, risk_level]\n  fields:\n    risk_level: { label: "Risk", type: enum, values: { high: amber } }\n');
    expect(cfg.hasProperties).toBe(true);
    expect(cfg.properties.order).toEqual(['id', 'risk_level']);
    expect(cfg.properties.fields!.risk_level).toEqual({ label: 'Risk', type: 'enum', values: { high: 'amber' } });
    expect(cfg.properties.fields!.status).toBeUndefined();
  });

  it('broken yaml falls back to the defaults, never to an empty model', () => {
    const cfg = workspaceConfig('statuses: [unclosed\n  entities: {{');
    expect(cfg.entities).toEqual(BUILTIN_ENTITIES);
    expect(cfg.statuses).toEqual(DEFAULT_STATUSES);
  });
});

describe('rawTaxonomy', () => {
  it('reports only what the file declares — no defaults merged in', () => {
    const raw = rawTaxonomy('statuses: [draft]\n');
    expect(raw.statuses).toEqual(['draft']);
    expect(raw.drivers).toEqual([]);
    expect(raw.links).toEqual([]);
    expect(raw.properties).toBeUndefined();
  });
});

describe('idSchemes', () => {
  it('reads config-declared schemes only', () => {
    expect(idSchemes('ids:\n  requirement: { pattern: "R-{seq:2}" }\n')).toEqual([{ kind: 'requirement', pattern: 'R-{seq:2}' }]);
    expect(idSchemes('')).toEqual([]);
  });
});
