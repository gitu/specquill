import { describe, expect, it } from 'vitest';
import { scaffoldConfigYml, scaffoldFor } from './scaffold';
import { idPattern } from './ids';
import { BUILTIN_ENTITIES } from './entities';
import { DEFAULT_DRIVERS, DEFAULT_LINK_TYPES, DEFAULT_PROPERTIES, DEFAULT_STATUSES, DEFAULT_TIMED, DEFAULT_TRACEABILITY, workspaceConfig } from './config';
import { defaultDoc } from './derive';

describe('scaffold', () => {
  it('config.yml scaffold round-trips through the client parsers', () => {
    const yml = scaffoldConfigYml('sample-payments');
    expect(yml).toContain('project: sample-payments');
    expect(idPattern('requirement', yml)).toBe('REQ-{seq:3}');
  });

  // the sample IS the default setup spelled out — importing it as-is must
  // change nothing. This is the drift guard between scaffold.ts and config.ts.
  it('sample config spells out exactly the built-in defaults', () => {
    const cfg = workspaceConfig(scaffoldConfigYml('p'));
    expect(cfg.entities).toEqual(BUILTIN_ENTITIES);
    expect(cfg.drivers).toEqual(DEFAULT_DRIVERS);
    expect(cfg.statuses).toEqual(DEFAULT_STATUSES);
    expect(cfg.linkTypes).toEqual(DEFAULT_LINK_TYPES);
    expect(cfg.traceability).toEqual(DEFAULT_TRACEABILITY);
    expect(cfg.properties).toEqual(DEFAULT_PROPERTIES);
    expect(cfg.timed).toEqual(DEFAULT_TIMED);
  });

  it('scaffoldFor knows config.yml and instructions.md; schema.json is combined away', () => {
    expect(scaffoldFor('.specquill/config.yml', 'p')).toContain('project: p');
    expect(scaffoldFor('.specquill/instructions.md', 'p')).toContain('Workspace instructions');
    expect(scaffoldFor('.specquill/schema.json', 'p')).toBeNull();
    expect(scaffoldFor('requirements/REQ-001.md', 'p')).toBeNull();
  });
});

describe('defaultDoc', () => {
  const E = BUILTIN_ENTITIES;
  it('prefers the first document in entity-family order', () => {
    expect(defaultDoc({
      'specs/capture-flow.md': '', 'requirements/REQ-002.md': '', 'requirements/REQ-001.md': '', 'index.md': '',
    }, E)).toBe('requirements/REQ-001.md'); // requirements outrank specs; sorted within
  });
  it('skips reserved index/log files inside families', () => {
    expect(defaultDoc({ 'requirements/index.md': '', 'specs/kyc-gate.md': '' }, E)).toBe('specs/kyc-gate.md');
  });
  it('falls back to the workspace index, then any markdown file', () => {
    expect(defaultDoc({ 'index.md': '', 'notes.md': '' }, E)).toBe('index.md');
    expect(defaultDoc({ 'zzz.md': '', 'aaa.md': '' }, E)).toBe('aaa.md');
  });
  it('is empty while the snapshot has not loaded', () => {
    expect(defaultDoc(undefined, E)).toBe('');
    expect(defaultDoc({}, E)).toBe('');
  });
});
