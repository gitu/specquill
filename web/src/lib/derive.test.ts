import { describe, expect, it } from 'vitest';
import { buildProps, buildTree, collectFieldValues, defaultDoc } from './derive';
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
