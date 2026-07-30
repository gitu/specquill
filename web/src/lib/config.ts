// config.ts — the workspace model in ONE file: .specquill/config.yml defines
// the document families (entities), the WHY-driver taxonomy, statuses, typed
// link relations, ID schemes and the property schema. EVERY section is
// optional — a workspace with no config at all runs on the built-in
// WHY → WHAT → HOW → WHEN model defined here, and a section that is present
// replaces its default. Entities merge instead of replace: built-ins can be
// tweaked field-by-field or removed with `hidden: true` without restating
// the whole set.
//
// Parsed with the real YAML parser (already a dependency via frontmatter.ts)
// — the regex parsers this consolidates could not handle the nested maps the
// property schema brings along.

import { parse } from 'yaml';

// ---------------------------------------------------------------- types

export interface EntityDef {
  kind: string;        // stable key ('requirement', 'decision', …)
  folder: string;      // top-level folder, with trailing slash
  label: string;       // plural display name
  icon: string;        // single glyph used in the tree
  color: string;       // CSS color (var(--…) or hex)
  description: string; // one-sentence "what is this" shown to users
  group?: string;      // model axis: why | what | how | when
  attributes?: string[]; // frontmatter keys new documents are seeded with
  builtin: boolean;
}

export interface DriverDef { key: string; label: string; icon: string; color: string }
export interface LinkTypeDef { name: string; from: string; to: string }

export interface PropertySchema {
  order?: string[];
  fields?: Record<string, { label?: string; type?: string; values?: Record<string, string> }>;
}

export interface WorkspaceConfig {
  entities: EntityDef[];
  drivers: DriverDef[];
  statuses: string[];
  linkTypes: LinkTypeDef[];
  properties: PropertySchema;
  /** the config declared its own properties: section (vs default/schema.json) */
  hasProperties: boolean;
}

// ---------------------------------------------------------------- defaults

// The default model — WHY drives WHAT, HOW realizes it, WHEN delivers it.
export const BUILTIN_ENTITIES: EntityDef[] = [
  {
    kind: 'regulation', group: 'why', folder: 'regulations/', label: 'Regulations', icon: '◈', color: 'var(--reg)', builtin: true,
    attributes: ['id', 'title', 'status'],
    description: 'External rules the product must comply with — the origin of regulatory drivers and change records.',
  },
  {
    kind: 'requirement', group: 'what', folder: 'requirements/', label: 'Requirements', icon: '▤', color: 'var(--prod)', builtin: true,
    attributes: ['id', 'title', 'status', 'priority', 'owner', 'drivers', 'implements', 'verifies'],
    description: 'WHAT the product must do — atomic, testable statements carrying drivers and traceability links.',
  },
  {
    kind: 'spec', group: 'how', folder: 'specs/', label: 'Specs', icon: '◈', color: 'var(--text-2)', builtin: true,
    attributes: ['title', 'status', 'satisfies', 'maps_to'],
    description: 'HOW requirements are realized — designs that satisfy requirements and map onto data fields.',
  },
  {
    kind: 'data_mapping', group: 'how', folder: 'data-mappings/', label: 'Data mappings', icon: '⇄', color: 'var(--data)', builtin: true,
    attributes: ['title'],
    description: 'Field-level source → target mappings; drift against the specs is detected here.',
  },
  {
    kind: 'diagram', group: 'how', folder: 'diagrams/', label: 'Diagrams', icon: '✎', color: 'var(--ai)', builtin: true,
    attributes: [],
    description: 'Sketches and text diagrams embedded in documents — portable formats, no tool lock-in.',
  },
  {
    kind: 'change', group: 'why', folder: 'changes/', label: 'Changes', icon: '⚑', color: 'var(--reg)', builtin: true,
    attributes: ['title', 'status', 'source', 'published'],
    description: 'Incoming change records (regulatory, product, technical) triaged against the documents they impact.',
  },
  {
    kind: 'work_item', group: 'when', folder: 'work-items/', label: 'Work items', icon: '⧗', color: 'var(--data)', builtin: true,
    attributes: ['id', 'title', 'status', 'priority', 'owner', 'delivers', 'due'],
    description: 'WHEN work lands — planned units of delivery that schedule requirements and specs from backlog to done.',
  },
];

export const DEFAULT_DRIVERS: DriverDef[] = [
  { key: 'regulatory', label: 'Regulatory', icon: '⚖', color: '#b06f16' },
  { key: 'product', label: 'Product', icon: '◆', color: '#2563c9' },
  { key: 'technical', label: 'Technical', icon: '⚙', color: '#5a616b' },
];

export const DEFAULT_STATUSES = ['draft', 'in_review', 'approved', 'deprecated'];

export const DEFAULT_LINK_TYPES: LinkTypeDef[] = [
  { name: 'drives', from: 'regulation, change', to: 'requirement' },       // WHY → WHAT
  { name: 'implements', from: 'requirement', to: 'spec' },                 // WHAT → HOW
  { name: 'satisfies', from: 'spec', to: 'requirement' },                  // HOW → WHAT
  { name: 'delivers', from: 'work_item', to: 'requirement, spec' },        // WHEN → WHAT/HOW
  { name: 'maps_to', from: 'spec', to: 'data_field' },
  { name: 'verifies', from: 'test', to: 'requirement' },
];

export const DEFAULT_PROPERTIES: PropertySchema = {
  order: ['id', 'type', 'status', 'priority', 'owner', 'source', 'due', 'drivers', 'implements', 'satisfies', 'delivers', 'maps_to', 'verifies', 'created', 'updated'],
  fields: {
    id: { label: 'ID', type: 'code' },
    type: { label: 'Type', type: 'tag' },
    status: { label: 'Status', type: 'enum', values: { draft: 'slate', in_review: 'amber', approved: 'green', deprecated: 'slate', triage: 'amber', backlog: 'slate', in_progress: 'blue', done: 'green', active: 'green' } },
    priority: { label: 'Priority', type: 'enum', values: { must: 'amber', should: 'blue', could: 'slate' } },
    owner: { label: 'Owner', type: 'user' },
    source: { label: 'Source', type: 'enum', values: { regulatory: 'amber', product: 'blue', technical: 'slate' } },
    due: { label: 'Due', type: 'date' },
    drivers: { label: 'Drivers', type: 'links' },
    implements: { label: 'Implements', type: 'links' },
    satisfies: { label: 'Satisfies', type: 'links' },
    delivers: { label: 'Delivers', type: 'links' },
    maps_to: { label: 'Maps to', type: 'links' },
    verifies: { label: 'Verified by', type: 'links' },
    created: { label: 'Created', type: 'date' },
    updated: { label: 'Updated', type: 'date' },
  },
};

/** Whether new documents should seed this frontmatter key as an empty list. */
export const isLinkAttr = (key: string) => DEFAULT_PROPERTIES.fields![key]?.type === 'links';

// ---------------------------------------------------------------- raw parse

interface RawEntity {
  folder?: unknown; label?: unknown; icon?: unknown; color?: unknown;
  description?: unknown; group?: unknown; attributes?: unknown; hidden?: unknown;
}

/** The config sections as WRITTEN — no defaults. Undefined = section absent. */
export interface RawConfig {
  entities?: Record<string, RawEntity>;
  drivers?: Record<string, { label?: unknown; icon?: unknown; color?: unknown }>;
  statuses?: string[];
  link_types?: Record<string, { from?: unknown; to?: unknown }>;
  ids?: Record<string, { pattern?: unknown }>;
  properties?: PropertySchema;
}

const str = (v: unknown): string =>
  typeof v === 'string' ? v : typeof v === 'number' || typeof v === 'boolean' ? String(v) : '';
const strList = (v: unknown): string[] =>
  Array.isArray(v) ? v.map(str).filter(Boolean) : str(v) ? [str(v)] : [];
const isMap = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v);

export function parseRawConfig(yml: string | undefined): RawConfig {
  if (!yml || !yml.trim()) return {};
  let doc: unknown;
  try { doc = parse(yml); } catch { return {}; } // mid-edit garbage → defaults
  if (!isMap(doc)) return {};
  const out: RawConfig = {};
  if (isMap(doc.entities)) out.entities = doc.entities as RawConfig['entities'];
  if (isMap(doc.drivers)) out.drivers = doc.drivers as RawConfig['drivers'];
  if (Array.isArray(doc.statuses)) out.statuses = doc.statuses.map(str).filter(Boolean);
  if (isMap(doc.link_types)) out.link_types = doc.link_types as RawConfig['link_types'];
  if (isMap(doc.ids)) out.ids = doc.ids as RawConfig['ids'];
  if (isMap(doc.properties)) {
    const p = doc.properties as Record<string, unknown>;
    const props: PropertySchema = {};
    if (Array.isArray(p.order)) props.order = p.order.map(str).filter(Boolean);
    if (isMap(p.fields)) {
      props.fields = {};
      for (const [key, f0] of Object.entries(p.fields)) {
        const f = isMap(f0) ? f0 : {};
        const def: { label?: string; type?: string; values?: Record<string, string> } = {};
        if (str(f.label)) def.label = str(f.label);
        if (str(f.type)) def.type = str(f.type);
        if (isMap(f.values)) {
          def.values = {};
          for (const [v, c] of Object.entries(f.values)) def.values[v] = str(c);
        }
        props.fields[key] = def;
      }
    }
    if (props.order || props.fields) out.properties = props;
  }
  return out;
}

// ---------------------------------------------------------------- effective

function effectiveEntities(raw: RawConfig): EntityDef[] {
  const out = BUILTIN_ENTITIES.map((e) => ({ ...e }));
  const hidden = new Set<string>();
  for (const [kind, spec0] of Object.entries(raw.entities || {})) {
    const spec: RawEntity = isMap(spec0) ? spec0 : {};
    if (spec.hidden === true) { hidden.add(kind); continue; }
    const folderRaw = str(spec.folder);
    const folder = folderRaw ? (folderRaw.endsWith('/') ? folderRaw : folderRaw + '/') : '';
    const attributes = Array.isArray(spec.attributes) ? spec.attributes.map(str).filter(Boolean) : undefined;
    const i = out.findIndex((e) => e.kind === kind);
    if (i >= 0) {
      // override: only the fields the config provides
      const cur = out[i];
      out[i] = {
        ...cur,
        folder: folder || cur.folder,
        label: str(spec.label) || cur.label,
        icon: str(spec.icon) || cur.icon,
        color: str(spec.color) || cur.color,
        description: str(spec.description) || cur.description,
        group: str(spec.group) || cur.group,
        attributes: attributes ?? cur.attributes,
      };
    } else {
      out.push({
        kind,
        folder: folder || kind + 's/',
        label: str(spec.label) || kind.replace(/_/g, ' '),
        icon: str(spec.icon) || '▢',
        color: str(spec.color) || 'var(--text-2)',
        description: str(spec.description) || '',
        group: str(spec.group) || undefined,
        attributes,
        builtin: false,
      });
    }
  }
  return out.filter((e) => !hidden.has(e.kind));
}

const driverList = (raw: RawConfig['drivers']): DriverDef[] =>
  Object.entries(raw || {}).map(([key, d0]) => {
    const d = isMap(d0) ? d0 : {};
    return { key, label: str(d.label) || key, icon: str(d.icon) || '•', color: str(d.color) || 'var(--text-2)' };
  });

const linkTypeList = (raw: RawConfig['link_types']): LinkTypeDef[] =>
  Object.entries(raw || {}).map(([name, l0]) => {
    const l = isMap(l0) ? l0 : {};
    return { name, from: strList(l.from).join(', '), to: strList(l.to).join(', ') };
  });

/** Effective workspace config: the file's sections over the built-in defaults. */
export function workspaceConfig(yml?: string): WorkspaceConfig {
  const raw = parseRawConfig(yml);
  return {
    entities: effectiveEntities(raw),
    drivers: raw.drivers ? driverList(raw.drivers) : DEFAULT_DRIVERS,
    statuses: raw.statuses?.length ? raw.statuses : DEFAULT_STATUSES,
    linkTypes: raw.link_types ? linkTypeList(raw.link_types) : DEFAULT_LINK_TYPES,
    properties: raw.properties || DEFAULT_PROPERTIES,
    hasProperties: !!raw.properties,
  };
}

/**
 * The taxonomy as the FILE declares it — no defaults merged in. This is what
 * the config-file summary renders; the app itself always reads the effective
 * config above.
 */
export function rawTaxonomy(yml: string) {
  const raw = parseRawConfig(yml);
  return {
    drivers: driverList(raw.drivers),
    statuses: raw.statuses || [],
    links: linkTypeList(raw.link_types),
    properties: raw.properties,
  };
}

/** The config's `ids:` entries (kind → pattern), no built-ins. */
export function idSchemes(configYml?: string): { kind: string; pattern: string }[] {
  const raw = parseRawConfig(configYml);
  return Object.entries(raw.ids || {})
    .map(([kind, s]) => ({ kind, pattern: str(isMap(s) ? s.pattern : '') }))
    .filter((s) => s.pattern);
}
