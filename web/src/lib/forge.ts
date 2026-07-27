// Forge vocabulary: GitLab and GitHub name and number the same object
// differently (merge request !12 vs pull request #12). Every user-visible
// string about a proposal goes through here so the UI speaks the host's
// language; an unknown/absent kind falls back to neutral wording.

export type ForgeKind = 'gitlab' | 'github' | undefined;

export interface ForgeTerms {
  /** "merge request" | "pull request" | "request" */
  noun: string;
  /** Noun with an initial capital, for sentence starts. */
  Noun: string;
  /** "!" on GitLab, "#" on GitHub and as the neutral default. */
  prefix: string;
}

export function forgeTerms(kind: ForgeKind): ForgeTerms {
  switch (kind) {
    case 'gitlab':
      return { noun: 'merge request', Noun: 'Merge request', prefix: '!' };
    case 'github':
      return { noun: 'pull request', Noun: 'Pull request', prefix: '#' };
    default:
      return { noun: 'request', Noun: 'Request', prefix: '#' };
  }
}

/** Reference to a request as the host writes it, e.g. "!12" or "#12". */
export function forgeRef(kind: ForgeKind, number: number): string {
  return forgeTerms(kind).prefix + number;
}
