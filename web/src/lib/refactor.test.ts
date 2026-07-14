import { describe, expect, it } from 'vitest';
import { referencingDocs, relLink, rewriteRefs } from './refactor';

describe('relLink', () => {
  it('produces sibling, child and parent-relative paths', () => {
    expect(relLink('specs', 'specs/y.md')).toBe('y.md');
    expect(relLink('', 'specs/x.md')).toBe('specs/x.md');
    expect(relLink('requirements', 'specs/x.md')).toBe('../specs/x.md');
    expect(relLink('a/b', 'a/c/x.md')).toBe('../c/x.md');
  });
});

describe('rewriteRefs', () => {
  const OLD = 'specs/venue.md';
  const NEW = 'specs/venues/venue-ids.md';

  it('rewrites every body-link style to a relative link, keeping anchors', () => {
    const doc = [
      '# Doc',
      '[abs](/specs/venue.md)',
      '[rel](../specs/venue.md#rules)',
      '[other](other.md)',
      '[ext](https://example.com/specs/venue.md)',
    ].join('\n');
    const out = rewriteRefs('requirements/REQ-001.md', doc, OLD, NEW)!;
    expect(out).toContain('[abs](../specs/venues/venue-ids.md)');
    expect(out).toContain('[rel](../specs/venues/venue-ids.md#rules)');
    expect(out).toContain('[other](other.md)');
    expect(out).toContain('https://example.com/specs/venue.md'); // external untouched
  });

  it('rewrites bare relative links from a root document', () => {
    const out = rewriteRefs('notes.md', '[v](specs/venue.md)', OLD, NEW)!;
    expect(out).toContain('[v](specs/venues/venue-ids.md)');
  });

  it('rewrites typed frontmatter links (inline and block lists)', () => {
    const doc = '---\nimplements: [specs/venue.md, specs/other.md]\nsatisfies:\n  - specs/venue.md\n---\n\nbody\n';
    const out = rewriteRefs('requirements/REQ-001.md', doc, OLD, NEW)!;
    expect(out).toContain('implements: [specs/venues/venue-ids.md, specs/other.md]');
    expect(out).toContain('- specs/venues/venue-ids.md');
  });

  it('does not touch longer paths sharing the prefix, returns null when unreferenced', () => {
    const doc = '---\nimplements: [specs/venue-extra.md]\n---\n\n[x](specs/venue-extra.md)\n';
    expect(rewriteRefs('requirements/REQ-001.md', doc, OLD, NEW)).toBeNull();
  });

  it('resolves sibling links from the linking document dir', () => {
    const doc = '[sibling](venue.md)';
    const out = rewriteRefs('specs/txn.md', doc, OLD, NEW)!;
    expect(out).toContain('[sibling](venues/venue-ids.md)');
  });
});

describe('referencingDocs', () => {
  it('finds referencing markdown documents only', () => {
    const files = {
      'specs/venue.md': '# self',
      'requirements/REQ-001.md': '---\nimplements: [specs/venue.md]\n---\n\nx\n',
      'specs/txn.md': '[v](venue.md)',
      'specs/unrelated.md': 'nothing here',
      'index.md': '- [Venue](specs/venue.md)',
    };
    expect(referencingDocs(files, 'specs/venue.md')).toEqual(['index.md', 'requirements/REQ-001.md', 'specs/txn.md']);
  });
});
