import { describe, expect, it } from 'vitest';
import { scaffoldConfigYml, scaffoldFor, scaffoldSchemaJson } from './scaffold';
import { idPattern } from './ids';
import { parseEntities, BUILTIN_ENTITIES } from './entities';
import { defaultDoc } from './derive';

describe('scaffold', () => {
  it('config.yml scaffold round-trips through the client parsers', () => {
    const yml = scaffoldConfigYml('sample-payments');
    expect(yml).toContain('project: sample-payments');
    expect(idPattern('requirement', yml)).toBe('REQ-{seq:3}');
    // commented-out entities block must not add families
    expect(parseEntities(yml)).toHaveLength(BUILTIN_ENTITIES.length);
  });
  it('schema.json scaffold is valid JSON with fields and order', () => {
    const schema = JSON.parse(scaffoldSchemaJson());
    expect(schema.order).toContain('status');
    expect(schema.fields.status.type).toBe('enum');
  });
  it('scaffoldFor only knows the two workspace files', () => {
    expect(scaffoldFor('.specquill/config.yml', 'p')).toContain('project: p');
    expect(scaffoldFor('.specquill/schema.json', 'p')).toContain('"order"');
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
