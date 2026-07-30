import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useBlocker, useParams } from 'react-router-dom';
import { useNav } from '../state/nav';
import { useQueryClient } from '@tanstack/react-query';
import { marked } from 'marked';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useFileAtHead, useFileQuery, useSaveFile } from '../api/hooks';
import { api, rawUrl, uploadAsset } from '../api/client';
import { esc, isReservedMd, resolveDocHref, resolvePath, scalar, stripFrontmatter } from '../lib/model';
import { assemble, fmToJS, touchUpdated } from '../lib/frontmatter';
import { HistoryDrawer } from '../components/HistoryDrawer';
import { MoveDialog } from '../components/MoveDialog';
import { ShareDialog } from '../components/ShareDialog';
import { buildProps, collectBacklinks, defaultDoc, srcMeta } from '../lib/derive';
import type { DocBacklink } from '../lib/derive';
import type { EntityDef } from '../lib/entities';
import { scaffoldFor } from '../lib/scaffold';
import { newDocTemplate } from '../lib/newdoc';
import { knownTargets, linkifyReferences, suggestReferences } from '../lib/refs';
import { DocBody } from '../components/DocBody';
import { AsciiDoc } from '../components/AsciiDoc';
import { ConfigDoc } from '../components/ConfigDoc';
import { isCodeExt } from '../lib/langs';
import { useDraft } from '../hooks/useDraft';
import { useWorkspace } from '../hooks/useWorkspace';
import { useToasts } from '../components/Toast';
import { useNarrow } from '../hooks/useMediaQuery';
import { MilkdownEditor, MilkdownApi } from '../editors/MilkdownEditor';
import { SourceEditor } from '../editors/SourceEditor';
import { PropertiesForm } from '../editors/PropertiesForm';
import { ExcalidrawModal } from '../editors/ExcalidrawModal';
import { IconShare, IconSpark, IconTrace, IconClose, IconDiagram, IconPen, IconImage, IconLink, IconLock, IconMenu } from '../components/icons';



// Read-mode chip for a doc-linking frontmatter item (implements, maps_to,
// diagrams, …): the target family's entity icon in its color, colored left
// edge, doc name in mono, anchor muted — same language as the driver and
// backlinks chips.
function RefChipView({ text, path, docTitle, entities, nav }: {
  text: string;
  path: string;
  docTitle?: string;
  entities: EntityDef[];
  nav: (p: string) => void;
}) {
  const folder = path.split('/')[0];
  const meta = entities.find((e) => e.folder.replace(/\/$/, '') === folder);
  const icon = meta?.icon || '▢';
  const color = meta?.color || 'var(--text-2)';
  const anchor = text.includes('#') ? text.split('#')[1] : '';
  return (
    <span
      onClick={() => nav('/editor/' + path)}
      title={'open ' + path}
      style={sx('display:inline-flex;align-items:center;gap:6px;padding:2px 9px;border:1px solid var(--border);border-left:3px solid ' + color + ';border-radius:7px;background:var(--surface-2);font-size:11.5px;cursor:pointer')}
    >
      <span style={{ color }}>{icon}</span>
      <span style={sx("font-family:'JetBrains Mono',monospace;font-weight:600;color:var(--prod)")}>{path.split('/').pop()!.replace(/\.(md|excalidraw|mermaid)$/, '')}</span>
      {anchor && <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3)")}>#{anchor}</span>}
      {docTitle && <span style={sx('color:var(--text-2);max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{docTitle}</span>}
    </span>
  );
}

// Read-mode driver chip, styled like the backlinks chips: type icon in its
// color, colored left edge, doc name in mono, anchor muted. parseProps folds
// each drivers entry to "type · ref"; prose refs render unlinked.
function DriverChipView({ raw, titles, nav }: { raw: string; titles: Record<string, string>; nav: (p: string) => void }) {
  const [type, ...rest] = raw.split(' · ');
  const ref = rest.join(' · ');
  const m = srcMeta(type);
  const pm = ref.match(/([\w-]+\/[\w.\/-]+\.md)/);
  const anchor = ref.includes('#') ? ref.split('#')[1] : '';
  const docTitle = pm ? titles[pm[1]] : undefined;
  return (
    <span
      onClick={pm ? () => nav('/editor/' + pm[1]) : undefined}
      title={pm ? 'open ' + pm[1] : undefined}
      style={sx('display:inline-flex;align-items:center;gap:6px;padding:2px 9px;border:1px solid var(--border);border-left:3px solid ' + m.fg + ';border-radius:7px;background:var(--surface-2);font-size:11.5px;' + (pm ? 'cursor:pointer' : ''))}
    >
      <span style={{ color: m.fg }}>{m.icon}</span>
      {pm
        ? <span style={sx("font-family:'JetBrains Mono',monospace;font-weight:600;color:var(--prod)")}>{pm[1].split('/').pop()!.replace(/\.md$/, '')}</span>
        : <span style={sx('color:var(--text-2)')}>{ref}</span>}
      {pm && anchor && <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3)")}>#{anchor}</span>}
      {docTitle && <span style={sx('color:var(--text-2);max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{docTitle}</span>}
    </span>
  );
}

// Computed backlinks panel: every inbound link to this document — driver
// citations, the other typed frontmatter relations, and body-text mentions.
// Deliberately its OWN box, not a row in the Properties panel — these are
// derived from the citing documents and are never stored in this one
// (they replaced the manual `drives:` key).
function BacklinksPanel({ links, nav }: { links: DocBacklink[]; nav: (p: string) => void }) {
  return (
    <div style={sx('margin:0 0 30px;border:1px dashed var(--border-2);border-radius:10px')}>
      <div style={sx('display:flex;align-items:center;gap:8px;padding:8px 14px;border-bottom:1px dashed var(--border)')}>
        <span style={sx('color:var(--text-3);font-size:12px')}>↳</span>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Backlinks</span>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>· computed from links to this document — not stored in it</span>
      </div>
      <div style={sx('display:flex;flex-wrap:wrap;gap:6px;align-items:center;padding:10px 14px')}>
        {links.map((l) => {
          const m = l.kind === 'driver' ? srcMeta(l.type || '') : null;
          return (
            <span
              key={l.from + '|' + l.kind}
              onClick={() => nav('/editor/' + l.from)}
              title={'open ' + l.from}
              style={sx('display:inline-flex;align-items:center;gap:6px;padding:2px 9px;border-radius:6px;font-size:11.5px;cursor:pointer;background:var(--surface-2)')}
            >
              {m
                ? <span style={{ color: m.fg }}>{m.icon}</span>
                : <span style={sx("font-family:'JetBrains Mono',monospace;font-size:9.5px;color:var(--text-3)")}>{l.kind}</span>}
              <span style={sx("font-family:'JetBrains Mono',monospace;font-weight:600;color:var(--prod)")}>{l.id || l.from.split('/').pop()}</span>
              {l.title && <span style={sx('color:var(--text-2);max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{l.title}</span>}
            </span>
          );
        })}
      </div>
    </div>
  );
}

// docPath keeps the document in the URL across the editor↔graph roundtrip:
// the editor tab reopens THAT doc, the graph tab carries it as focus context.
export function docTabsStrip(active: 'editor' | 'graph', docName: string, nav: (p: string) => void, dirty?: boolean, docPath?: string) {
  const tab = (on: boolean) => on
    ? 'background:var(--bg);color:var(--text);border-bottom:2px solid var(--text)'
    : 'background:transparent;color:var(--text-3);border-bottom:2px solid transparent;border-right:1px solid var(--border)';
  return (
    <div style={sx('height:38px;flex:none;display:flex;align-items:stretch;background:var(--panel);border-bottom:1px solid var(--border);padding-left:2px')}>
      <div onClick={() => nav(docPath ? '/editor/' + docPath : '/editor')} style={sx('display:flex;align-items:center;gap:8px;padding:0 14px;cursor:pointer;' + tab(active === 'editor'))}>
        <span style={sx('color:var(--reg)')}>◈</span>
        <span style={sx('font-size:12.5px;font-weight:600')}>{docName}</span>
        {dirty && <span style={sx('width:5px;height:5px;border-radius:50%;background:var(--reg)')} />}
      </div>
      <div onClick={() => nav(docPath ? '/graph/' + docPath : '/graph')} style={sx('display:flex;align-items:center;gap:8px;padding:0 14px;cursor:pointer;' + tab(active === 'graph'))}>
        <IconTrace size={13} width={1.9} />
        <span style={sx('font-size:12.5px;font-weight:600')}>Impact Graph</span>
      </div>
      <div style={sx('flex:1')} />
    </div>
  );
}

type Kind = 'md' | 'mermaid' | 'excalidraw' | 'yaml' | 'image' | 'adoc' | 'code' | 'text';

function kindOf(name: string): Kind {
  const ext = name.split('.').pop()!.toLowerCase();
  if (/^(png|jpe?g|gif|webp|svg)$/.test(ext)) return 'image'; // the /raw asset types
  if (ext === 'md' || ext === 'markdown') return 'md';
  if (ext === 'excalidraw') return 'excalidraw';
  if (ext === 'mermaid' || ext === 'mmd') return 'mermaid';
  if (ext === 'yml' || ext === 'yaml') return 'yaml';
  if (ext === 'adoc' || ext === 'asciidoc') return 'adoc';
  // json stays 'text': the view route renders it through ConfigDoc
  if (ext !== 'json' && isCodeExt(ext)) return 'code';
  return 'text';
}

export function EditorView() {
  const nav = useNav();
  const app = useApp();
  const { '*': splat } = useParams();
  // "~<repoId>/<path>" targets a read-only input repo (default branch)
  const raw0 = splat || defaultDoc(app.files, app.entities);
  const roMatch = raw0.match(/^~([\w-]+)\/(.+)$/);
  // read-only: reference-repo documents, and viewers (per-repo role) — the
  // server refuses their writes anyway, the chrome just degrades to match
  const readOnly = !!roMatch || !app.canEdit;
  const fileRepo = roMatch ? roMatch[1] : app.repoId;
  const fileRef = roMatch ? '' : app.branch;
  const path = roMatch ? roMatch[2] : raw0;
  // OKF reserved files are derived artifacts, regenerated at commit time —
  // viewable but never hand-edited (a manual edit would be overwritten anyway)
  const generated = isReservedMd(path);
  const name = path.split('/').pop()!;
  const kind = kindOf(name);
  const ext = name.split('.').pop()!.toLowerCase();
  // images never travel through the JSON files endpoint (it would mangle the
  // bytes) — they render straight off /raw
  const file = useFileQuery(fileRepo, fileRef, kind === 'image' ? undefined : path);
  const save = useSaveFile(app.repoId, app.branch); // sketch-file creation
  const toasts = useToasts();
  const narrow = useNarrow();
  const { ensureWritableBranch } = useWorkspace();
  // documents open read-only by default; editing is an explicit mode
  const [mode, setMode] = useState<'view' | 'edit' | 'source'>('view');
  const [propsOpen, setPropsOpen] = useState(true);
  const [outlineOpen, setOutlineOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const [excalidrawPath, setExcalidrawPath] = useState<string | null>(null);
  const editorApi = useRef<MilkdownApi | null>(null);
  // bumped when a sketch is saved so embedded previews re-render
  const [sketchGen, setSketchGen] = useState(0);

  // durable draft: autosaves to the branch worktree; on protected branches the
  // buffer is carried until the workspace switch enables persistence
  const rawRef = useRef('');
  const { draft, setRaw, markDirty, syncState, flush, resolveConflict } = useDraft({
    repo: fileRepo,
    branch: app.branch,
    path,
    file,
    enabled: !readOnly && !generated && !app.isProtectedBranch && kind !== 'image',
    onRecovered: () => toasts.push({ text: `Recovered unsaved changes for ${name}`, kind: 'info' }),
    beforePersist: () => {
      const fresh = editorApi.current?.flush();
      if (fresh == null) return null;
      const nl = fresh.endsWith('\n') ? fresh : fresh + '\n';
      const curFm = stripFrontmatter(rawRef.current).fm;
      return curFm ? assemble(touchUpdated(curFm), '\n' + nl) : nl;
    },
  });
  rawRef.current = draft.raw;
  const conflict = syncState === 'conflict';
  // committed baseline for the source-mode changed-line gutter
  const headBaseline = useFileAtHead(fileRepo, app.branch, path, mode === 'source' && !readOnly && !generated && !app.isProtectedBranch);

  const qc = useQueryClient();

  const { fm, body } = useMemo(() => stripFrontmatter(draft.raw), [draft.raw]);
  const title = kind === 'md' ? scalar(fm, 'title') || name : name;
  const status = kind === 'md' ? scalar(fm, 'status') : '';

  const openPath = useCallback((rel: string) => {
    const dir = path.split('/').slice(0, -1).join('/');
    nav('/editor/' + resolveDocHref(dir, rel));
  }, [nav, path]);

  // frontmatter refs are workspace-root-relative by convention (unlike body
  // links, which resolve against the document's folder) — resolving them via
  // openPath would prepend the current dir ("regulations/requirements/…")
  const openFmPath = useCallback((p: string) => {
    nav('/editor/' + (p.startsWith('~') ? p : p.replace(/^\/+/, '')));
  }, [nav]);

  const onBodyChange = useCallback((md: string) => {
    const curFm = stripFrontmatter(rawRef.current).fm;
    const nl = md.endsWith('\n') ? md : md + '\n';
    setRaw(curFm ? assemble(touchUpdated(curFm), '\n' + nl) : nl);
  }, [setRaw]);
  const onFmChange = useCallback((nextFm: string) => {
    // a property edit bumps `updated` — unless the edit IS a date override
    // (created/updated changed by hand), which must win over the auto-bump
    const prev = fmToJS(stripFrontmatter(rawRef.current).fm);
    const next = fmToJS(nextFm);
    const dateOverride = prev.updated !== next.updated || prev.created !== next.created;
    setRaw(assemble(dateOverride ? nextFm : touchUpdated(nextFm), stripFrontmatter(rawRef.current).body));
  }, [setRaw]);
  const onRawChange = useCallback((raw: string) => {
    // typing in source mode on a protected branch triggers the workspace
    // switch; the dirty draft is carried onto the new branch
    if (app.isProtectedBranch && !readOnly && !generated) void ensureWritableBranch();
    setRaw(raw);
  }, [setRaw, app.isProtectedBranch, readOnly, generated, ensureWritableBranch]);

  const enterEdit = useCallback(async () => {
    await ensureWritableBranch();
    setMode('edit');
  }, [ensureWritableBranch]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') { e.preventDefault(); void flush(); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [flush]);

  // in-app navigation guard: unflushed work auto-flushes; failures ask
  const blocker = useBlocker(
    !readOnly && !app.isProtectedBranch &&
    (syncState === 'pending' || syncState === 'saving' || syncState === 'error' || syncState === 'conflict'),
  );
  useEffect(() => {
    if (blocker.state !== 'blocked') return;
    if (syncState === 'error' || syncState === 'conflict') {
      if (window.confirm('Your latest changes could not be saved. Leave anyway? (a local recovery copy is kept)')) blocker.proceed();
      else blocker.reset();
      return;
    }
    void flush().finally(() => blocker.proceed());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [blocker.state]);

  const insertMermaid = () => {
    editorApi.current?.insert('```mermaid\nflowchart LR\n  A[Start] --> B[Next step]\n```');
  };

  // ---- images: doc-relative srcs load via /raw; paste/drop/pick uploads ----
  const docDir = path.split('/').slice(0, -1).join('/');
  const resolveAsset = useCallback((src: string) => {
    if (/^(https?:|data:|blob:)/.test(src) || !fileRepo) return src;
    // sketchGen busts the browser cache after a sketch save
    return rawUrl(fileRepo, app.branch, resolvePath(docDir, src)) + '&v=' + sketchGen;
  }, [fileRepo, app.branch, docDir, sketchGen]);
  const uploadImage = useCallback(async (imgFile: File): Promise<string | null> => {
    if (!fileRepo || readOnly) return null;
    try {
      const res = await uploadAsset(fileRepo, app.branch, docDir ? `${docDir}/assets` : 'assets', imgFile);
      qc.invalidateQueries({ queryKey: ['status', fileRepo, app.branch] });
      // repo-relative → doc-relative (always directly under <docdir>/assets)
      return 'assets/' + res.path.split('/').pop();
    } catch (e) {
      toasts.push({ text: `Image upload failed: ${(e as Error).message}`, kind: 'error' });
      return null;
    }
  }, [fileRepo, app.branch, docDir, readOnly, qc, toasts]);
  const imagePicker = useRef<HTMLInputElement>(null);
  const pickImage = async (list: FileList | null) => {
    for (const f of Array.from(list ?? [])) {
      const src = await uploadImage(f);
      if (src) editorApi.current?.insert(`![${f.name.replace(/\.\w+$/, '')}](${src})`);
    }
  };

  // create (or reuse) a sketch file, embed it at the cursor, open the editor
  const insertSketch = async () => {
    // dynamic default: <doc-basename>[-N]; sketches are PNGs with the scene
    // embedded (*.excalidraw.png) — natively viewable, editable in the modal
    const base = name.replace(/\.\w+$/, '').toLowerCase().replace(/[^a-z0-9-]+/g, '-') || 'sketch';
    const nameIn = window.prompt('Sketch name:', base);
    if (!nameIn) return;
    const slug = nameIn.toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-+|-+$/g, '') || 'sketch';
    const target = `diagrams/${slug}.excalidraw.png`;
    const dir = path.split('/').slice(0, -1).join('/');
    const up = dir ? dir.split('/').map(() => '..').join('/') + '/' : '';
    editorApi.current?.insert(`![${slug}](${up}${target})`);
    // the file itself is created by the sketch editor's first save
    setExcalidrawPath(target);
  };

  // reference tooling: auto-link known entities + suggest unlinked mentions
  const targets = useMemo(() => (app.model ? knownTargets(app.model) : []), [app.model]);
  const currentBodyMd = () => {
    const fresh = editorApi.current?.flush();
    return fresh != null ? fresh : stripFrontmatter(draft.raw).body;
  };
  const applyLinkify = (only?: string) => {
    const md = currentBodyMd();
    const linked = linkifyReferences(md, targets, path, only);
    if (linked !== md) editorApi.current?.replaceAll(linked);
  };

  const viewHtml = useMemo(() => {
    if (!draft.raw) return '';
    if (kind === 'md') {
      // strip the duplicated title heading — horizontal whitespace only, or
      // `\s` swallows following block content (e.g. an image after a bare #)
      const b = body.replace(/^\s*#[ \t]+.+\n+/, '').replace(/\s*\{#[\w-]+\}\s*$/gm, '');
      return marked.parse(b) as string;
    }
    if (kind === 'mermaid') return '<pre><code class="language-mermaid">' + esc(draft.raw.replace(/^%%.*\n/, '')) + '</code></pre>';
    if (kind === 'excalidraw') return '<div data-excalidraw="1"></div>';
    return '<pre style="white-space:pre-wrap"><code>' + esc(draft.raw) + '</code></pre>';
  }, [kind, draft.raw, body]);

  const viewProps = useMemo(
    () => (kind === 'md' && fm ? buildProps(fm, app.schema) : []),
    [kind, fm, app.schema],
  );

  // every inbound link to this doc — shown as a computed panel
  const backlinks = useMemo(
    () => (kind === 'md' && app.model ? collectBacklinks(app.model)[path] || [] : []),
    [kind, app.model, path],
  );

  // path → frontmatter title, for the link chips' secondary text
  const docTitles = useMemo(() => {
    const m: Record<string, string> = {};
    for (const [p, raw] of Object.entries(app.files || {})) {
      if (!p.endsWith('.md')) continue;
      const t = (stripFrontmatter(raw).fm.match(/^title:\s*["']?(.*?)["']?\s*$/m) || [])[1];
      if (t) m[p] = t;
    }
    return m;
  }, [app.files]);

  const change = app.model?.changes.find((c) => c.status === 'triage');
  const tseg = (on: boolean) => (on ? 'background:var(--surface);box-shadow:var(--shadow);color:var(--text)' : 'color:var(--text-3)');
  // ready only when the draft belongs to *this* path — during a file switch
  // the draft briefly still holds the previous document; images skip the file
  // query entirely and are always ready
  const ready = kind === 'image' || (!!file.data && draft.path === path && draft.raw !== '');
  const editable = kind === 'md' && !readOnly && !generated;
  // a persisted 'edit'/'source' choice degrades gracefully on files where the
  // mode doesn't exist
  const effMode = (mode === 'edit' && !editable) || (mode === 'source' && kind === 'image') ? 'view' : mode;
  // sketches are editable straight from their image view (workspace only)
  const sketchEditable = /\.excalidraw\.png$/i.test(name) && !readOnly && !generated;

  // outline: h1-h3 headings for the sticky TOC (code fences skipped)
  const outline = useMemo(() => {
    if (kind !== 'md') return [];
    const out: { level: number; text: string }[] = [];
    let fence = false;
    for (const line of body.split('\n')) {
      if (/^```/.test(line.trim())) { fence = !fence; continue; }
      if (fence) continue;
      const m = line.match(/^(#{1,3})[ \t]+(.+?)\s*$/);
      if (m) {
        const text = m[2]
          .replace(/\s*\{#[\w-]+\}\s*$/, '')
          .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1') // links → their text
          .replace(/[*_`~]/g, ''); // inline emphasis/code markers
        out.push({ level: m[1].length, text });
      }
    }
    return out;
  }, [body, kind]);
  const jumpToHeading = useCallback((idx: number) => {
    const host = document.querySelector(effMode === 'view' ? '#specquill-doc' : '.milkdown-editable');
    if (!host) return;
    // view mode strips the leading title heading from the rendered html
    const stripped = effMode === 'view' && outline[0]?.level === 1;
    const target = stripped ? idx - 1 : idx;
    if (target < 0) { host.closest('[data-doc-scroll]')?.scrollTo({ top: 0, behavior: 'smooth' }); return; }
    host.querySelectorAll('h1,h2,h3')[target]?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [effMode, outline]);

  const suggestions = useMemo(() => {
    if (effMode !== 'edit' || !targets.length || !ready) return [];
    return suggestReferences(stripFrontmatter(draft.raw).body, targets, path).slice(0, 6);
  }, [effMode, targets, draft.raw, path, ready]);

  return (
    <div style={sx('flex:1;min-height:0;display:flex;flex-direction:column')}>
      {!narrow && docTabsStrip('editor', name, nav, draft.dirty, raw0)}
      <div style={sx('height:40px;flex:none;display:flex;align-items:center;gap:' + (narrow ? '8px' : '12px') + ';padding:0 ' + (narrow ? '10px' : '16px') + ';background:var(--surface);border-bottom:1px solid var(--border);' + (narrow ? 'overflow-x:auto;overflow-y:hidden' : ''))}>
        <div style={sx("display:flex;align-items:center;gap:6px;font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-2);min-width:30px;overflow:hidden")}>
          <span style={sx('color:var(--text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{path}</span>
          {draft.dirty && <span title="unsaved changes" style={sx('flex:none;width:6px;height:6px;border-radius:50%;background:var(--reg)')} />}
        </div>
        <div style={sx('flex:1')} />
        {effMode === 'edit' && (
          <>
            <span style={sx('flex:none;display:inline-flex;border:1px solid var(--border-2);border-radius:7px;overflow:hidden')}>
              {([['strong', 'B', 'Bold (Ctrl+B)', 'font-weight:800'], ['em', 'I', 'Italic (Ctrl+I)', 'font-style:italic'], ['strike', 'S', 'Strikethrough', 'text-decoration:line-through'], ['code', '‹›', 'Inline code', "font-family:'JetBrains Mono',monospace;font-size:10.5px"]] as const).map(([mark, label, title, style]) => (
                <button key={mark} onMouseDown={(e) => e.preventDefault()} onClick={() => editorApi.current?.format(mark)} title={title}
                  style={sx('flex:none;height:26px;width:26px;border:none;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer;' + style)}>
                  {label}
                </button>
              ))}
            </span>
            <button onClick={insertMermaid} title="Insert a mermaid diagram at the cursor"
              style={sx('flex:none;display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer')}>
              <IconDiagram /> Diagram
            </button>
            <button onClick={insertSketch} title="Create an excalidraw sketch and embed it at the cursor"
              style={sx('flex:none;display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer')}>
              <IconPen /> Sketch
            </button>
            <button onClick={() => imagePicker.current?.click()} title="Upload an image and embed it at the cursor (or just paste/drop one)"
              style={sx('flex:none;display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer')}>
              <IconImage /> Image
            </button>
            <input ref={imagePicker} type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml" multiple hidden
              onChange={(e) => { void pickImage(e.target.files); e.target.value = ''; }} />
            <button onClick={() => applyLinkify()} title="Turn plain-text mentions of requirements, specs and fields into links"
              style={sx('flex:none;display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer')}>
              <IconLink /> Link refs
            </button>
            <span style={sx('width:1px;height:20px;background:var(--border)')} />
          </>
        )}
        {!readOnly && syncState !== 'clean' && (
          <span data-sync={syncState} style={sx("display:inline-flex;align-items:center;gap:5px;font-size:11.5px;font-family:'JetBrains Mono',monospace;" +
            (syncState === 'saved' ? 'color:var(--data)' : syncState === 'error' || syncState === 'conflict' ? 'color:var(--del)' : 'color:var(--text-3)'))}>
            {syncState === 'saved' ? 'Saved ✓'
              : syncState === 'saving' ? 'Saving…'
              : syncState === 'pending' ? (app.isProtectedBranch ? 'unsaved' : 'Saving…')
              : syncState === 'conflict' ? 'conflict'
              : 'Save failed'}
            {syncState === 'error' && (
              <button onClick={() => void flush()} style={sx('height:22px;padding:0 9px;border:1px solid var(--reg-line);border-radius:6px;background:var(--surface);color:var(--del);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer')}>
                Retry
              </button>
            )}
          </span>
        )}
        <div style={sx('flex:none;display:flex;background:var(--surface-2);border:1px solid var(--border);border-radius:8px;padding:2px')}>
          <span onClick={() => setMode('view')} style={sx('padding:3px 12px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;' + tseg(effMode === 'view'))}>View</span>
          {editable && (
            <span onClick={() => void enterEdit()} style={sx('padding:3px 12px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;' + tseg(effMode === 'edit'))}>Edit</span>
          )}
          {kind !== 'image' && (
            <span onClick={() => setMode('source')} style={sx('padding:3px 12px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;' + tseg(effMode === 'source'))}>Source</span>
          )}
          <span onClick={() => setHistoryOpen(true)} style={sx('padding:3px 12px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;' + tseg(historyOpen))}>History</span>
        </div>
        <span style={sx('width:1px;height:20px;background:var(--border)')} />
        {!readOnly && !generated && (
          <button onClick={() => setMoveOpen(true)} title="Move or rename this file — referencing documents can be rewritten"
            style={sx('flex:none;display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer')}>
            Move
          </button>
        )}
        <button onClick={() => setShareOpen(true)} title="Share this workspace as an OKF bundle (unauthenticated zip link)"
          style={sx('flex:none;display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer')}>
          <IconShare />Share
        </button>
      </div>
      {historyOpen && <HistoryDrawer path={path} onClose={() => setHistoryOpen(false)} />}
      {moveOpen && <MoveDialog path={path} onClose={() => setMoveOpen(false)} />}
      {shareOpen && <ShareDialog onClose={() => setShareOpen(false)} />}

      {conflict && (
        <div style={sx('flex:none;display:flex;align-items:center;gap:10px;padding:8px 16px;background:var(--reg-bg);border-bottom:1px solid var(--reg-line);color:var(--reg);font-size:12.5px')}>
          Someone else changed this file since you loaded it.
          <button onClick={() => void resolveConflict('mine')} style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
            Keep my version
          </button>
          <button onClick={() => void resolveConflict('theirs')} style={sx('height:24px;padding:0 10px;border:1px solid var(--reg-line);border-radius:6px;background:var(--surface);color:var(--reg);font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
            Discard mine &amp; reload
          </button>
        </div>
      )}

      <div data-doc-scroll style={sx('flex:1;overflow-y:auto;padding:' + (narrow ? '16px 14px 60px' : '34px 40px 80px'))}>
        {outline.length > 1 && !narrow && effMode !== 'source' && (
          <div style={sx('position:sticky;top:45vh;float:right;height:0;z-index:6;display:flex;flex-direction:column;align-items:flex-end')}>
            <button data-outline onClick={() => setOutlineOpen((v) => !v)} title="Outline"
              style={sx('display:flex;align-items:center;gap:5px;height:26px;padding:0 9px;border:1px solid var(--border);border-radius:7px;background:color-mix(in srgb, var(--surface) 92%, transparent);backdrop-filter:blur(4px);color:var(--text-3);font-family:inherit;font-size:11px;cursor:pointer;' + (outlineOpen ? 'color:var(--text)' : ''))}>
              <IconMenu /> Outline
            </button>
            {/* the list's flex:none is load-bearing: the height:0 sticky wrapper
                is a flex column, and an overflow-y:auto child has no automatic
                minimum size — without it the list gets crushed to a sliver */}
            {outlineOpen && (
              <div data-outline-list style={sx('flex:none;margin-top:6px;width:210px;padding:8px 6px;background:var(--surface);border:1px solid var(--border);border-radius:10px;box-shadow:var(--shadow-lg);max-height:62vh;overflow-y:auto')}>
                {outline.map((h, i) => (
                  <div key={i} onClick={() => jumpToHeading(i)}
                    style={{ ...sx('padding:3px 8px;border-radius:6px;font-size:11.5px;color:var(--text-2);cursor:pointer;overflow:hidden;text-overflow:ellipsis;white-space:nowrap'), paddingLeft: 8 + (h.level - 1) * 11 }}>
                    {h.text}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
        {file.isLoading && (
          <div style={sx('max-width:820px;margin:0 auto;display:flex;flex-direction:column;gap:13px')}>
            <div style={sx('height:30px;width:52%;border-radius:7px;background:var(--surface-2)')} />
            <div style={sx('height:12px;width:38%;border-radius:6px;background:var(--surface-2)')} />
            <div style={sx('height:12px;width:92%;border-radius:6px;background:var(--surface-2);margin-top:14px')} />
            <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>rendering {path}…</div>
          </div>
        )}
        {file.error != null && (
          <div style={sx('max-width:820px;margin:0 auto;padding:16px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:10px;color:var(--reg);font-size:13px')}>
            Couldn't load {path}: {String((file.error as Error).message || file.error)}
            {generated && /not found/.test(String((file.error as Error).message || '')) && (
              <div style={sx('margin-top:10px;color:var(--text-2);font-size:12px')}>
                {name} is generated automatically at commit time — it can't be created manually.
              </div>
            )}
            {/* missing optional workspace files (and plain docs) can be created in place */}
            {!readOnly && !generated && /not found/.test(String((file.error as Error).message || '')) &&
              (scaffoldFor(path, app.repoId || '') !== null || kind === 'md') && (
              <div style={sx('margin-top:10px')}>
                <button
                  onClick={() => void (async () => {
                    const content = scaffoldFor(path, app.repoId || '') ?? newDocTemplate(path, app.entities, { schema: app.schema });
                    const branch = await ensureWritableBranch();
                    await api<{ sha: string }>(`/api/repos/${app.repoId}/files/${path}?branch=${encodeURIComponent(branch)}`, {
                      method: 'PUT',
                      body: JSON.stringify({ content, baseSha: '' }),
                    });
                    qc.invalidateQueries({ queryKey: ['file', fileRepo] });
                    qc.invalidateQueries({ queryKey: ['status', app.repoId] });
                    qc.invalidateQueries({ queryKey: ['snapshot', app.repoId] });
                  })()}
                  style={sx('height:28px;padding:0 12px;border:1px solid var(--reg-line);border-radius:7px;background:var(--surface);color:var(--reg);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
                  Create {name}
                </button>
                {scaffoldFor(path, '') !== null && (
                  <span style={sx('margin-left:10px;color:var(--text-2);font-size:12px')}>
                    This workspace runs on built-in defaults — create the file to customize them.
                  </span>
                )}
              </div>
            )}
          </div>
        )}

        {/* ---- View: read-only render (the default) ---- */}
        {effMode === 'view' && ready && (
          <div style={sx('max-width:820px;margin:0 auto')}>
            <div style={sx('display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:12px')}>
              <h1 style={sx('margin:0;font-size:29px;font-weight:700;letter-spacing:-.5px;line-height:1.15')}>{title}</h1>
              {status && (
                <span style={sx('display:inline-flex;align-items:center;gap:6px;padding:4px 10px;border-radius:20px;background:var(--reg-bg);color:var(--reg);font-size:11.5px;font-weight:600;text-transform:capitalize')}>
                  <span style={sx('width:6px;height:6px;border-radius:50%;background:var(--reg)')} />{status.replace(/_/g, ' ')}
                </span>
              )}
              {readOnly && (
                <span style={sx('display:inline-flex;align-items:center;gap:5px;padding:4px 10px;border-radius:20px;background:var(--surface-2);color:var(--text-3);font-size:11.5px;font-weight:600')}>
                  <IconLock /> read-only · {fileRepo}
                </span>
              )}
              {generated && (
                <span title="Regenerated automatically at commit time — manual edits are overwritten"
                  style={sx('display:inline-flex;align-items:center;gap:5px;padding:4px 10px;border-radius:20px;background:var(--surface-2);color:var(--text-3);font-size:11.5px;font-weight:600')}>
                  ⟳ generated
                </span>
              )}
              <div style={sx('flex:1')} />
              {editable && (
                <button onClick={() => void enterEdit()} style={sx('display:inline-flex;align-items:center;gap:5px;height:28px;padding:0 13px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
                  <IconPen /> Edit
                </button>
              )}
            </div>
            {viewProps.length > 0 && (
              <div style={sx('margin:16px 0 30px;border:1px solid var(--border);border-radius:10px;overflow:hidden;background:var(--surface)')}>
                <div onClick={() => setPropsOpen((v) => !v)} style={sx('display:flex;align-items:center;gap:8px;padding:8px 14px;background:var(--surface-2);cursor:pointer;user-select:none')}>
                  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" strokeWidth="2.6" style={{ transform: propsOpen ? 'rotate(90deg)' : 'rotate(0deg)', transition: 'transform .15s' }}>
                    <path d="M9 6l6 6-6 6" />
                  </svg>
                  <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Properties</span>
                  <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>· {viewProps.length} fields</span>
                </div>
                {propsOpen && viewProps.map((p) => (
                  <div key={p.key} style={sx('display:flex;gap:14px;padding:8px 14px;border-top:1px solid var(--border)')}>
                    <span style={sx("width:132px;flex:none;font-family:'JetBrains Mono',monospace;font-size:11px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.3px;padding-top:2px")}>{p.key}</span>
                    <div style={sx('flex:1;display:flex;flex-wrap:wrap;gap:6px;align-items:center;min-width:0')}>
                      {p.rawKey === 'drivers'
                        ? p.items.map((it, i) => <DriverChipView key={i} raw={it.text} titles={docTitles} nav={nav} />)
                        : p.items.map((it, i) => (
                          it.openPath
                            ? <RefChipView key={i} text={it.text} path={it.openPath} docTitle={docTitles[it.openPath]} entities={app.entities} nav={nav} />
                            : <span key={i} style={sx(it.style)}>{it.text}</span>
                        ))}
                    </div>
                  </div>
                ))}
              </div>
            )}
            {backlinks.length > 0 && <BacklinksPanel links={backlinks} nav={nav} />}
            {kind === 'image' ? (
              fileRepo && (
                <div
                  onClick={sketchEditable ? () => void ensureWritableBranch().then(() => setExcalidrawPath(path)) : undefined}
                  title={sketchEditable ? 'Click to edit the sketch' : undefined}
                  style={sx('text-align:center' + (sketchEditable ? ';cursor:pointer' : ''))}
                >
                  <img
                    src={rawUrl(fileRepo, fileRef, path) + '&v=' + sketchGen}
                    alt={name}
                    style={sx('max-width:100%;border:1px solid var(--border);border-radius:10px;background:var(--surface);padding:8px')}
                  />
                </div>
              )
            ) : kind === 'excalidraw' && !readOnly ? (
              <div
                onClick={() => void ensureWritableBranch().then(() => setExcalidrawPath(path))}
                title="Click to edit the sketch"
                style={sx('cursor:pointer')}
              >
                <DocBody html={viewHtml} docPath={path} />
              </div>
            ) : kind === 'yaml' || name.endsWith('.json') ? (
              <ConfigDoc path={path} raw={draft.raw} />
            ) : kind === 'adoc' ? (
              <AsciiDoc raw={draft.raw} docPath={path} />
            ) : kind === 'code' ? (
              <SourceEditor value={draft.raw} lang={ext} onChange={() => {}} readOnly />
            ) : (
              <DocBody html={viewHtml} docPath={path} />
            )}
            {app.aiSuggestions && change && kind === 'md' && !readOnly && (
              <div style={sx('margin-top:24px;border:1px solid var(--ai-line);border-radius:10px;overflow:hidden;background:var(--surface);box-shadow:var(--shadow)')}>
                <div style={sx('display:flex;align-items:center;gap:9px;padding:10px 14px;background:var(--ai-bg);border-bottom:1px solid var(--ai-line)')}>
                  <IconSpark size={14} stroke="var(--ai)" />
                  <span style={sx('font-size:12px;font-weight:600;color:var(--ai)')}>Speccy suggests an edit</span>
                  <span style={sx('font-size:11px;color:var(--text-2)')}>from {change.name} · {change.published}</span>
                  <div style={sx('flex:1')} />
                  <button onClick={() => nav('/diff?change=' + encodeURIComponent(change.path))} style={sx('height:26px;padding:0 11px;border:none;border-radius:6px;background:var(--ai);color:#fff;font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer')}>
                    Review diff →
                  </button>
                </div>
                <div style={sx('padding:12px 14px;font-size:13px;line-height:1.62;color:var(--text)')}>{change.summary}</div>
              </div>
            )}
          </div>
        )}

        {/* ---- Edit: WYSIWYG + properties form ---- */}
        {effMode === 'edit' && ready && editable && (
          <div style={sx('max-width:820px;margin:0 auto')}>
            {/* no-frontmatter docs still get the box: the add-property row can create the block */}
            {/* no overflow:hidden here — the combobox popups must escape the box */}
            <div style={sx('margin:0 0 30px;border:1px solid var(--border);border-radius:10px;background:var(--surface)')}>
              <div onClick={() => setPropsOpen((v) => !v)} style={sx('display:flex;align-items:center;gap:8px;padding:8px 14px;background:var(--surface-2);cursor:pointer;user-select:none;border-radius:' + (propsOpen ? '9px 9px 0 0' : '9px'))}>
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" strokeWidth="2.6" style={{ transform: propsOpen ? 'rotate(90deg)' : 'rotate(0deg)', transition: 'transform .15s' }}>
                  <path d="M9 6l6 6-6 6" />
                </svg>
                <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Properties</span>
                <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>· editable</span>
              </div>
              {propsOpen && (
                <PropertiesForm
                  fm={fm}
                  body={body}
                  schema={app.schema}
                  files={app.files}
                  onChange={onFmChange}
                  onOpenPath={openFmPath}
                />
              )}
            </div>
            {backlinks.length > 0 && <BacklinksPanel links={backlinks} nav={nav} />}
            <MilkdownEditor
              key={path + ':' + draft.gen + ':' + sketchGen + ':' + app.theme + (conflict ? ':c' : '')}
              body={body}
              docPath={path}
              files={app.files}
              onChange={onBodyChange}
              onDirty={markDirty}
              onOpenPath={openPath}
              onOpenExcalidraw={setExcalidrawPath}
              onReady={(api) => { editorApi.current = api; }}
              resolveAsset={resolveAsset}
              onUploadImage={uploadImage}
              onRequestImage={() => imagePicker.current?.click()}
              onRequestSketch={() => void insertSketch()}
            />
            {suggestions.length > 0 && (
              <div style={sx('margin-top:20px;border:1px solid var(--prod-line);border-radius:10px;overflow:hidden;background:var(--surface)')}>
                <div style={sx('display:flex;align-items:center;gap:8px;padding:8px 14px;background:var(--prod-bg)')}>
                  <span style={sx('color:var(--prod);display:inline-flex')}><IconLink size={13} /></span>
                  <span style={sx('font-size:12px;font-weight:600;color:var(--prod)')}>Suggested references</span>
                  <span style={sx('font-size:11px;color:var(--text-2)')}>mentioned in the text but not linked</span>
                </div>
                <div style={sx('display:flex;flex-wrap:wrap;gap:6px;padding:11px 14px')}>
                  {suggestions.map((s) => (
                    <span
                      key={s.path}
                      onClick={() => applyLinkify(s.path)}
                      title={'Link the first mention to ' + s.path}
                      style={sx("display:inline-flex;align-items:center;gap:5px;padding:4px 10px;border:1px solid var(--border);border-radius:20px;font-size:11.5px;color:var(--prod);cursor:pointer;background:var(--surface-2);font-family:'JetBrains Mono',monospace")}
                    >
                      ＋ {s.label}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        {effMode === 'source' && ready && (
          <div style={sx('max-width:960px;margin:0 auto')}>
            <SourceEditor
              value={draft.raw}
              lang={kind === 'md' ? 'markdown' : kind === 'yaml' ? 'yaml' : ext}
              onChange={onRawChange}
              readOnly={readOnly || generated}
              baseline={headBaseline.data?.content}
            />
          </div>
        )}
      </div>
      {excalidrawPath && (
        <ExcalidrawModal
          path={excalidrawPath}
          onClose={() => setExcalidrawPath(null)}
          onSaved={() => setSketchGen((g) => g + 1)}
        />
      )}
    </div>
  );
}
