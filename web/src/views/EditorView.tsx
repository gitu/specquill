import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useBlocker, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { marked } from 'marked';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { useFileAtHead, useFileQuery, useMe, usePresence, useSaveFile } from '../api/hooks';
import { rawUrl, uploadAsset } from '../api/client';
import { useCollabSession } from '../collab/useCollabSession';
import { userColor } from '../collab/session';
import { esc, resolvePath, scalar, stripFrontmatter } from '../lib/model';
import { assemble } from '../lib/frontmatter';
import { buildProps } from '../lib/derive';
import { knownTargets, linkifyReferences, suggestReferences } from '../lib/refs';
import { DocBody } from '../components/DocBody';
import { useDraft } from '../hooks/useDraft';
import { useWorkspace } from '../hooks/useWorkspace';
import { useToasts } from '../components/Toast';
import { useNarrow } from '../hooks/useMediaQuery';
import { MilkdownEditor, MilkdownApi } from '../editors/MilkdownEditor';
import { SourceEditor } from '../editors/SourceEditor';
import { PropertiesForm } from '../editors/PropertiesForm';
import { ExcalidrawModal } from '../editors/ExcalidrawModal';
import { IconShare, IconSpark, IconTrace, IconClose, IconDiagram, IconPen, IconImage, IconLink, IconUserPlus, IconLock, IconMenu } from '../components/icons';

const DEFAULT_DOC = 'specs/txn-report.md';

export function docTabsStrip(active: 'editor' | 'graph', docName: string, nav: (p: string) => void, dirty?: boolean) {
  const tab = (on: boolean) => on
    ? 'background:var(--bg);color:var(--text);border-bottom:2px solid var(--text)'
    : 'background:transparent;color:var(--text-3);border-bottom:2px solid transparent;border-right:1px solid var(--border)';
  return (
    <div style={sx('height:38px;flex:none;display:flex;align-items:stretch;background:var(--panel);border-bottom:1px solid var(--border);padding-left:2px')}>
      <div onClick={() => nav('/editor')} style={sx('display:flex;align-items:center;gap:8px;padding:0 14px;cursor:pointer;' + tab(active === 'editor'))}>
        <span style={sx('color:var(--reg)')}>◈</span>
        <span style={sx('font-size:12.5px;font-weight:600')}>{docName}</span>
        {dirty && <span style={sx('width:5px;height:5px;border-radius:50%;background:var(--reg)')} />}
      </div>
      <div onClick={() => nav('/graph')} style={sx('display:flex;align-items:center;gap:8px;padding:0 14px;cursor:pointer;' + tab(active === 'graph'))}>
        <IconTrace size={13} width={1.9} />
        <span style={sx('font-size:12.5px;font-weight:600')}>Impact Graph</span>
      </div>
      <div style={sx('flex:1')} />
    </div>
  );
}

type Kind = 'md' | 'mermaid' | 'excalidraw' | 'yaml' | 'text';

function kindOf(name: string): Kind {
  const ext = name.split('.').pop()!;
  return ext === 'md' ? 'md' : ext === 'excalidraw' ? 'excalidraw' : ext === 'mermaid' ? 'mermaid' : ext === 'yml' || ext === 'yaml' ? 'yaml' : 'text';
}

export function EditorView() {
  const nav = useNavigate();
  const app = useApp();
  const { '*': splat } = useParams();
  // "~<repoId>/<path>" targets a read-only input repo (default branch)
  const raw0 = splat || DEFAULT_DOC;
  const roMatch = raw0.match(/^~([\w-]+)\/(.+)$/);
  const readOnly = !!roMatch;
  const fileRepo = roMatch ? roMatch[1] : app.repoId;
  const fileRef = roMatch ? '' : app.branch;
  const path = roMatch ? roMatch[2] : raw0;
  const name = path.split('/').pop()!;
  const kind = kindOf(name);
  const file = useFileQuery(fileRepo, fileRef, path);
  const save = useSaveFile(app.repoId, app.branch); // sketch-file creation
  const toasts = useToasts();
  const narrow = useNarrow();
  const { ensureWritableBranch } = useWorkspace();
  // documents open read-only by default; editing is an explicit mode
  const [mode, setMode] = useState<'view' | 'edit' | 'source'>('view');
  const [propsOpen, setPropsOpen] = useState(true);
  const [outlineOpen, setOutlineOpen] = useState(false);
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
    // md edit mode is room-driven (the collab session owns persistence);
    // source mode and non-md files keep the PUT autosave path
    enabled: !readOnly && !app.isProtectedBranch && !(mode === 'edit' && kind === 'md'),
    onRecovered: () => toasts.push({ text: `Recovered unsaved changes for ${name}`, kind: 'info' }),
    beforePersist: () => {
      const fresh = editorApi.current?.flush();
      if (fresh == null) return null;
      const nl = fresh.endsWith('\n') ? fresh : fresh + '\n';
      const curFm = stripFrontmatter(rawRef.current).fm;
      return curFm ? assemble(curFm, '\n' + nl) : nl;
    },
  });
  rawRef.current = draft.raw;
  const conflict = syncState === 'conflict';
  // committed baseline for the source-mode changed-line gutter
  const headBaseline = useFileAtHead(fileRepo, app.branch, path, mode === 'source' && !readOnly && !app.isProtectedBranch);

  // ---- real-time co-editing (markdown, edit mode, writable branch) ----
  const me = useMe();
  // source mode leaves the CRDT room — while others are still live in it the
  // room owns the file (PUTs 409), so source becomes read-only
  const presence = usePresence(mode === 'source' && !readOnly ? fileRepo : undefined);
  const othersInRoom = mode === 'source' && (presence.data || []).some(
    (r) => r.branch === app.branch && r.path === path && !r.orphaned &&
      r.users.some((u) => u.userId !== (me.data?.id ?? -1)),
  );
  const collabEligible = mode === 'edit' && kind === 'md' && !readOnly && !app.isProtectedBranch;
  // note: no !file.isFetching here — flush acks invalidate the file query,
  // and dropping the session on every refetch remounts the editor/toolbar
  const session = useCollabSession({
    enabled: collabEligible && !!file.data,
    repo: fileRepo,
    branch: app.branch,
    path,
    baseSha: file.data?.sha,
    initialFm: file.data ? stripFrontmatter(file.data.content).fm : '',
    me: me.data ? { id: me.data.id, name: me.data.name } : undefined,
  });
  const collabReady = session !== null && (session.status === 'synced' || session.status === 'seeding');
  // room flushes write to the worktree outside the PUT path — refresh the
  // dirty-files status when a flush ack lands
  const qc = useQueryClient();
  // the session serializes through the live editor for flushes
  useEffect(() => {
    if (!session) return;
    session.setSerializer(() => editorApi.current?.serialize() ?? null);
    // flush acks refresh git status + the file query (view/source read it);
    // deliberately NOT cleared on unmount — the final flush acks after the
    // view releases the session
    session.onFlushed = () => {
      qc.invalidateQueries({ queryKey: ['status', fileRepo, app.branch] });
      qc.invalidateQueries({ queryKey: ['file', fileRepo, app.branch, path] });
    };
    return () => session.setSerializer(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session]);
  // frontmatter lives in the room's Y.Map while a session is active
  const [fmGen, setFmGen] = useState(0);
  useEffect(() => {
    if (!session) return;
    return session.onFmChange(() => setFmGen((g) => g + 1));
  }, [session]);
  const collabFm = session && fmGen >= 0 ? session.getFm() : '';

  // invite links deep-link into a live document on another branch
  const [searchParams, setSearchParams] = useSearchParams();
  useEffect(() => {
    const inviteBranch = searchParams.get('branch');
    if (!searchParams.has('invite') || !inviteBranch) return;
    setSearchParams({}, { replace: true });
    if (inviteBranch === app.branch) {
      void enterEdit();
      return;
    }
    toasts.push({
      text: `You've been invited to co-edit ${name} on ${inviteBranch}`,
      kind: 'info',
      duration: 15_000,
      action: {
        label: 'Switch & join',
        onClick: () => {
          app.switchBranch(inviteBranch, { carryDraft: true });
          setTimeout(() => void enterEdit(), 300);
        },
      },
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams]);

  const { fm, body } = useMemo(() => stripFrontmatter(draft.raw), [draft.raw]);
  const title = kind === 'md' ? scalar(fm, 'title') || name : name;
  const status = kind === 'md' ? scalar(fm, 'status') : '';

  const openPath = useCallback((rel: string) => {
    const dir = path.split('/').slice(0, -1).join('/');
    nav('/editor/' + (rel.includes('/') && !rel.startsWith('.') ? rel : resolvePath(dir, rel)));
  }, [nav, path]);

  const onBodyChange = useCallback((md: string) => {
    const curFm = stripFrontmatter(rawRef.current).fm;
    const nl = md.endsWith('\n') ? md : md + '\n';
    setRaw(curFm ? assemble(curFm, '\n' + nl) : nl);
  }, [setRaw]);
  const onFmChange = useCallback((nextFm: string) => {
    setRaw(assemble(nextFm, stripFrontmatter(rawRef.current).body));
  }, [setRaw]);
  const onRawChange = useCallback((raw: string) => {
    // typing in source mode on a protected branch triggers the workspace
    // switch; the dirty draft is carried onto the new branch
    if (app.isProtectedBranch && !readOnly) void ensureWritableBranch();
    setRaw(raw);
  }, [setRaw, app.isProtectedBranch, readOnly, ensureWritableBranch]);

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

  const change = app.model?.changes.find((c) => c.status === 'triage');
  const tseg = (on: boolean) => (on ? 'background:var(--surface);box-shadow:var(--shadow);color:var(--text)' : 'color:var(--text-3)');
  // ready only when the draft belongs to *this* path — during a file switch
  // the draft briefly still holds the previous document
  const ready = !!file.data && draft.path === path && draft.raw !== '';
  const editable = kind === 'md' && !readOnly;
  // a persisted 'edit' choice degrades gracefully on files that can't be edited
  const effMode = mode === 'edit' && !editable ? 'view' : mode;

  // outline: h1-h3 headings for the sticky TOC (code fences skipped)
  const outline = useMemo(() => {
    if (kind !== 'md') return [];
    const out: { level: number; text: string }[] = [];
    let fence = false;
    for (const line of body.split('\n')) {
      if (/^```/.test(line.trim())) { fence = !fence; continue; }
      if (fence) continue;
      const m = line.match(/^(#{1,3})[ \t]+(.+?)\s*$/);
      if (m) out.push({ level: m[1].length, text: m[2].replace(/\s*\{#[\w-]+\}\s*$/, '') });
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
      {!narrow && docTabsStrip('editor', name, nav, draft.dirty)}
      <div style={sx('height:40px;flex:none;display:flex;align-items:center;gap:' + (narrow ? '8px' : '12px') + ';padding:0 ' + (narrow ? '10px' : '16px') + ';background:var(--surface);border-bottom:1px solid var(--border);' + (narrow ? 'overflow-x:auto;overflow-y:hidden' : ''))}>
        <div style={sx("display:flex;align-items:center;gap:6px;font-family:'IBM Plex Mono',monospace;font-size:11.5px;color:var(--text-2);min-width:30px;overflow:hidden")}>
          <span style={sx('color:var(--text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{path}</span>
          {draft.dirty && <span title="unsaved changes" style={sx('flex:none;width:6px;height:6px;border-radius:50%;background:var(--reg)')} />}
        </div>
        <div style={sx('flex:1')} />
        {effMode === 'edit' && (
          <>
            <span style={sx('flex:none;display:inline-flex;border:1px solid var(--border-2);border-radius:7px;overflow:hidden')}>
              {([['strong', 'B', 'Bold (Ctrl+B)', 'font-weight:800'], ['em', 'I', 'Italic (Ctrl+I)', 'font-style:italic'], ['strike', 'S', 'Strikethrough', 'text-decoration:line-through'], ['code', '‹›', 'Inline code', "font-family:'IBM Plex Mono',monospace;font-size:10.5px"]] as const).map(([mark, label, title, style]) => (
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
        {collabEligible && session && (
          <>
            {/* who's here */}
            <span style={sx('display:inline-flex;align-items:center')}>
              {session.peers.map((p, i) => (
                <span key={p.connId} title={p.name}
                  style={{ ...sx('width:22px;height:22px;border-radius:50%;display:inline-flex;align-items:center;justify-content:center;color:#fff;font-size:9.5px;font-weight:700;border:2px solid var(--surface)'), background: userColor(p.userId), marginLeft: i === 0 ? 0 : -6 }}>
                  {p.name.split(/[\s._-]+/).slice(0, 2).map((w) => w[0]).join('')}
                </span>
              ))}
            </span>
            <button
              onClick={() => {
                const link = `${location.origin}/#/editor/${path}?branch=${encodeURIComponent(app.branch)}&invite=1`;
                void navigator.clipboard.writeText(link);
                toasts.push({ text: 'Invite link copied — anyone opening it joins this document live', kind: 'success' });
              }}
              title="Invite someone to co-edit this document"
              style={sx('flex:none;display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--ai-line);border-radius:7px;background:var(--ai-bg);color:var(--ai);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer')}>
              <IconUserPlus /> Invite
            </button>
            <span data-sync={session.dirty ? 'saving' : session.savedSha ? 'saved' : 'clean'}
              style={sx("flex:none;display:inline-flex;align-items:center;gap:5px;font-size:11.5px;font-family:'IBM Plex Mono',monospace;min-width:64px;" +
                (session.status === 'offline' ? 'color:var(--del)' : session.dirty ? 'color:var(--text-3)' : 'color:var(--data)'))}>
              {session.status === 'offline' ? 'offline — reconnecting'
                : session.status === 'error' ? session.errorMsg
                : session.dirty ? 'Saving…'
                : session.savedSha ? 'Saved ✓' : 'live'}
            </span>
          </>
        )}
        {!readOnly && !(collabEligible && session) && syncState !== 'clean' && (
          <span data-sync={syncState} style={sx("display:inline-flex;align-items:center;gap:5px;font-size:11.5px;font-family:'IBM Plex Mono',monospace;" +
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
          <span onClick={() => setMode('source')} style={sx('padding:3px 12px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;' + tseg(effMode === 'source'))}>Source</span>
          <span style={sx('padding:3px 12px;border-radius:6px;font-size:12px;color:var(--text-3)')}>History</span>
        </div>
        <span style={sx('width:1px;height:20px;background:var(--border)')} />
        <button style={sx('flex:none;display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer')}>
          <IconShare />Share
        </button>
      </div>

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
            {outlineOpen && (
              <div data-outline-list style={sx('margin-top:6px;width:210px;padding:8px 6px;background:var(--surface);border:1px solid var(--border);border-radius:10px;box-shadow:var(--shadow-lg);max-height:62vh;overflow-y:auto')}>
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
            <div style={sx("font-family:'IBM Plex Mono',monospace;font-size:11px;color:var(--text-3)")}>rendering {path}…</div>
          </div>
        )}
        {file.error != null && (
          <div style={sx('max-width:820px;margin:0 auto;padding:16px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:10px;color:var(--reg);font-size:13px')}>
            Couldn't load {path}: {String((file.error as Error).message || file.error)}
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
                  <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:10.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Properties</span>
                  <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:10.5px;color:var(--text-3)")}>· {viewProps.length} fields</span>
                </div>
                {propsOpen && viewProps.map((p) => (
                  <div key={p.key} style={sx('display:flex;gap:14px;padding:8px 14px;border-top:1px solid var(--border)')}>
                    <span style={sx("width:132px;flex:none;font-family:'IBM Plex Mono',monospace;font-size:11px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.3px;padding-top:2px")}>{p.key}</span>
                    <div style={sx('flex:1;display:flex;flex-wrap:wrap;gap:6px;align-items:center;min-width:0')}>
                      {p.items.map((it, i) => (
                        <span key={i} onClick={it.openPath ? () => nav('/editor/' + it.openPath) : undefined} style={sx(it.style)}>{it.text}</span>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}
            {kind === 'excalidraw' && !readOnly ? (
              <div
                onClick={() => void ensureWritableBranch().then(() => setExcalidrawPath(path))}
                title="Click to edit the sketch"
                style={sx('cursor:pointer')}
              >
                <DocBody html={viewHtml} docPath={path} />
              </div>
            ) : (
              <DocBody html={viewHtml} docPath={path} />
            )}
            {app.aiSuggestions && change && kind === 'md' && !readOnly && (
              <div style={sx('margin-top:24px;border:1px solid var(--ai-line);border-radius:10px;overflow:hidden;background:var(--surface);box-shadow:var(--shadow)')}>
                <div style={sx('display:flex;align-items:center;gap:9px;padding:10px 14px;background:var(--ai-bg);border-bottom:1px solid var(--ai-line)')}>
                  <IconSpark size={14} stroke="var(--ai)" />
                  <span style={sx('font-size:12px;font-weight:600;color:var(--ai)')}>Copilot suggests an edit</span>
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

        {/* ---- Edit: WYSIWYG + properties form (room-driven when collab) ---- */}
        {effMode === 'edit' && ready && editable && (!collabEligible || collabReady) && (
          <div style={sx('max-width:820px;margin:0 auto')}>
            {(session ? collabFm : fm) && (
              <div style={sx('margin:0 0 30px;border:1px solid var(--border);border-radius:10px;overflow:hidden;background:var(--surface)')}>
                <div onClick={() => setPropsOpen((v) => !v)} style={sx('display:flex;align-items:center;gap:8px;padding:8px 14px;background:var(--surface-2);cursor:pointer;user-select:none')}>
                  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" strokeWidth="2.6" style={{ transform: propsOpen ? 'rotate(90deg)' : 'rotate(0deg)', transition: 'transform .15s' }}>
                    <path d="M9 6l6 6-6 6" />
                  </svg>
                  <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:10.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>Properties</span>
                  <span style={sx("font-family:'IBM Plex Mono',monospace;font-size:10.5px;color:var(--text-3)")}>· editable</span>
                </div>
                {propsOpen && (
                  <PropertiesForm
                    fm={session ? collabFm : fm}
                    schema={app.schema}
                    files={app.files}
                    onChange={session ? (nextFm) => session.setFm(nextFm) : onFmChange}
                    onOpenPath={openPath}
                  />
                )}
              </div>
            )}
            <MilkdownEditor
              key={path + ':' + (session ? 'room:' + sketchGen : draft.gen + ':' + sketchGen) + ':' + app.theme + (conflict ? ':c' : '')}
              body={session && file.data ? stripFrontmatter(file.data.content).body : body}
              docPath={path}
              files={app.files}
              collab={session ? { doc: session.doc, awareness: session.awareness, seedGranted: session.seedGranted } : undefined}
              // collab: the room owns persistence — routing serialized text
              // into the draft would create a second (stale) source of truth
              // that blocks adopting flushed content after the room closes
              onChange={session ? () => {} : onBodyChange}
              onDirty={session ? () => session.markUserEdited() : markDirty}
              onOpenPath={openPath}
              onOpenExcalidraw={setExcalidrawPath}
              onReady={(api) => { editorApi.current = api; }}
              onCollabTeardown={session ? (md) => session.flushSerialized(md) : undefined}
              resolveAsset={resolveAsset}
              onUploadImage={uploadImage}
              onRequestImage={() => imagePicker.current?.click()}
              onRequestSketch={() => void insertSketch()}
            />
            {collabEligible && session && session.peers.length > 1 && (
              <div style={sx("margin-top:10px;font-size:11px;color:var(--text-3);font-family:'IBM Plex Mono',monospace")}>
                co-editing live with {session.peers.filter((p) => p.name !== me.data?.name).map((p) => p.name).join(', ')} — everyone lands as Co-authored-by on commit
              </div>
            )}
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
                      style={sx("display:inline-flex;align-items:center;gap:5px;padding:4px 10px;border:1px solid var(--border);border-radius:20px;font-size:11.5px;color:var(--prod);cursor:pointer;background:var(--surface-2);font-family:'IBM Plex Mono',monospace")}
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
            {othersInRoom && (
              <div style={sx('margin-bottom:10px;padding:8px 12px;border:1px solid var(--border-2);border-radius:9px;font-size:12px;color:var(--text-2);background:var(--surface)')}>
                Source is read-only while others are co-editing this file live — switch to <b>Edit</b> to collaborate.
              </div>
            )}
            <SourceEditor
              value={draft.raw}
              lang={kind === 'md' ? 'markdown' : kind === 'yaml' ? 'yaml' : 'text'}
              onChange={onRawChange}
              readOnly={readOnly || othersInRoom}
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
