import { describe, expect, it } from 'vitest';
import { newDocTemplate } from './newdoc';
import { todayStr } from './frontmatter';
import type { EntityDef } from './entities';

const ent = (kind: string, folder: string): EntityDef => ({
  kind, folder, label: kind, icon: '▢', color: 'x', description: '', builtin: false,
});

const dates = `created: ${todayStr()}\nupdated: ${todayStr()}\n`;

describe('newDocTemplate', () => {
  it('types the document after its folder family', () => {
    expect(newDocTemplate('requirements/REQ-001.md')).toBe(
      `---\ntype: Requirement\ntitle: REQ-001\nstatus: draft\n${dates}---\n\n# REQ-001\n`,
    );
  });

  it('derives the family from the top-level folder, however deep the file sits', () => {
    expect(newDocTemplate('requirements/auth/session/REQ-002.md')).toContain('type: Requirement');
    expect(newDocTemplate('specs/sub/thing.md')).toContain('type: Specification');
  });

  it('custom entity families title-case their kind', () => {
    const entities = [ent('test_case', 'testcases/'), ent('decision', 'decisions/')];
    expect(newDocTemplate('testcases/TC-1.md', entities)).toContain('type: Test Case');
    expect(newDocTemplate('decisions/ADR-001.md', entities)).toContain('type: Decision');
  });

  it('unknown folders and root files fall back to Document', () => {
    expect(newDocTemplate('notes/whatever.md')).toContain('type: Document');
    expect(newDocTemplate('README.md')).toContain('type: Document');
  });

  it('carries an explicit id and title into frontmatter and heading', () => {
    expect(newDocTemplate('requirements/REQ-091.md', undefined, { id: 'REQ-091', title: 'Venue routing' })).toBe(
      `---\nid: REQ-091\ntype: Requirement\ntitle: Venue routing\nstatus: draft\n${dates}---\n\n# Venue routing\n`,
    );
  });

  it('omits the id line when no id is given', () => {
    expect(newDocTemplate('specs/venue.md', undefined, { title: 'Venue' })).not.toContain('id:');
  });

  it('seeds link-typed attributes as empty lists — workspace-defined fields included', () => {
    const e: EntityDef = { ...ent('requirement', 'requirements/'), attributes: ['id', 'title', 'status', 'depends_on', 'implements', 'owner'] };
    const out = newDocTemplate('requirements/REQ-1.md', [e], { schema: { fields: { depends_on: { type: 'links' } } } });
    expect(out).toContain('depends_on: []'); // link field only the workspace schema knows
    expect(out).toContain('implements: []'); // built-in link field seeds even with a trimmed schema
    expect(out).not.toContain('owner:');     // scalar attributes are not pre-seeded
  });
});
