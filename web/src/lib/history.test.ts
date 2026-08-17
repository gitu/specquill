import { describe, expect, it } from 'vitest';
import { buildHistory, pluralLabel, sinceDays, singularLabel } from './history';
import { buildModel } from './model';
import { workspaceConfig } from './config';
import type { Commit } from '../api/hooks';

const files = {
  'requirements/REQ-042.md': '---\nid: REQ-042\ntitle: Transaction reporting\nstatus: approved\n---\n',
  'specs/txn.md': '---\ntitle: Txn spec\nstatus: draft\n---\n',
  // typed into a family it does not live in — classification must follow the type
  'notes/parked.md': '---\ntype: Requirement\nid: REQ-099\ntitle: Parked\n---\n',
};
const model = buildModel(files);

const commit = (over: Partial<Commit>): Commit => ({
  sha: 'a'.repeat(40), parent: 'b'.repeat(40), author: 'Jane', email: 'j@t',
  date: '2026-07-30T10:00:00+02:00', subject: 'edit', files: [], ...over,
});

describe('buildHistory', () => {
  const commits: Commit[] = [
    commit({
      sha: 'c1', subject: 'tighten expiry', date: '2026-07-30T10:00:00+02:00',
      files: [{ status: 'M', path: 'requirements/REQ-042.md' }, { status: 'A', path: 'specs/txn.md' }],
    }),
    commit({
      sha: 'c2', subject: 'retire the old venue map', author: 'Sam', date: '2026-07-30T09:00:00+02:00',
      // deleted and renamed-away paths are gone from the snapshot — only the
      // folder rule can classify them
      files: [
        { status: 'D', path: 'data-mappings/venue.md' },
        { status: 'R', path: 'requirements/REQ-070.md', oldPath: 'requirements/old-venue.md' },
      ],
    }),
    commit({ sha: 'c3', subject: 'chore', date: '2026-07-28T08:00:00+02:00', files: [{ status: 'M', path: 'notes/parked.md' }] }),
  ];

  it('classifies changed paths through the workspace config', () => {
    const { items } = buildHistory(commits, model);
    const c1 = items.find((c) => c.sha === 'c1')!;
    expect(c1.files.map((f) => [f.kind, f.change])).toEqual([['requirement', 'modified'], ['spec', 'added']]);
    expect(c1.files[0].title).toBe('Transaction reporting');
    expect(c1.summary).toBe('1 requirement · 1 spec');
    // deleted/renamed paths fall back to the folder rule and keep both sides
    const c2 = items.find((c) => c.sha === 'c2')!;
    expect(c2.files.map((f) => [f.kind, f.change])).toEqual([['data_mapping', 'deleted'], ['requirement', 'renamed']]);
    expect(c2.files[1].oldPath).toBe('requirements/old-venue.md');
    // a document typed into another family counts as that family, not its folder
    expect(items.find((c) => c.sha === 'c3')!.files[0].kind).toBe('requirement');
  });

  it('groups by day and counts families before filtering', () => {
    const { days, counts } = buildHistory(commits, model);
    expect(days.map((d) => [d.day, d.commits.length])).toEqual([['2026-07-30', 2], ['2026-07-28', 1]]);
    expect(counts.requirement).toBe(3); // c1, c2 (rename), c3
    expect(counts.spec).toBe(1);
    expect(counts.data_mapping).toBe(1);
    expect(counts.all).toBe(3);
  });

  it('filters by family, change type, author and the WHAT axis', () => {
    const shas = (f: Parameters<typeof buildHistory>[2]) => buildHistory(commits, model, f).items.map((c) => c.sha);
    expect(shas({ kind: 'spec' })).toEqual(['c1']);
    expect(shas({ change: 'deleted' })).toEqual(['c2']);
    expect(shas({ author: 'Sam' })).toEqual(['c2']);
    // counts stay honest under a filter — the chips must not lie
    expect(buildHistory(commits, model, { kind: 'spec' }).counts.requirement).toBe(3);
    // WHAT-axis only: the data-mapping-only side of c2 still qualifies via its rename
    expect(shas({ whatOnly: true })).toEqual(['c1', 'c2', 'c3']);
  });

  it('respects a workspace that renames or hides families', () => {
    const cfg = workspaceConfig('entities:\n  requirement: { label: "User stories" }\n  spec: { hidden: true }\n');
    const { items, counts } = buildHistory(commits, buildModel(files, cfg));
    expect(items[0].files[0].label).toBe('user story');
    expect(items[0].summary).toBe('1 user story · 1 file'); // the spec has no family now
    expect(counts.spec).toBeUndefined();
  });
});

describe('label helpers', () => {
  it('singularizes family labels and pluralizes them back', () => {
    expect(singularLabel({ label: 'Requirements' } as never)).toBe('requirement');
    expect(singularLabel({ label: 'Data mappings' } as never)).toBe('data mapping');
    expect(singularLabel({ label: 'Stories' } as never)).toBe('story');
    expect(pluralLabel('story')).toBe('stories');
    expect(pluralLabel('requirement')).toBe('requirements');
    expect(pluralLabel('specs')).toBe('specs');
  });

  it('sinceDays walks back calendar days in local time', () => {
    expect(sinceDays(30, new Date('2026-08-01T12:00:00Z'))).toBe('2026-07-02');
    expect(sinceDays(0, new Date('2026-08-01T12:00:00Z'))).toBe('2026-08-01');
  });
});
