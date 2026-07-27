import { describe, expect, it } from 'vitest';
import { forgeRef, forgeTerms } from './forge';

// The UI must speak the host's language: GitLab merge requests are !12,
// GitHub pull requests are #12. An unknown kind stays neutral rather than
// guessing GitLab (which is what the first cut did).
describe('forge vocabulary', () => {
  it('names and numbers GitLab merge requests', () => {
    expect(forgeTerms('gitlab')).toEqual({ noun: 'merge request', Noun: 'Merge request', prefix: '!' });
    expect(forgeRef('gitlab', 12)).toBe('!12');
  });

  it('names and numbers GitHub pull requests', () => {
    expect(forgeTerms('github')).toEqual({ noun: 'pull request', Noun: 'Pull request', prefix: '#' });
    expect(forgeRef('github', 12)).toBe('#12');
  });

  it('falls back to neutral wording when the kind is unknown', () => {
    const terms = forgeTerms(undefined);
    expect(terms.noun).toBe('request');
    expect(terms.Noun).toBe('Request');
    expect(forgeRef(undefined, 12)).toBe('#12');
  });
});
