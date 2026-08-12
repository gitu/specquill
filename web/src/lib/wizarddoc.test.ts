import { describe, expect, it } from 'vitest';
import { draftBody, draftDocument } from './wizarddoc';
import { sectionsFor, sectionSchemes } from './sections';

describe('draftBody', () => {
  it('renders the title and one heading per section', () => {
    const md = draftBody('Seven-year retention', [
      { name: 'Overview', content: 'Records live seven years.' },
      { name: 'Edge cases', content: '' },
    ]);
    expect(md).toContain('# Seven-year retention\n');
    expect(md).toContain('\n## Overview\n\nRecords live seven years.\n');
    // an empty section keeps its heading: the author lands in the editor with
    // a visible place to fill, instead of a silently dropped block
    expect(md).toContain('## Edge cases');
  });

  it('skips nameless sections', () => {
    expect(draftBody('T', [{ name: '  ', content: 'orphan' }])).not.toContain('orphan');
  });
});

describe('draftDocument', () => {
  const sections = [{ name: 'Overview', content: 'Body.' }];

  it('carries the family frontmatter plus the drafted body', () => {
    const doc = draftDocument('specs/seven-year.md', undefined, { id: 'SPEC-1', title: 'Seven-year retention' }, sections);
    expect(doc.startsWith('---\n')).toBe(true);
    expect(doc).toContain('id: SPEC-1');
    expect(doc).toContain('type: Specification');
    expect(doc).toContain('title: Seven-year retention');
    expect(doc).toContain('status: draft');
    expect(doc).toContain('\n## Overview\n\nBody.\n');
    // exactly one frontmatter block — the template's own H1 must not survive
    // alongside the drafted one
    expect(doc.match(/^---$/gm)).toHaveLength(2);
    expect(doc.match(/^# /gm)).toHaveLength(1);
  });

  it('records carried-over documents as a related list', () => {
    const doc = draftDocument('specs/x.md', undefined, { id: 'S1', title: 'X' }, sections, ['specs/retention.md']);
    expect(doc).toContain('related:');
    expect(doc).toContain('specs/retention.md');
  });

  it('omits the related key when nothing was carried', () => {
    expect(draftDocument('specs/x.md', undefined, { id: 'S1', title: 'X' }, sections)).not.toContain('related:');
  });
});

describe('sections', () => {
  it('gives each built-in family its own outline', () => {
    expect(sectionsFor('spec')[0]).toBe('Overview');
    expect(sectionsFor('requirement')).toContain('Acceptance criteria');
  });

  it('falls back to a generic outline for custom families', () => {
    expect(sectionsFor('bikeshed')).toEqual(['Context', 'Details', 'Acceptance criteria', 'Open questions']);
  });

  it('never hands out its own array (callers reorder)', () => {
    sectionsFor('spec')[0] = 'mutated';
    expect(sectionsFor('spec')[0]).toBe('Overview');
  });

  it('reads per-family overrides from the workspace config', () => {
    const yml = 'entities:\n  spec: { folder: "specs/" }\nsections:\n  spec: ["Problem", "Solution"]\n  requirement: [\'A\', \'B\']\nids:\n  spec: { pattern: "S-{seq:3}" }\n';
    expect(sectionSchemes(yml)).toEqual({ spec: ['Problem', 'Solution'], requirement: ['A', 'B'] });
    expect(sectionsFor('spec', yml)).toEqual(['Problem', 'Solution']);
    // a family the block does not mention keeps its built-in outline
    expect(sectionsFor('change', yml)[0]).toBe('Summary');
  });
});
