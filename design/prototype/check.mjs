// syntax/model smoke test for repo-render.js + app.js (parse only)
import fs from 'fs';
import vm from 'vm';
import * as R from './repo-render.js';

// parse app.js as an ES module (no execution — DOM not available here)
const src = fs.readFileSync(new URL('./app.js', import.meta.url), 'utf8');
new vm.SourceTextModule(src, { identifier: 'app.js' });
console.log('app.js parses OK');

// build the model from the real repo files and sanity-check it
const read = (p) => fs.readFileSync(new URL('../../repo/' + p, import.meta.url), 'utf8');
const paths = [
  'regulations/gdpr.md', 'regulations/mifid-ii.md', 'regulations/dora.md',
  'requirements/REQ-042.md', 'requirements/REQ-051.md', 'requirements/REQ-063.md',
  'requirements/REQ-070.md', 'requirements/REQ-090.md', 'requirements/REQ-095.md',
  'specs/txn-report.md', 'specs/venue.md',
  'data-mappings/trade.md', 'data-mappings/customer.md',
  'changes/2026-06-mifid-rts22.md', 'changes/2026-06-partial-fills.md',
  'changes/2026-06-oms-v4.md', 'changes/2026-05-dora-incident.md',
];
const files = Object.fromEntries(paths.map((p) => [p, read(p)]));
const M = R.buildModel(files);
console.log('model:', {
  regs: M.regs.length, requirements: M.requirements.length, specs: M.specs.length,
  maps: M.maps.length, changes: M.changes.length, fields: M.fields.length,
  drifts: M.fields.filter((f) => f.drift).length,
});
if (M.requirements.length !== 6 || M.changes.length !== 4 || M.fields.length < 5) {
  throw new Error('unexpected model shape');
}
const svg = R.excalidrawToSvg(JSON.parse(read('diagrams/data-flow.excalidraw')), {});
if (!svg.startsWith('<svg')) throw new Error('excalidraw render failed');
console.log('excalidraw SVG OK (' + svg.length + ' chars)');
const props = R.parseProps(R.stripFrontmatter(files['requirements/REQ-042.md']).fm);
console.log('REQ-042 props keys:', props.map((p) => p.key).join(', '));
console.log('ALL CHECKS PASSED');
