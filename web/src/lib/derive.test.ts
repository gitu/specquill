import { describe, expect, it } from 'vitest';
import { buildTree, defaultDoc } from './derive';
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
