// Frontmatter template for files created in the app. Every document carries
// an OKF `type` (the only frontmatter field the format requires), derived
// from the folder family it lands in.

export const DOC_TYPES: Record<string, string> = {
  requirements: 'Requirement',
  specs: 'Specification',
  regulations: 'Regulation',
  'data-mappings': 'Data Mapping',
  changes: 'Change Record',
  decisions: 'Decision',
  glossary: 'Glossary',
};

export function newDocTemplate(path: string): string {
  const family = path.includes('/') ? path.split('/')[0] : '';
  const type = DOC_TYPES[family] || 'Document';
  const name = path.split('/').pop()!.replace(/\.md$/, '');
  return `---\ntype: ${type}\ntitle: ${name}\nstatus: draft\n---\n\n# ${name}\n`;
}
