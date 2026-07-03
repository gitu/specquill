import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { buildModel } from './model';
import { knownTargets, linkifyReferences, suggestReferences } from './refs';

const REPO = join(process.cwd(), '../repo');
const files: Record<string, string> = {};
for (const folder of ['regulations', 'requirements', 'specs', 'data-mappings', 'changes']) {
  for (const name of readdirSync(join(REPO, folder))) {
    files[`${folder}/${name}`] = readFileSync(join(REPO, folder, name), 'utf8');
  }
}
const model = buildModel(files);
const targets = knownTargets(model);

describe('linkifyReferences', () => {
  it('links a plain requirement id with a relative path', () => {
    const md = 'This depends on REQ-042 heavily.\n';
    const out = linkifyReferences(md, targets, 'specs/txn-report.md');
    expect(out).toContain('[REQ-042](../requirements/REQ-042.md)');
  });

  it('never touches code, fences, or existing links', () => {
    const md = 'See [REQ-042](../requirements/REQ-042.md) and `REQ-042` and\n```\nREQ-042\n```\n';
    const out = linkifyReferences(md, targets, 'specs/venue.md');
    expect(out).toBe(md);
  });

  it('links a title mention once only', () => {
    const md = 'Exception Handling matters. Exception Handling again.\n';
    const out = linkifyReferences(md, targets, 'specs/txn-report.md');
    expect(out.match(/\[Exception Handling\]/g)).toHaveLength(1);
  });

  it('skips self references', () => {
    const md = 'REQ-042 is this doc.\n';
    const out = linkifyReferences(md, targets, 'requirements/REQ-042.md');
    expect(out).toBe(md);
  });

  it('can restrict to a single target', () => {
    const md = 'REQ-042 and REQ-051.\n';
    const out = linkifyReferences(md, targets, 'specs/venue.md', 'requirements/REQ-051.md');
    expect(out).toContain('[REQ-051]');
    expect(out).not.toContain('[REQ-042]');
  });
});

describe('suggestReferences', () => {
  it('proposes mentioned-but-unlinked entities', () => {
    const md = 'The trade.venue field and REQ-070 need review.\n';
    const suggestions = suggestReferences(md, targets, 'specs/txn-report.md');
    const paths = suggestions.map((s) => s.path);
    expect(paths).toContain('requirements/REQ-070.md');
    expect(paths).toContain('data-mappings/trade.md'); // via trade.venue field id
  });

  it('does not propose already-linked targets', () => {
    const md = 'See [REQ-070](../requirements/REQ-070.md), REQ-070 rules.\n';
    const suggestions = suggestReferences(md, targets, 'specs/venue.md');
    expect(suggestions.map((s) => s.path)).not.toContain('requirements/REQ-070.md');
  });
});
