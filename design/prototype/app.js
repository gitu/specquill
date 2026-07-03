// app.js — reqbase UI. Vanilla-JS implementation of design/Reqbase.dc.html.
// The whole screen is a pure function of state; every setState() re-renders,
// preserving scroll positions of containers tagged with data-sk.
import * as R from './repo-render.js';

const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

const ICONS = {
  branch: '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="var(--text-2)" stroke-width="2"><circle cx="6" cy="6" r="2.4"/><circle cx="6" cy="18" r="2.4"/><circle cx="18" cy="9" r="2.4"/><path d="M6 8.4v7.2M8.3 7.3l7.5 1.4M18 11.4c0 4-6 2.6-6 6.6"/></svg>',
  search: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/></svg>',
  pr: '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="var(--prod)" stroke-width="2"><circle cx="6" cy="6" r="2.4"/><circle cx="6" cy="18" r="2.4"/><circle cx="18" cy="18" r="2.4"/><path d="M6 8.4v7.2M18 15.6c0-5-12-2-12-9.6"/></svg>',
  dash: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3.5" y="3.5" width="7.5" height="7.5" rx="1.6"/><rect x="13" y="3.5" width="7.5" height="4.5" rx="1.6"/><rect x="13" y="11" width="7.5" height="9.5" rx="1.6"/><rect x="3.5" y="14" width="7.5" height="6.5" rx="1.6"/></svg>',
  folder: '<svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 5.5A1.5 1.5 0 015.5 4h4l2 2.5h7A1.5 1.5 0 0120 8v9.5a1.5 1.5 0 01-1.5 1.5h-13A1.5 1.5 0 014 17.5z"/></svg>',
  changes: '<svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 12h6M14 12h6M8 8l-4 4 4 4M16 16l4-4-4-4"/></svg>',
  trace: '<svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="6" cy="6" r="2.3"/><circle cx="18" cy="6" r="2.3"/><circle cx="12" cy="18" r="2.3"/><path d="M7.5 7.4l3.3 8.4M16.5 7.4l-3.3 8.4M8 6h8"/></svg>',
  traceSm: '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9"><circle cx="6" cy="6" r="2.3"/><circle cx="18" cy="6" r="2.3"/><circle cx="12" cy="18" r="2.3"/><path d="M7.5 7.4l3.3 8.4M16.5 7.4l-3.3 8.4M8 6h8"/></svg>',
  matrix: '<svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="4" width="18" height="16" rx="1.6"/><path d="M3 9h18M3 14h18M9 4v16M15 4v16"/></svg>',
  model: '<svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 3l8 4.5v9L12 21l-8-4.5v-9z"/><path d="M12 3v18M4 7.5l8 4.5 8-4.5"/></svg>',
  spark: (w) => `<svg width="${w}" height="${w}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/></svg>`,
  sparkAI: (w) => `<svg width="${w}" height="${w}" viewBox="0 0 24 24" fill="none" stroke="var(--ai)" stroke-width="1.8"><path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/></svg>`,
  gear: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M19 5l-2 2M7 17l-2 2"/></svg>',
  chevR: '<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="M8 6l6 6-6 6"/></svg>',
  chevD: '<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" style="flex:none"><path d="M6 9l6 6 6-6"/></svg>',
  plus: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 5v14M5 12h14"/></svg>',
  sync: '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 11a8 8 0 10-2.3 5.7M20 5v6h-6"/></svg>',
  close: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="opacity:.6"><path d="M6 6l12 12M18 6L6 18"/></svg>',
  share: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 12v7a1 1 0 001 1h14a1 1 0 001-1v-7M16 6l-4-4-4 4M12 2v14"/></svg>',
  up: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 19V5M6 11l6-6 6 6"/></svg>',
  down: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-left:2px"><path d="M12 5v14M6 13l6 6 6-6"/></svg>',
  send: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M12 19V5M6 11l6-6 6 6"/></svg>',
  arrow: '<svg width="20" height="12" viewBox="0 0 20 12" fill="none" stroke="var(--text-3)" stroke-width="1.5"><path d="M1 6h16M13 2l4 4-4 4"/></svg>',
};

class App {
  constructor(root) {
    this.root = root;
    this.actions = [];
    const qs = new URLSearchParams(location.search);
    this.state = {
      view: 'editor', docMode: 'preview', propsOpen: true, matrixDense: true,
      theme: qs.get('theme') || localStorage.getItem('reqbase-theme') || 'light',
      copilotOpen: qs.has('copilot') ? qs.get('copilot') !== '0' : localStorage.getItem('reqbase-copilot') !== '0',
      aiSuggestions: true,
    };
    Object.assign(this.state, this.parseHash());
    root.addEventListener('click', (e) => {
      const el = e.target.closest('[data-a]');
      if (el) { const fn = this.actions[+el.dataset.a]; if (fn) { e.preventDefault(); fn(e); } }
    });
    window.addEventListener('hashchange', () => {
      const h = this.parseHash();
      if (h.view === 'editor' && h.openFile && h.openFile !== this.state.openFile) this.openDoc(h.openFile);
      else if (h.view && h.view !== this.state.view) this.setState(h);
    });
    this.render();
    this.loadRepo();
  }

  // #/<view> or #/editor/<path> — deep-linkable views
  parseHash() {
    const m = location.hash.match(/^#\/([\w-]+)(?:\/(.+))?$/);
    if (!m) return {};
    const view = ['dashboard', 'editor', 'changes', 'graph', 'matrix', 'model', 'diff'].includes(m[1]) ? m[1] : null;
    if (!view) return {};
    const out = { view };
    if (view === 'editor' && m[2]) out.openFile = decodeURI(m[2]);
    return out;
  }
  syncHash() {
    const s = this.state;
    const h = s.view === 'editor' && s.openFile ? '#/editor/' + encodeURI(s.openFile) : '#/' + s.view;
    if (location.hash !== h) history.replaceState(null, '', h);
  }

  setState(patch) {
    if (typeof patch === 'function') patch = patch(this.state);
    Object.assign(this.state, patch);
    this.render();
  }

  on(fn) { return fn ? `data-a="${this.actions.push(fn) - 1}"` : ''; }

  applyTheme() {
    document.body.setAttribute('data-theme', this.state.theme || 'light');
  }

  // ---------------------------------------------------------------- data
  async loadRepo() {
    try {
      this._excalidraw = await (await fetch('repo/diagrams/data-flow.excalidraw')).text();
      try { this._schema = await (await fetch('repo/.reqbase/schema.json')).json(); } catch (e) { this._schema = null; }
      try { this._configYml = await (await fetch('repo/.reqbase/config.yml')).text(); } catch (e) { this._configYml = ''; }
      const paths = [
        'regulations/gdpr.md', 'regulations/mifid-ii.md', 'regulations/dora.md',
        'requirements/REQ-042.md', 'requirements/REQ-051.md', 'requirements/REQ-063.md',
        'requirements/REQ-070.md', 'requirements/REQ-090.md', 'requirements/REQ-095.md',
        'specs/txn-report.md', 'specs/venue.md',
        'data-mappings/trade.md', 'data-mappings/customer.md',
        'changes/2026-06-mifid-rts22.md', 'changes/2026-06-partial-fills.md',
        'changes/2026-06-oms-v4.md', 'changes/2026-05-dora-incident.md',
      ];
      const pairs = await Promise.all(paths.map(async (p) => [p, await (await fetch('repo/' + p)).text()]));
      const files = Object.fromEntries(pairs);
      this._rawFiles = files;
      const model = R.buildModel(files);
      const chgRaw = files['changes/2026-06-mifid-rts22.md'];
      const chg = R.stripFrontmatter(chgRaw);
      this.setState({
        model,
        change: {
          summary: R.foldScalar(chg.fm, 'ai_summary'),
          reference: R.scalar(chg.fm, 'reference'),
          published: R.scalar(chg.fm, 'published'),
          diff: R.extractDiff(chgRaw),
        },
      });
      this.openDoc(this.state.openFile || 'specs/txn-report.md', this.state.view !== 'editor');
    } catch (e) { this.setState({ docErr: String(e && e.message || e) }); }
  }

  async openDoc(path, stay) {
    this.setState(stay ? { openFile: path, doc: null, docErr: '' } : { view: 'editor', openFile: path, doc: null, docErr: '' });
    try {
      const raw = await (await fetch('repo/' + path)).text();
      const name = path.split('/').pop();
      const ext = name.split('.').pop();
      const kind = ext === 'md' ? 'md' : ext === 'excalidraw' ? 'excalidraw' : ext === 'mermaid' ? 'mermaid' : (ext === 'yml' || ext === 'yaml') ? 'yaml' : 'text';
      const doc = { path, name, kind, raw, title: name, status: '', owner: '', id: '', drivers: [], bodyHtml: '' };
      if (kind === 'md') {
        const { fm, body } = R.stripFrontmatter(raw);
        doc.fm = fm;
        doc.title = R.scalar(fm, 'title') || name;
        doc.status = R.scalar(fm, 'status');
        doc.owner = R.scalar(fm, 'owner');
        doc.id = R.scalar(fm, 'id');
        doc.drivers = R.drivers(fm);
        const b = body.replace(/^\s*#\s+.+\n+/, '').replace(/\s*\{#[\w-]+\}\s*$/gm, '');
        doc.bodyHtml = window.marked ? window.marked.parse(b) : '<pre>' + esc(b) + '</pre>';
      } else if (kind === 'mermaid') {
        doc.bodyHtml = '<pre><code class="language-mermaid">' + esc(raw.replace(/^%%.*\n/, '')) + '</code></pre>';
      } else if (kind === 'excalidraw') {
        this._openExcalidraw = raw;
        doc.bodyHtml = '<div data-excalidraw="1"></div>';
      } else {
        doc.bodyHtml = '<pre style="white-space:pre-wrap"><code>' + esc(raw) + '</code></pre>';
      }
      this.setState({ doc });
    } catch (e) { this.setState({ docErr: String(e && e.message || e) }); }
  }

  hydrateDoc() {
    const host = document.getElementById('reqbase-doc');
    if (!host || host.dataset.hydrated) return;
    host.dataset.hydrated = '1';
    const dark = (this.state.theme || 'light') === 'dark';
    // mermaid fenced blocks
    const codes = host.querySelectorAll('code.language-mermaid');
    if (codes.length && window.mermaid) {
      try { window.mermaid.initialize({ startOnLoad: false, theme: dark ? 'dark' : 'neutral', securityLevel: 'loose', fontFamily: "'IBM Plex Sans',sans-serif" }); } catch (e) {}
      codes.forEach((c, i) => {
        const src = c.textContent;
        window.mermaid.render('mmd-' + Date.now() + '-' + i, src).then((r) => {
          const pre = c.closest('pre') || c;
          const div = document.createElement('div');
          div.style.cssText = 'text-align:center;padding:8px 0;margin:16px 0';
          div.innerHTML = r.svg;
          pre.replaceWith(div);
        }).catch(() => {});
      });
    }
    // excalidraw — both markdown embeds and directly-opened .excalidraw files
    const cmap = { '#1a1e24': 'var(--text)', '#2563c9': 'var(--prod)', '#e5edfb': 'var(--prod-bg)', '#12876a': 'var(--data)', '#daf0e8': 'var(--data-bg)', '#5a616b': 'var(--text-2)', '#b06f16': 'var(--reg)', '#ffffff': 'var(--surface)', 'transparent': 'transparent' };
    const drawExc = (raw, el) => {
      if (!raw) return;
      try {
        const box = document.createElement('div');
        box.style.cssText = 'border:1px solid var(--border);border-radius:10px;background:var(--surface);padding:14px 12px;margin:16px 0';
        box.innerHTML = R.excalidrawToSvg(JSON.parse(raw), cmap);
        el.replaceWith(box);
      } catch (e) {}
    };
    host.querySelectorAll('img').forEach((img) => { if (/\.excalidraw$/.test(img.getAttribute('src') || '')) drawExc(this._excalidraw, img); });
    const embed = host.querySelector('[data-excalidraw]');
    if (embed) drawExc(this._openExcalidraw, embed);
    // internal links → open that file in the editor
    const dir = ((this.state.doc && this.state.doc.path) || '').split('/').slice(0, -1).join('/');
    host.querySelectorAll('a[href]').forEach((a) => {
      const href = a.getAttribute('href') || '';
      if (/^(https?:|#|mailto:)/.test(href) || !/\.(md|excalidraw|mermaid|ya?ml)(#|$)/.test(href)) return;
      const target = R.resolvePath(dir, href.split('#')[0]);
      a.style.cursor = 'pointer';
      a.addEventListener('click', (e) => { e.preventDefault(); this.openDoc(target); });
    });
  }

  // ---------------------------------------------------------------- model → vals
  buildTree() {
    const M = this.state.model;
    const fmeta = {
      regulations: { icon: '◈', color: 'var(--reg)' },
      requirements: { icon: '▤', color: 'var(--prod)' },
      specs: { icon: '◈', color: 'var(--text-2)' },
      'data-mappings': { icon: '⇄', color: 'var(--data)' },
      diagrams: { icon: '✎', color: 'var(--ai)' },
      changes: { icon: '⚑', color: 'var(--reg)' },
    };
    const order = ['regulations', 'requirements', 'specs', 'data-mappings', 'diagrams', 'changes'];
    const paths = M ? [...M.regs, ...M.requirements, ...M.specs, ...M.maps, ...M.changes].map((x) => x.path) : [];
    paths.push('diagrams/data-flow.excalidraw', 'diagrams/reporting.mermaid');
    const byFolder = {};
    paths.forEach((p) => { const f = p.split('/')[0]; (byFolder[f] = byFolder[f] || []).push(p); });
    const modified = { 'regulations/mifid-ii.md': 'M', 'requirements/REQ-042.md': 'M', 'specs/txn-report.md': 'M', 'data-mappings/trade.md': 'M', 'requirements/REQ-063.md': 'A' };
    this._nModified = Object.keys(modified).length;
    const open = this.state.openFile;
    return order.filter((f) => byFolder[f]).map((folder) => ({
      name: folder,
      files: byFolder[folder].sort().map((path) => {
        const n = path.split('/').pop();
        const active = path === open;
        const badge = modified[path] || '';
        return {
          path, name: n,
          icon: folder === 'diagrams' ? (n.endsWith('.mermaid') ? '⌗' : '✎') : fmeta[folder].icon,
          color: fmeta[folder].color,
          rowStyle: active ? 'background:var(--surface);box-shadow:var(--shadow);font-weight:600;color:var(--text)' : 'color:var(--text-2)',
          badge,
          badgeStyle: badge === 'A' ? 'color:var(--add)' : 'color:var(--reg)',
          open: () => this.openDoc(path),
        };
      }),
    }));
  }

  driverChip(t) {
    return {
      regulatory: { icon: '⚖', label: 'Regulatory', s: 'background:var(--reg-bg);color:var(--reg)' },
      product: { icon: '◆', label: 'Product', s: 'background:var(--prod-bg);color:var(--prod)' },
      technical: { icon: '⚙', label: 'Technical', s: 'background:var(--surface-2);color:var(--text-2)' },
    }[t] || { icon: '•', label: t, s: 'background:var(--surface-2);color:var(--text-2)' };
  }

  buildProps(fm) {
    if (!fm) return [];
    const schema = this._schema || { fields: {}, order: [], colors: {} };
    const pal = { green: { fg: 'var(--data)', bg: 'var(--data-bg)' }, amber: { fg: 'var(--reg)', bg: 'var(--reg-bg)' }, blue: { fg: 'var(--prod)', bg: 'var(--prod-bg)' }, violet: { fg: 'var(--ai)', bg: 'var(--ai-bg)' }, slate: { fg: 'var(--text-2)', bg: 'var(--surface-2)' } };
    const entries = R.parseProps(fm);
    const byKey = {}; entries.forEach((e) => { byKey[e.key] = e; });
    const order = schema.order || [];
    const keys = [...order.filter((k) => byKey[k]), ...entries.map((e) => e.key).filter((k) => order.indexOf(k) < 0)].filter((k) => k !== 'title');
    const chip = (bg, fg, mono, cap) => 'display:inline-flex;align-items:center;padding:2px 9px;border-radius:6px;font-size:11.5px;' + (mono ? "font-family:'IBM Plex Mono',monospace;" : '') + (cap ? 'text-transform:capitalize;' : '') + 'background:' + bg + ';color:' + fg;
    const badge = (c) => 'display:inline-flex;align-items:center;padding:2px 10px;border-radius:20px;font-size:11.5px;font-weight:600;text-transform:capitalize;background:' + c.bg + ';color:' + c.fg;
    const linkStyle = "color:var(--prod);cursor:pointer;text-decoration:underline;text-decoration-color:var(--prod-line);font-family:'IBM Plex Mono',monospace;font-size:12px";
    const linkItem = (t) => {
      const pm = String(t).match(/([\w-]+\/[\w.\/-]+\.(?:md|excalidraw|mermaid))/);
      if (pm) { const path = pm[1]; return { text: t, style: linkStyle, open: () => this.openDoc(path) }; }
      return { text: t, style: chip('var(--surface-2)', 'var(--text-2)', true), open: undefined };
    };
    return keys.map((key) => {
      const e = byKey[key];
      const def = (schema.fields || {})[key] || {};
      const label = def.label || key.replace(/_/g, ' ');
      const type = def.type;
      let items;
      if (e.type === 'scalar') {
        const v = e.value;
        if (type === 'enum') { const cn = (def.values || {})[String(v).toLowerCase()] || 'slate'; items = [{ text: v, style: badge(pal[cn] || pal.slate), open: undefined }]; }
        else if (type === 'percent') { const n = parseFloat(v) || 0; const pct = Math.round(n <= 1 ? n * 100 : n); const c = pct > 80 ? 'var(--data)' : pct > 60 ? 'var(--prod)' : 'var(--reg)'; items = [{ text: pct + '%', style: 'display:inline-flex;padding:2px 10px;border-radius:20px;font-size:11.5px;font-weight:600;background:var(--surface-2);color:' + c, open: undefined }]; }
        else if (type === 'user') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text)', true), open: undefined }];
        else if (type === 'code') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text-2)', true), open: undefined }];
        else if (type === 'tag') items = [{ text: v, style: chip('var(--surface-2)', 'var(--text-2)', false, true), open: undefined }];
        else if (type === 'date') items = [{ text: v, style: "font-family:'IBM Plex Mono',monospace;font-size:11.5px;color:var(--text-2)", open: undefined }];
        else items = [{ text: v, style: 'font-size:13px;color:var(--text);line-height:1.5', open: undefined }];
      } else {
        items = e.items.map((it) => (type === 'code' || type === 'anchors') ? { text: it, style: chip('var(--surface-2)', 'var(--text-2)', true), open: undefined } : linkItem(it));
      }
      return { key: label, items };
    });
  }

  daysAgo(d) {
    if (!d) return '';
    const then = new Date(d + 'T00:00:00'), now = new Date();
    const n = Math.round((now - then) / 86400000);
    if (n <= 0) return 'today';
    if (n < 7) return n + 'd';
    if (n < 30) return Math.round(n / 7) + 'w';
    return Math.round(n / 30) + 'mo';
  }
  srcMeta(s) {
    return { regulatory: { icon: '⚖', label: 'Regulatory', fg: 'var(--reg)', bg: 'var(--reg-bg)' }, product: { icon: '◆', label: 'Product', fg: 'var(--prod)', bg: 'var(--prod-bg)' }, technical: { icon: '⚙', label: 'Technical', fg: 'var(--text-2)', bg: 'var(--surface-2)' } }[s] || { icon: '•', label: s || 'Change', fg: 'var(--text-2)', bg: 'var(--surface-2)' };
  }
  statusMeta(s) {
    const v = String(s || '').toLowerCase();
    const m = { triage: ['Triage', 'var(--reg)'], in_progress: ['In progress', 'var(--prod)'], auto_remapped: ['Auto-remapped', 'var(--data)'], backlog: ['Backlog', 'var(--text-3)'], done: ['Done', 'var(--data)'], merged: ['Merged', 'var(--data)'] }[v];
    return m ? { label: m[0], color: m[1] } : { label: (s || '').replace(/_/g, ' '), color: 'var(--text-2)' };
  }

  buildChanges() {
    const M = this.state.model; if (!M) return { items: [], sel: null, counts: {} };
    const filter = this.state.changeFilter || 'all';
    const order = { triage: 0, in_progress: 1, auto_remapped: 2, backlog: 3 };
    const all = M.changes.map((c) => {
      const nImp = c.impReqs.length + c.impSpecs.length + c.impMaps.length;
      return { path: c.path, title: c.title, source: c.source, summary: c.summary, status: c.status, published: c.published,
        ago: this.daysAgo(c.published), reqs: c.impReqs, specs: c.impSpecs, maps: c.impMaps, diff: c.diff, nImpacted: nImp };
    }).sort((a, b) => (order[a.status] ?? 9) - (order[b.status] ?? 9));
    const counts = { all: all.length, regulatory: 0, product: 0, technical: 0 };
    all.forEach((c) => { if (counts[c.source] != null) counts[c.source]++; });
    const items = all.filter((c) => filter === 'all' || c.source === filter);
    const selPath = this.state.selChange || (items[0] && items[0].path);
    const sel = all.find((c) => c.path === selPath) || items[0] || null;
    return { items, sel, counts };
  }

  buildDashboard() {
    const M = this.state.model; if (!M) return null;
    const openChanges = M.changes.filter((c) => c.status !== 'done' && c.status !== 'merged');
    const bySource = { regulatory: 0, product: 0, technical: 0 };
    openChanges.forEach((c) => { if (bySource[c.source] != null) bySource[c.source]++; });
    const drifts = M.fields.filter((f) => f.drift).length;
    const covVals = M.requirements.map((r) => r.coverage).filter((n) => n > 0);
    const cov = covVals.length ? Math.round((covVals.reduce((a, b) => a + b, 0) / covVals.length) * 100) : 0;
    // link-health ratios
    const reqWithDriver = M.requirements.filter((r) => r.drivers.length).length;
    const reqWithSpec = M.requirements.filter((r) => r.implements.some((p) => p.startsWith('specs/'))).length;
    const specWithField = M.specs.filter((s) => s.maps_to.length).length;
    const pct = (a, b) => b ? Math.round((a / b) * 100) : 0;
    return {
      openCount: openChanges.length, bySource, specCount: M.specs.length, reqCount: M.requirements.length,
      drifts, cov,
      feed: this.buildChanges().items.slice(0, 3).map((c) => {
        const m = this.srcMeta(c.source);
        return { title: c.title, summary: c.summary, ago: c.ago, srcIcon: m.icon, srcLabel: m.label,
          srcChip: 'flex:none;align-self:flex-start;display:inline-flex;align-items:center;gap:4px;padding:3px 8px;border-radius:6px;font-size:10.5px;font-weight:600;background:' + m.bg + ';color:' + m.fg,
          open: () => this.openChange(c.path) };
      }),
      health: [
        { label: 'Requirements → drivers', pct: pct(reqWithDriver, M.requirements.length), color: 'var(--reg)' },
        { label: 'Requirements → specs', pct: pct(reqWithSpec, M.requirements.length), color: 'var(--prod)' },
        { label: 'Specs → data fields', pct: pct(specWithField, M.specs.length), color: 'var(--data)' },
      ],
    };
  }

  openChange(path) { this.setState({ view: 'changes', selChange: path }); }

  dashVals() {
    const d = this.buildDashboard();
    if (!d) return { dashReady: false, dashOpenCount: 0, dashFeed: [], dashHealth: [] };
    return {
      dashReady: true,
      dashOpenCount: d.openCount,
      dashSrcLine: d.bySource.regulatory + ' regulatory · ' + d.bySource.product + ' product · ' + d.bySource.technical + ' tech',
      dashSpecCount: d.specCount, dashReqCount: d.reqCount, dashDrifts: d.drifts, dashCov: d.cov,
      dashCovStyle: 'width:' + d.cov + '%;height:100%;background:' + (d.cov > 80 ? 'var(--data)' : d.cov > 60 ? 'var(--prod)' : 'var(--reg)'),
      dashFeed: d.feed,
      dashHealth: d.health.map((h) => ({ label: h.label, pct: h.pct, barStyle: 'width:' + h.pct + '%;height:100%;background:' + h.color })),
    };
  }

  changeVals() {
    const c = this.buildChanges();
    const sel = c.sel;
    const fSeg = (on) => 'flex:1;text-align:center;padding:4px 0;border-radius:6px;font-size:11.5px;cursor:pointer;' + (on ? 'font-weight:600;background:var(--surface);box-shadow:var(--shadow)' : 'color:var(--text-3)');
    const cur = this.state.changeFilter || 'all';
    const items = c.items.map((it) => {
      const m = this.srcMeta(it.source), st = this.statusMeta(it.status);
      const active = sel && sel.path === it.path;
      return {
        title: it.title, ago: it.ago, summary: it.summary,
        srcIcon: m.icon, srcLabel: m.label,
        srcChip: 'display:inline-flex;align-items:center;gap:3px;padding:2px 7px;border-radius:5px;font-size:10px;font-weight:600;background:' + m.bg + ';color:' + m.fg,
        rowStyle: 'padding:13px 14px;border-bottom:1px solid var(--border);cursor:pointer;' + (active ? 'border-left:3px solid ' + m.fg + ';background:var(--surface)' : 'border-left:3px solid transparent'),
        statusLabel: st.label + ' · ' + it.nImpacted + ' impacted', dotStyle: 'width:6px;height:6px;border-radius:50%;background:' + st.color,
        open: () => this.setState({ selChange: it.path }),
      };
    });
    let sv = { changeSelected: false };
    if (sel) {
      const m = this.srcMeta(sel.source);
      const impacts = [];
      sel.reqs.forEach((r) => impacts.push({ badge: r, label: this.reqTitle(r), tag: 'needs update', tagColor: 'var(--reg)', style: 'display:flex;align-items:center;gap:10px;padding:11px 14px;border:1px solid var(--border);border-radius:9px', open: () => this.openReqByName(r) }));
      sel.specs.forEach((s) => impacts.push({ badge: '◈', label: s.split('/').pop(), tag: 'spec', tagColor: 'var(--text-3)', style: 'display:flex;align-items:center;gap:10px;padding:11px 14px;border:1px solid var(--border);border-radius:9px', open: () => this.openDoc(s) }));
      sel.maps.forEach((mp) => { const drift = /executionTimestamp|drift/.test(mp); const field = mp.split('#')[1] || mp.split('/').pop(); impacts.push({ badge: '⇄', label: field, tag: drift ? '⚠ drift' : 'mapping', tagColor: drift ? 'var(--reg)' : 'var(--text-3)', style: 'display:flex;align-items:center;gap:10px;padding:11px 14px;border-radius:9px;' + (drift ? 'border:1px solid var(--reg-line);background:var(--reg-bg)' : 'border:1px solid var(--border)'), open: () => this.openDoc(mp.split('#')[0]) }); });
      sv = {
        changeSelected: true,
        selSrcChip: 'display:inline-flex;align-items:center;gap:4px;padding:3px 9px;border-radius:6px;font-size:11px;font-weight:600;background:' + m.bg + ';color:' + m.fg,
        selSrcIcon: m.icon, selSrcLabel: m.label,
        selRef: sel.path.split('/').pop(), selPublished: sel.published,
        selTitle: sel.title, selSummary: sel.summary,
        selImpacts: impacts,
        selHasDiff: !!sel.diff,
        openSelDiff: () => this.setState({ view: 'diff', diffPath: sel.path }),
      };
    }
    return {
      changeCount: c.items.length, changeItems: items,
      cfAll: fSeg(cur === 'all'), cfReg: fSeg(cur === 'regulatory'), cfProd: fSeg(cur === 'product'), cfTech: fSeg(cur === 'technical'),
      setCfAll: () => this.setState({ changeFilter: 'all', selChange: null }),
      setCfReg: () => this.setState({ changeFilter: 'regulatory', selChange: null }),
      setCfProd: () => this.setState({ changeFilter: 'product', selChange: null }),
      setCfTech: () => this.setState({ changeFilter: 'technical', selChange: null }),
      ...sv,
    };
  }

  modelVals() {
    const yml = this._configYml || '';
    // drivers block: key: { label: "..", icon: "..", color: "#.." }
    const driverBlock = (yml.match(/drivers:\s*\n([\s\S]*?)(?=\nstatuses:|\n[a-z_]+:\s*\n|$)/) || [])[1] || '';
    const drivers = [];
    driverBlock.split('\n').forEach((l) => {
      const m = l.match(/^\s{2}([\w-]+):\s*\{\s*label:\s*"([^"]*)",\s*icon:\s*"([^"]*)",\s*color:\s*"([^"]*)"/);
      if (m) drivers.push({ key: m[1], label: m[2], icon: m[3], color: m[4] });
    });
    const statuses = ((yml.match(/statuses:\s*\[(.*?)\]/) || [])[1] || '').split(',').map((s) => s.trim()).filter(Boolean);
    // link_types block
    const linkBlock = (yml.match(/link_types:\s*\n([\s\S]*?)(?=\npaths:|\n[a-z_]+:\s*\n|$)/) || [])[1] || '';
    const links = [];
    linkBlock.split('\n').forEach((l) => {
      const m = l.match(/^\s{2}([\w-]+):\s*\{\s*from:\s*(\[[^\]]*\]|[\w-]+),\s*to:\s*([\w-]+)/);
      if (m) links.push({ name: m[1], from: m[2].replace(/[[\]]/g, ''), to: m[3] });
    });
    const M = this.state.model;
    // entity counts by folder
    const entities = M ? [
      { kind: 'regulation', label: 'Regulations', count: M.regs.length, color: 'var(--reg)', path: 'regulations/' },
      { kind: 'requirement', label: 'Requirements', count: M.requirements.length, color: 'var(--prod)', path: 'requirements/' },
      { kind: 'spec', label: 'Specs', count: M.specs.length, color: 'var(--text)', path: 'specs/' },
      { kind: 'data_mapping', label: 'Data mappings', count: M.maps.length, color: 'var(--data)', path: 'data-mappings/' },
      { kind: 'change', label: 'Changes', count: M.changes.length, color: 'var(--ai)', path: 'changes/' },
    ] : [];
    // schema field rows
    const schema = this._schema || { fields: {}, order: [] };
    const typeColor = { enum: 'var(--reg)', links: 'var(--prod)', percent: 'var(--data)', code: 'var(--text-2)', user: 'var(--text-2)', tag: 'var(--ai)', date: 'var(--text-3)', text: 'var(--text-2)' };
    const schemaFields = (schema.order || []).filter((k) => (schema.fields || {})[k]).map((k) => {
      const f = schema.fields[k];
      const vals = f.values ? Object.keys(f.values) : [];
      return { key: k, label: f.label || k, type: f.type || 'text', typeStyle: "font-family:'IBM Plex Mono',monospace;font-size:10.5px;padding:1px 7px;border-radius:5px;background:var(--surface-2);color:" + (typeColor[f.type] || 'var(--text-2)'), values: vals.join(' · ') };
    });
    return {
      modelDrivers: drivers.map((d) => ({ label: d.label, icon: d.icon, chip: 'display:inline-flex;align-items:center;gap:6px;padding:6px 12px;border-radius:8px;border:1px solid var(--border);border-left:3px solid ' + d.color + ';background:var(--surface);font-size:12.5px;font-weight:600', iconStyle: 'color:' + d.color })),
      modelStatuses: statuses.map((s) => { const m = this.statusMeta(s); return { label: s.replace(/_/g, ' '), style: 'display:inline-flex;align-items:center;gap:5px;padding:3px 10px;border-radius:20px;font-size:11.5px;font-weight:600;background:var(--surface-2);color:' + m.color, dot: 'width:6px;height:6px;border-radius:50%;background:' + m.color }; }),
      modelLinks: links,
      modelEntities: entities.map((e) => ({ label: e.label, count: e.count, kindStyle: "font-family:'IBM Plex Mono',monospace;font-size:10.5px;color:var(--text-3)", kind: e.kind, dot: 'width:9px;height:9px;border-radius:3px;background:' + e.color, open: () => this.openDoc(this.firstIn(e.path)) })),
      modelSchemaFields: schemaFields,
      openConfig: () => this.openDoc('.reqbase/config.yml'),
      openSchema: () => this.openDoc('.reqbase/schema.json'),
    };
  }

  firstIn(prefix) { const M = this.state.model; if (!M) return prefix; const all = [...M.regs, ...M.requirements, ...M.specs, ...M.maps, ...M.changes]; const hit = all.find((x) => x.path && x.path.startsWith(prefix)); return hit ? hit.path : prefix; }

  reqTitle(id) { const M = this.state.model; const r = M && M.requirements.find((x) => x.id === id); return r ? r.title : id; }
  openReqByName(id) { const M = this.state.model; const r = M && M.requirements.find((x) => x.id === id); if (r) this.openDoc(r.path); }

  buildGraph() {
    const M = this.state.model; if (!M) return null;
    const reqs = M.requirements, specs = M.specs, fields = M.fields;
    const srcMap = {};
    reqs.forEach((r) => r.drivers.forEach((d) => { const k = d.type + '|' + d.ref; if (!srcMap[k]) srcMap[k] = { key: k, type: d.type, ref: d.ref }; }));
    const sources = Object.values(srcMap);
    const short = (ref) => { const b = ref.split('/').pop().split('#')[0]; return b.replace('.md', ''); };
    const sColor = (t) => t === 'regulatory' ? 'var(--reg)' : t === 'product' ? 'var(--prod)' : 'var(--text-2)';
    const sIcon = (t) => t === 'regulatory' ? '⚖' : t === 'product' ? '◆' : '⚙';
    const colX = [16, 250, 486, 712], colW = [156, 150, 150, 176], H = 540;
    const nodes = [], idOf = {};
    const push = (id, col, o) => { o.id = id; o.col = col; nodes.push(o); idOf[id] = o; };
    sources.forEach((s) => push('src:' + s.key, 0, { label: sIcon(s.type) + ' ' + short(s.ref), sub: s.type, kind: 'src', color: sColor(s.type) }));
    reqs.forEach((r) => push('req:' + r.path, 1, { label: r.id, sub: r.title, kind: 'req' }));
    specs.forEach((sp) => push('spec:' + sp.path, 2, { label: sp.name, sub: 'spec', kind: 'spec' }));
    fields.forEach((f) => push('field:' + f.name, 3, { label: f.name, sub: f.drift ? '⚠ drift' : '', kind: 'field', drift: f.drift }));
    [0, 1, 2, 3].forEach((c) => { const col = nodes.filter((n) => n.col === c); const n = col.length; col.forEach((o, i) => { o.x = colX[c]; o.w = colW[c]; o.y = Math.round((H / (n + 1)) * (i + 1)); }); });
    nodes.forEach((o) => {
      const base = 'position:absolute;left:' + o.x + 'px;top:' + (o.y - 20) + 'px;width:' + o.w + 'px;padding:8px 10px;border-radius:9px;box-shadow:var(--shadow);';
      if (o.kind === 'src') o.boxStyle = base + 'background:var(--surface);border:1px solid var(--border-2);border-left:3px solid ' + o.color;
      else if (o.kind === 'field') o.boxStyle = base + (o.drift ? 'background:var(--reg-bg);border:1px solid var(--reg-line)' : 'background:var(--data-bg);border:1px solid var(--data-line)');
      else o.boxStyle = base + 'background:var(--surface);border:1px solid var(--border-2)';
      o.labelStyle = o.kind === 'src' ? "font-family:'IBM Plex Mono',monospace;font-size:9.5px;font-weight:700;color:" + o.color
        : o.kind === 'field' ? "font-family:'IBM Plex Mono',monospace;font-size:11px;font-weight:600;color:var(--data)"
        : o.kind === 'spec' ? 'font-size:12px;font-weight:600'
        : "font-family:'IBM Plex Mono',monospace;font-size:9.5px;color:var(--text-3)";
      o.subStyle = o.kind === 'field' ? 'font-size:10px;color:var(--reg);margin-top:1px'
        : o.kind === 'spec' ? "font-family:'IBM Plex Mono',monospace;font-size:9.5px;color:var(--text-3);margin-top:1px"
        : 'font-size:12px;font-weight:600;margin-top:1px;text-transform:capitalize';
    });
    const edges = [];
    const edge = (a, b, stroke) => { const p = idOf[a], q = idOf[b]; if (!p || !q) return; const x1 = p.x + p.w, y1 = p.y, x2 = q.x, y2 = q.y, mx = (x1 + x2) / 2; edges.push({ d: 'M' + x1 + ' ' + y1 + ' C' + mx + ' ' + y1 + ' ' + mx + ' ' + y2 + ' ' + x2 + ' ' + y2, stroke }); };
    const resolveField = (ref) => { const a = ref.split('#')[1] || ''; return fields.find((f) => f.name === a || f.name.endsWith('.' + a)); };
    reqs.forEach((r) => {
      r.drivers.forEach((d) => edge('src:' + (d.type + '|' + d.ref), 'req:' + r.path, sColor(d.type)));
      r.implements.filter((p) => p.startsWith('specs/')).forEach((sp) => edge('req:' + r.path, 'spec:' + sp, 'var(--border-2)'));
      r.maps_to.forEach((ref) => { const f = resolveField(ref); if (f) edge('req:' + r.path, 'field:' + f.name, 'var(--border-2)'); });
    });
    return { nodes, edges, H, stats: { s: sources.length, r: reqs.length, sp: specs.length, f: fields.length } };
  }

  buildMatrix() {
    const M = this.state.model; if (!M) return { mgroups: [], mcolumns: [], mrows: [], caption: '' };
    const specs = M.specs, fields = M.fields, reqs = M.requirements;
    const CW = 26;
    const columns = [];
    specs.forEach((s) => columns.push({ kind: 'spec', ref: s.path, label: s.name.replace('.md', '') }));
    fields.forEach((f) => columns.push({ kind: 'field', ref: f.name, label: f.name.split('.').pop(), drift: f.drift }));
    columns.push({ kind: 'test', ref: 'tests', label: 'tests' });
    const mgroups = [
      { label: 'Specs', color: 'var(--text-2)', width: specs.length * CW },
      { label: 'Data fields', color: 'var(--data)', width: fields.length * CW },
      { label: 'Tests', color: 'var(--prod)', width: CW },
    ];
    const sqBase = 'width:15px;height:15px;border-radius:4px;box-sizing:border-box;';
    const sq = (t) => t === 'linked' ? sqBase + 'background:var(--data);border:1px solid var(--data)' : t === 'drift' ? sqBase + 'background:var(--reg);border:1px solid var(--reg)' : 'width:5px;height:5px;border-radius:50%;background:var(--border-2)';
    const fieldNameFromRef = (ref) => { const a = ref.split('#')[1] || ''; const f = fields.find((x) => x.name === a || x.name.endsWith('.' + a)); return f ? f.name : null; };
    const mrows = reqs.map((r) => {
      const mappedFields = new Set(r.maps_to.map(fieldNameFromRef).filter(Boolean));
      const cells = columns.map((c) => {
        let t = 'none';
        if (c.kind === 'spec') t = r.implements.indexOf(c.ref) >= 0 ? 'linked' : 'none';
        else if (c.kind === 'field') t = mappedFields.has(c.ref) ? (c.drift ? 'drift' : 'linked') : 'none';
        else if (c.kind === 'test') t = r.verifies.length ? 'linked' : 'none';
        return { sq: sq(t) };
      });
      const cov = Math.round((r.coverage || 0) * 100);
      const covC = cov > 80 ? 'var(--data)' : cov > 64 ? 'var(--prod)' : 'var(--reg)';
      return { id: r.id, name: r.title, cells, cov, covStyle: 'width:' + cov + '%;height:100%;background:' + covC };
    });
    return { mgroups, mcolumns: columns, mrows, caption: reqs.length + ' requirements × ' + columns.length + ' artifacts' };
  }

  // Diff view is driven by the change record selected in the inbox (falls back to PR #128).
  diffVals() {
    const M = this.state.model;
    const changes = M ? M.changes : [];
    const c = changes.find((x) => x.path === this.state.diffPath) || changes.find((x) => x.path === 'changes/2026-06-mifid-rts22.md') || null;
    if (!c) return { diffReady: false };
    const isMifid = c.path === 'changes/2026-06-mifid-rts22.md';
    const files = isMifid
      ? [
          { icon: '◈', color: 'var(--reg)', name: 'mifid-ii.md', add: '+8', del: '−3', active: true },
          { icon: '⇄', color: 'var(--data)', name: 'trade.md', add: '+2', del: '−2', active: false },
          { icon: '▤', color: 'var(--prod)', name: 'REQ-042.md', add: '+4', del: '', active: false },
        ]
      : [
          ...c.impSpecs.map((s, i) => ({ icon: '◈', color: 'var(--reg)', name: s.split('/').pop(), add: '', del: '', active: i === 0 })),
          ...c.impMaps.map((mp) => ({ icon: '⇄', color: 'var(--data)', name: (mp.split('#')[0] || mp).split('/').pop(), add: '', del: '', active: false })),
          ...c.impReqs.map((r) => ({ icon: '▤', color: 'var(--prod)', name: r + '.md', add: '', del: '', active: false })),
        ];
    if (!isMifid && files.length && !files.some((f) => f.active)) files[0].active = true;
    const raw = c.diff || '';
    const diffLines = raw.split('\n').map((ln) => {
      const sign = ln[0] === '+' ? '+' : ln[0] === '-' ? '-' : ' ';
      const style = sign === '+' ? 'background:var(--add-bg)' : sign === '-' ? 'background:var(--del-bg)' : '';
      const signColor = sign === '+' ? 'var(--add)' : sign === '-' ? 'var(--del)' : 'var(--text-3)';
      const textColor = sign === ' ' ? 'var(--text-2)' : 'var(--text)';
      return { sign, text: ln.slice(1), rowStyle: style, signColor, textColor };
    });
    return {
      diffReady: true,
      diffTitle: c.title,
      diffPr: this._chgScalar(c.path, 'pr'),
      diffBranch: this._chgScalar(c.path, 'branch') || 'feature/mifid-update',
      diffFileHeader: isMifid ? 'regulations/mifid-ii.md' : (c.impSpecs[0] || c.path),
      diffHunk: '@@ ' + c.path + ' @@',
      diffFiles: files,
      diffLines,
      diffIsMifid: isMifid,
    };
  }
  _chgScalar(path, key) {
    const raw = this._rawFiles && this._rawFiles[path];
    if (!raw) return '';
    const v = R.scalar(R.stripFrontmatter(raw).fm, key);
    return v === 'null' ? '' : v;
  }

  // ---------------------------------------------------------------- vals
  renderVals() {
    const view = this.state.view || 'dashboard';
    const set = (v) => () => this.setState({ view: v });
    const tab = (on) => on
      ? 'background:var(--bg);color:var(--text);border-bottom:2px solid var(--text)'
      : 'background:transparent;color:var(--text-3);border-bottom:2px solid transparent;border-right:1px solid var(--border)';
    const A = 'background:var(--surface);box-shadow:var(--shadow);color:var(--text)';
    const I = 'background:transparent;color:var(--text-2)';
    const seg = (on) => on ? 'background:var(--text);color:var(--surface)' : 'color:var(--text-2);cursor:pointer';
    const isEditor = view === 'editor', isGraph = view === 'graph', isMatrix = view === 'matrix';
    const mx = this.buildMatrix();
    const graph = this.buildGraph();

    // ---- file-driven document + change ----
    const doc = this.state.doc;
    const change = this.state.change;
    const mode = this.state.docMode || 'preview';
    const tseg = (on) => on ? 'background:var(--surface);box-shadow:var(--shadow);color:var(--text)' : 'color:var(--text-3)';
    // syntax-tinted source lines
    let dashCount = 0, inFm = false, fenced = false;
    const docSourceLines = doc ? doc.raw.split('\n').map((t, i) => {
      const tr = t.trimStart();
      let color = 'var(--text)';
      if (t.trim() === '---' && dashCount < 2) { dashCount++; inFm = dashCount === 1; color = 'var(--text-3)'; }
      else if (inFm) { color = /^\s/.test(t) ? 'var(--text-2)' : 'var(--prod)'; }
      else if (/^```/.test(tr)) { fenced = !fenced; color = 'var(--ai)'; }
      else if (fenced) { color = 'var(--text-2)'; }
      else if (/^#{1,6}\s/.test(tr)) { color = 'var(--reg)'; }
      else if (/^\|/.test(tr)) { color = 'var(--data)'; }
      else if (/^([-*]\s|>\s|\d+\.\s)/.test(tr)) { color = 'var(--text-2)'; }
      return { n: i + 1, text: t, color };
    }) : [];
    const docProps = this.buildProps(doc && doc.fm);

    return {
      docReady: !!doc,
      docLoading: !doc && !this.state.docErr,
      docErr: this.state.docErr || '',
      docBodyHtml: doc ? doc.bodyHtml : '',
      docKey: doc ? doc.path : '',
      docTitle: doc ? doc.title : '',
      docStatus: doc ? (doc.status || '').replace(/_/g, ' ') : '',
      docName: doc ? doc.name : '',
      docPath: doc ? doc.path : 'specs/txn-report.md',
      docHasStatus: !!(doc && doc.status),
      docProps,
      docHasProps: docProps.length > 0,
      propsCount: docProps.length,
      propsOpen: this.state.propsOpen !== false,
      toggleProps: () => this.setState((s) => ({ propsOpen: s.propsOpen === false })),
      propsChevron: this.state.propsOpen !== false ? 'transform:rotate(90deg);transition:transform .15s' : 'transform:rotate(0deg);transition:transform .15s',
      treeFolders: this.buildTree(),
      nModified: this._nModified || 0,
      isPreview: mode !== 'source',
      isSource: mode === 'source',
      setPreview: () => this.setState({ docMode: 'preview' }),
      setSource: () => this.setState({ docMode: 'source' }),
      segPreview: tseg(mode !== 'source'),
      segSource: tseg(mode === 'source'),
      docSourceLines,
      changeSummary: change ? change.summary : '',
      changePublished: change ? change.published : '',
      changeReference: change ? change.reference : '',
      mgroups: mx.mgroups, mcolumns: mx.mcolumns, mrows: mx.mrows, matrixCaption: mx.caption,
      graphNodes: graph ? graph.nodes : [], graphEdges: graph ? graph.edges : [],
      graphStats: graph ? graph.stats : { s: 0, r: 0, sp: 0, f: 0 }, graphH: graph ? graph.H : 540,
      isDashboard: view === 'dashboard',
      isChanges: view === 'changes',
      ...this.dashVals(),
      ...this.changeVals(),
      ...this.diffVals(),
      isEditor, isGraph, isMatrix,
      isDiff: view === 'diff',
      isDoc: isEditor || isGraph,
      setDashboard: set('dashboard'),
      setChanges: set('changes'),
      setEditor: set('editor'),
      setGraph: set('graph'),
      setMatrix: set('matrix'),
      setModel: set('model'),
      setDiff: set('diff'),
      tabEditor: tab(isEditor),
      tabGraph: tab(isGraph),
      railDash: view === 'dashboard' ? A : I,
      railExpl: (isEditor || view === 'diff') ? A : I,
      railChng: view === 'changes' ? A : I,
      railTrace: isGraph ? A : I,
      railMap: isMatrix ? A : I,
      railModel: view === 'model' ? A : I,
      isModel: view === 'model',
      showTree: isEditor || view === 'diff',
      ...this.modelVals(),
      segGraph: seg(isGraph),
      segMatrix: seg(isMatrix),
      copilotOpen: this.state.copilotOpen,
      aiSuggestions: this.state.aiSuggestions,
      toggleCopilot: () => { const v2 = !this.state.copilotOpen; localStorage.setItem('reqbase-copilot', v2 ? '1' : '0'); this.setState({ copilotOpen: v2 }); },
      toggleTheme: () => { const t = this.state.theme === 'dark' ? 'light' : 'dark'; localStorage.setItem('reqbase-theme', t); this.setState({ theme: t }); },
      toggleAI: () => this.setState((s) => ({ aiSuggestions: !s.aiSuggestions })),
      themeLabel: this.state.theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme',
    };
  }

  // ---------------------------------------------------------------- render
  render() {
    this.applyTheme();
    this.syncHash();
    const scrolls = {};
    this.root.querySelectorAll('[data-sk]').forEach((el) => { scrolls[el.dataset.sk] = [el.scrollTop, el.scrollLeft]; });
    this.actions = [];
    const v = this.renderVals();
    this.root.innerHTML = this.template(v);
    this.root.querySelectorAll('[data-sk]').forEach((el) => { const s = scrolls[el.dataset.sk]; if (s) { el.scrollTop = s[0]; el.scrollLeft = s[1]; } });
    this.hydrateDoc();
  }

  template(v) {
    const on = (f) => this.on(f);
    return `
<div style="height:100vh;min-height:720px;display:flex;flex-direction:column;overflow:hidden;font-size:13px">

  <!-- top bar -->
  <header style="height:46px;flex:none;display:flex;align-items:center;gap:12px;padding:0 12px 0 14px;background:var(--surface);border-bottom:1px solid var(--border);position:relative;z-index:5">
    <div style="display:flex;align-items:center;gap:8px">
      <div style="width:22px;height:22px;border-radius:6px;background:var(--text);display:flex;align-items:center;justify-content:center">
        <div style="width:9px;height:9px;border-radius:2px;border:2px solid var(--surface);transform:rotate(45deg)"></div>
      </div>
      <span style="font-weight:700;font-size:14px;letter-spacing:-.2px">reqbase</span>
    </div>
    <div style="display:flex;align-items:center;gap:6px;padding:4px 9px;border:1px solid var(--border-2);border-radius:7px;cursor:default;background:var(--surface-2)">
      ${ICONS.branch}
      <span style="font-family:'IBM Plex Mono',monospace;font-size:11.5px;font-weight:500">feature/mifid-update</span>
      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="2.4"><path d="M6 9l6 6 6-6"/></svg>
    </div>
    <div style="flex:1"></div>
    <div style="width:340px;height:30px;display:flex;align-items:center;gap:8px;padding:0 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface-2);color:var(--text-3)">
      ${ICONS.search}
      <span style="font-size:12.5px">Search requirements, specs, fields, changes…</span>
      <div style="flex:1"></div>
      <span style="font-family:'IBM Plex Mono';font-size:11px;padding:1px 5px;border:1px solid var(--border-2);border-radius:4px">⌘K</span>
    </div>
    <div style="flex:1"></div>
    <div style="display:flex;align-items:center;gap:5px;font-family:'IBM Plex Mono';font-size:11.5px;color:var(--text-2);padding:4px 8px;border:1px solid var(--border);border-radius:7px">
      ${ICONS.up}2 ${ICONS.down}0
    </div>
    <button ${on(v.setDiff)} style="display:flex;align-items:center;gap:6px;height:30px;padding:0 12px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">
      ${ICONS.pr} Open PR
    </button>
    <div style="width:28px;height:28px;border-radius:50%;background:linear-gradient(135deg,var(--ai),var(--prod));display:flex;align-items:center;justify-content:center;color:#fff;font-weight:600;font-size:11px">SG</div>
  </header>

  <!-- body -->
  <div style="flex:1;display:flex;min-height:0">

    <!-- icon rail -->
    <nav style="width:52px;flex:none;background:var(--rail);border-right:1px solid var(--border);display:flex;flex-direction:column;align-items:center;padding:8px 0;gap:3px">
      <button ${on(v.setDashboard)} title="Overview" style="width:40px;height:40px;border-radius:9px;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;${v.railDash}">${ICONS.dash}</button>
      <button ${on(v.setEditor)} title="Specs" style="width:40px;height:40px;border-radius:9px;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;${v.railExpl}">${ICONS.folder}</button>
      <button ${on(v.setChanges)} title="Changes" style="width:40px;height:40px;border-radius:9px;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;position:relative;${v.railChng}">
        ${ICONS.changes}
        ${v.dashReady ? `<span style="position:absolute;top:5px;right:6px;min-width:15px;height:15px;padding:0 3px;border-radius:8px;background:var(--reg);color:#fff;font-size:9.5px;font-weight:700;display:flex;align-items:center;justify-content:center">${v.dashOpenCount}</span>` : ''}
      </button>
      <button ${on(v.setGraph)} title="Traceability" style="width:40px;height:40px;border-radius:9px;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;${v.railTrace}">${ICONS.trace}</button>
      <button ${on(v.setMatrix)} title="Traceability matrix" style="width:40px;height:40px;border-radius:9px;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;${v.railMap}">${ICONS.matrix}</button>
      <button ${on(v.setModel)} title="Model definitions" style="width:40px;height:40px;border-radius:9px;border:none;display:flex;align-items:center;justify-content:center;cursor:pointer;${v.railModel}">${ICONS.model}</button>
      <div style="flex:1"></div>
      <button ${on(v.toggleCopilot)} title="Copilot" style="width:40px;height:40px;border-radius:9px;border:none;background:${v.copilotOpen ? 'var(--ai-bg)' : 'transparent'};color:var(--ai);display:flex;align-items:center;justify-content:center;cursor:pointer">${ICONS.spark(19)}</button>
      <button ${on(v.toggleTheme)} title="${v.themeLabel}" style="width:40px;height:40px;border-radius:9px;border:none;background:transparent;color:var(--text-2);display:flex;align-items:center;justify-content:center;cursor:pointer">${ICONS.gear}</button>
    </nav>

    ${v.showTree ? this.tplTree(v) : ''}

    <!-- center -->
    <main style="flex:1;min-width:0;display:flex;flex-direction:column;background:var(--bg)">
      ${v.isDoc ? this.tplTabs(v) : ''}
      ${v.isEditor ? this.tplEditor(v) : ''}
      ${v.isGraph ? this.tplGraph(v) : ''}
      ${v.isDashboard ? this.tplDashboard(v) : ''}
      ${v.isChanges ? this.tplChanges(v) : ''}
      ${v.isMatrix ? this.tplMatrix(v) : ''}
      ${v.isModel ? this.tplModel(v) : ''}
      ${v.isDiff ? this.tplDiff(v) : ''}
    </main>

    ${v.copilotOpen ? this.tplCopilot(v) : ''}
  </div>
</div>`;
  }

  tplTree(v) {
    const on = (f) => this.on(f);
    return `
    <aside style="width:250px;flex:none;background:var(--panel);border-right:1px solid var(--border);display:flex;flex-direction:column">
      <div style="height:38px;flex:none;display:flex;align-items:center;justify-content:space-between;padding:0 8px 0 14px;border-bottom:1px solid var(--border)">
        <div style="display:flex;align-items:center;gap:5px;font-weight:700;font-size:11px;letter-spacing:.5px;color:var(--text-2)">${ICONS.chevR}TRADING-SPECS</div>
        <div style="display:flex;gap:2px;color:var(--text-3)">
          <span style="width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer">${ICONS.plus}</span>
          <span style="width:22px;height:22px;display:flex;align-items:center;justify-content:center;border-radius:5px;cursor:pointer">${ICONS.sync}</span>
        </div>
      </div>
      <div data-sk="tree" style="flex:1;overflow-y:auto;padding:8px 6px;font-size:12.5px;user-select:none">
        ${v.treeFolders.map((folder) => `
          <div style="display:flex;align-items:center;gap:5px;padding:4px 8px;margin-top:3px;color:var(--text-2);font-weight:600">${ICONS.chevD}<span style="opacity:.9">${esc(folder.name)}</span></div>
          ${folder.files.map((f) => `
            <div ${on(f.open)} style="display:flex;align-items:center;gap:7px;padding:5px 8px 5px 26px;border-radius:6px;cursor:pointer;${f.rowStyle}"><span style="color:${f.color};flex:none">${f.icon}</span><span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(f.name)}</span><div style="flex:1"></div><span style="font-family:'IBM Plex Mono';font-size:10px;${f.badgeStyle}">${f.badge}</span></div>`).join('')}
        `).join('')}
      </div>
      <div style="height:30px;flex:none;display:flex;align-items:center;gap:8px;padding:0 12px;border-top:1px solid var(--border);font-family:'IBM Plex Mono';font-size:10.5px;color:var(--text-2)">
        <span style="color:var(--reg)">●</span> ${v.nModified} changes<div style="flex:1"></div><span>synced 2m ago</span>
      </div>
    </aside>`;
  }

  tplTabs(v) {
    const on = (f) => this.on(f);
    return `
      <div style="height:38px;flex:none;display:flex;align-items:stretch;background:var(--panel);border-bottom:1px solid var(--border);padding-left:2px">
        <div ${on(v.setEditor)} style="display:flex;align-items:center;gap:8px;padding:0 14px;cursor:pointer;${v.tabEditor}">
          <span style="color:var(--reg)">◈</span><span style="font-size:12.5px;font-weight:600">${esc(v.docName || v.docPath.split('/').pop())}</span>
          <span style="width:5px;height:5px;border-radius:50%;background:var(--reg)"></span>
        </div>
        <div style="display:flex;align-items:center;gap:8px;padding:0 14px;color:var(--text-3);border-right:1px solid var(--border)">
          <span style="color:var(--prod)">▤</span><span style="font-size:12.5px">REQ-042.md</span>
          ${ICONS.close}
        </div>
        <div ${on(v.setGraph)} style="display:flex;align-items:center;gap:8px;padding:0 14px;cursor:pointer;${v.tabGraph}">
          ${ICONS.traceSm}
          <span style="font-size:12.5px;font-weight:600">Impact Graph</span>
        </div>
        <div style="flex:1"></div>
      </div>`;
  }

  tplEditor(v) {
    const on = (f) => this.on(f);
    return `
      <div style="flex:1;min-height:0;display:flex;flex-direction:column">
        <div style="height:40px;flex:none;display:flex;align-items:center;gap:12px;padding:0 16px;background:var(--surface);border-bottom:1px solid var(--border)">
          <div style="display:flex;align-items:center;gap:6px;font-family:'IBM Plex Mono';font-size:11.5px;color:var(--text-2)"><span style="color:var(--text)">${esc(v.docPath)}</span></div>
          <div style="flex:1"></div>
          <div style="display:flex;background:var(--surface-2);border:1px solid var(--border);border-radius:8px;padding:2px">
            <span ${on(v.setPreview)} style="padding:3px 12px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;${v.segPreview}">Preview</span>
            <span ${on(v.setSource)} style="padding:3px 12px;border-radius:6px;font-size:12px;font-weight:600;cursor:pointer;${v.segSource}">Source</span>
            <span style="padding:3px 12px;border-radius:6px;font-size:12px;color:var(--text-3)">History</span>
          </div>
          <span style="width:1px;height:20px;background:var(--border)"></span>
          <button style="display:flex;align-items:center;gap:5px;height:28px;padding:0 10px;border:1px solid var(--border-2);border-radius:7px;background:var(--surface);color:var(--text-2);font-family:inherit;font-size:12px;cursor:pointer">${ICONS.share}Share</button>
        </div>

        <div data-sk="editor" style="flex:1;overflow-y:auto;padding:34px 40px 80px">
          ${v.isPreview ? `
          <div style="max-width:820px;margin:0 auto">
            ${v.docLoading ? `
              <div style="display:flex;flex-direction:column;gap:13px">
                <div style="height:30px;width:52%;border-radius:7px;background:var(--surface-2)"></div>
                <div style="height:12px;width:38%;border-radius:6px;background:var(--surface-2)"></div>
                <div style="height:12px;width:92%;border-radius:6px;background:var(--surface-2);margin-top:14px"></div>
                <div style="height:12px;width:82%;border-radius:6px;background:var(--surface-2)"></div>
                <div style="height:120px;width:100%;border-radius:10px;background:var(--surface-2);margin-top:10px"></div>
                <div style="font-family:'IBM Plex Mono';font-size:11px;color:var(--text-3)">rendering ${esc(v.docPath)}…</div>
              </div>` : ''}
            ${v.docReady ? `
              <div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap">
                <h1 style="margin:0;font-size:29px;font-weight:700;letter-spacing:-.5px;line-height:1.15">${esc(v.docTitle)}</h1>
                ${v.docHasStatus ? `<span style="display:inline-flex;align-items:center;gap:6px;padding:4px 10px;border-radius:20px;background:var(--reg-bg);color:var(--reg);font-size:11.5px;font-weight:600;text-transform:capitalize"><span style="width:6px;height:6px;border-radius:50%;background:var(--reg)"></span>${esc(v.docStatus)}</span>` : ''}
              </div>
              ${v.docHasProps ? `
              <div style="margin:16px 0 30px;border:1px solid var(--border);border-radius:10px;overflow:hidden;background:var(--surface)">
                <div ${on(v.toggleProps)} style="display:flex;align-items:center;gap:8px;padding:8px 14px;background:var(--surface-2);cursor:pointer;user-select:none">
                  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="2.6" style="${v.propsChevron}"><path d="M9 6l6 6-6 6"/></svg>
                  <span style="font-family:'IBM Plex Mono';font-size:10.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px">Properties</span>
                  <span style="font-family:'IBM Plex Mono';font-size:10.5px;color:var(--text-3)">· ${v.propsCount} fields</span>
                </div>
                ${v.propsOpen ? v.docProps.map((p) => `
                  <div style="display:flex;gap:14px;padding:8px 14px;border-top:1px solid var(--border)">
                    <span style="width:132px;flex:none;font-family:'IBM Plex Mono';font-size:11px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.3px;padding-top:2px">${esc(p.key)}</span>
                    <div style="flex:1;display:flex;flex-wrap:wrap;gap:6px;align-items:center;min-width:0">
                      ${p.items.map((it) => `<span ${on(it.open)} style="${it.style}">${esc(it.text)}</span>`).join('')}
                    </div>
                  </div>`).join('') : ''}
              </div>` : ''}
              <div id="reqbase-doc" data-key="${esc(v.docKey)}">${v.docBodyHtml}</div>
              ${v.aiSuggestions && v.changeSummary ? `
                <div style="margin-top:24px;border:1px solid var(--ai-line);border-radius:10px;overflow:hidden;background:var(--surface);box-shadow:var(--shadow)">
                  <div style="display:flex;align-items:center;gap:9px;padding:10px 14px;background:var(--ai-bg);border-bottom:1px solid var(--ai-line)">
                    ${ICONS.sparkAI(14)}
                    <span style="font-size:12px;font-weight:600;color:var(--ai)">Copilot suggests an edit</span>
                    <span style="font-size:11px;color:var(--text-2)">from ${esc(v.changeReference)} · ${esc(v.changePublished)}</span>
                    <div style="flex:1"></div>
                    <button ${on(v.setDiff)} style="height:26px;padding:0 11px;border:none;border-radius:6px;background:var(--ai);color:#fff;font-family:inherit;font-size:11.5px;font-weight:600;cursor:pointer">Review diff →</button>
                  </div>
                  <div style="padding:12px 14px;font-size:13px;line-height:1.62;color:var(--text)">${esc(v.changeSummary)}</div>
                </div>` : ''}
            ` : ''}
            ${v.docErr ? `<div style="padding:16px;border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:10px;color:var(--reg);font-size:13px">Couldn't load source files: ${esc(v.docErr)}</div>` : ''}
          </div>` : ''}
          ${v.isSource ? `
          <div style="max-width:960px;margin:0 auto;border:1px solid var(--border);border-radius:10px;overflow:hidden;background:var(--surface);font-family:'IBM Plex Mono',monospace;font-size:12.5px;line-height:1.8">
            ${v.docSourceLines.map((ln) => `<div style="display:flex"><span style="width:46px;flex:none;text-align:right;padding:0 12px 0 0;color:var(--text-3);user-select:none;background:var(--surface-2);border-right:1px solid var(--border)">${ln.n}</span><span style="flex:1;white-space:pre-wrap;word-break:break-word;padding:0 14px;color:${ln.color}">${esc(ln.text)}</span></div>`).join('')}
          </div>` : ''}
        </div>
      </div>`;
  }

  tplGraph(v) {
    const on = (f) => this.on(f);
    return `
      <div data-sk="graph" style="flex:1;min-height:0;position:relative;overflow:auto;background:radial-gradient(circle,var(--border) 1px,transparent 1px);background-size:22px 22px">
        <div style="position:absolute;left:50%;top:14px;transform:translateX(-50%);z-index:4;display:flex;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow-lg);padding:3px">
          <span style="padding:5px 15px;border-radius:6px;font-size:12px;font-weight:600;${v.segGraph}">Graph</span>
          <span ${on(v.setMatrix)} style="padding:5px 15px;border-radius:6px;font-size:12px;font-weight:600;${v.segMatrix}">Matrix</span>
        </div>
        <div style="position:absolute;left:16px;top:14px;z-index:3;display:flex;align-items:center;gap:6px;padding:6px;background:var(--surface);border:1px solid var(--border);border-radius:10px;box-shadow:var(--shadow-lg);flex-wrap:wrap;max-width:calc(100% - 32px)">
          <span style="font-size:10.5px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;padding:0 6px">Layers</span>
          <span style="display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;background:var(--reg-bg);color:var(--reg);font-size:11.5px;font-weight:600">◉ Sources</span>
          <span style="display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;background:var(--prod-bg);color:var(--prod);font-size:11.5px;font-weight:600">◉ Requirements</span>
          <span style="display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;background:var(--surface-2);color:var(--text-2);font-size:11.5px;font-weight:600">◉ Specs</span>
          <span style="display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border-radius:6px;background:var(--data-bg);color:var(--data);font-size:11.5px;font-weight:600">◉ Data fields</span>
          <span style="width:1px;height:18px;background:var(--border);margin:0 2px"></span>
          <span ${on(v.toggleAI)} style="display:inline-flex;align-items:center;gap:6px;padding:4px 9px;border-radius:6px;background:var(--ai-bg);color:var(--ai);font-size:11.5px;font-weight:600;cursor:pointer"><span style="width:22px;height:13px;border-radius:8px;background:${v.aiSuggestions ? 'var(--ai)' : 'var(--border-2)'};position:relative;display:inline-block"><span style="position:absolute;${v.aiSuggestions ? 'right' : 'left'}:1px;top:1px;width:11px;height:11px;border-radius:50%;background:#fff"></span></span>AI suggestions</span>
        </div>

        <div style="position:relative;width:900px;height:${v.graphH}px;margin:70px auto 40px;min-width:900px">
          <svg style="position:absolute;inset:0;width:100%;height:100%;overflow:visible">
            ${v.graphEdges.map((e) => `<path d="${e.d}" fill="none" stroke="${e.stroke}" stroke-width="1.8"></path>`).join('')}
          </svg>
          ${v.graphNodes.map((n) => `
            <div style="${n.boxStyle}">
              <div style="${n.labelStyle}">${esc(n.label)}</div>
              <div style="${n.subStyle}">${esc(n.sub)}</div>
            </div>`).join('')}
        </div>

        <div style="position:absolute;right:16px;top:14px;z-index:3;width:210px;background:var(--surface);border:1px solid var(--border);border-radius:11px;box-shadow:var(--shadow-lg);overflow:hidden">
          <div style="padding:10px 14px;border-bottom:1px solid var(--border);background:var(--surface-2);font-family:'IBM Plex Mono';font-size:9.5px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px">Lineage · from links</div>
          <div style="padding:11px 14px;display:flex;flex-direction:column;gap:9px;font-size:12.5px">
            <div style="display:flex;justify-content:space-between;align-items:center"><span style="color:var(--text-2)">Sources</span><b>${v.graphStats.s}</b></div>
            <div style="display:flex;justify-content:space-between;align-items:center"><span style="color:var(--text-2)">Requirements</span><b>${v.graphStats.r}</b></div>
            <div style="display:flex;justify-content:space-between;align-items:center"><span style="color:var(--text-2)">Specs</span><b>${v.graphStats.sp}</b></div>
            <div style="display:flex;justify-content:space-between;align-items:center"><span style="color:var(--text-2)">Data fields</span><b>${v.graphStats.f}</b></div>
          </div>
        </div>

        <div style="position:absolute;left:16px;bottom:14px;z-index:3;display:flex;align-items:center;gap:14px;padding:7px 12px;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow);font-size:11px;color:var(--text-2)">
          <span style="display:flex;align-items:center;gap:6px"><span style="width:16px;height:2px;background:var(--text-2)"></span>lineage · computed from frontmatter links</span>
        </div>
        <div style="position:absolute;right:16px;bottom:14px;z-index:3;display:flex;align-items:center;background:var(--surface);border:1px solid var(--border);border-radius:9px;box-shadow:var(--shadow);overflow:hidden">
          <span style="width:30px;height:30px;display:flex;align-items:center;justify-content:center;cursor:pointer;color:var(--text-2);border-right:1px solid var(--border)">−</span>
          <span style="padding:0 10px;font-family:'IBM Plex Mono';font-size:11px">100%</span>
          <span style="width:30px;height:30px;display:flex;align-items:center;justify-content:center;cursor:pointer;color:var(--text-2);border-left:1px solid var(--border)">+</span>
        </div>
      </div>`;
  }

  tplDashboard(v) {
    const on = (f) => this.on(f);
    return `
      <div data-sk="dash" style="flex:1;min-height:0;overflow-y:auto;background:var(--bg)">
        <div style="max-width:1020px;margin:0 auto;padding:28px 32px 64px">
          <div style="display:flex;align-items:flex-end;justify-content:space-between;gap:16px;flex-wrap:wrap">
            <div>
              <div style="font-family:'IBM Plex Mono';font-size:11.5px;color:var(--text-3)">trading-specs · feature/mifid-update</div>
              <h1 style="margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px">Overview</h1>
            </div>
            <div style="display:flex;gap:8px">
              <button style="height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">+ New requirement</button>
              <button ${on(v.setChanges)} style="height:32px;padding:0 13px;border:none;border-radius:8px;background:var(--text);color:var(--bg);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">Review changes · ${v.dashOpenCount}</button>
            </div>
          </div>

          <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-top:22px">
            <div style="background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:15px 16px;box-shadow:var(--shadow)"><div style="font-size:11.5px;color:var(--text-2)">Open changes</div><div style="display:flex;align-items:baseline;gap:8px;margin-top:8px"><span style="font-size:27px;font-weight:700;letter-spacing:-.5px">${v.dashOpenCount}</span></div><div style="font-size:10.5px;color:var(--text-3);margin-top:4px">${v.dashSrcLine || ''}</div></div>
            <div style="background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:15px 16px;box-shadow:var(--shadow)"><div style="font-size:11.5px;color:var(--text-2)">Requirements</div><div style="display:flex;align-items:baseline;gap:8px;margin-top:8px"><span style="font-size:27px;font-weight:700;letter-spacing:-.5px">${v.dashReqCount ?? '–'}</span></div><div style="font-size:10.5px;color:var(--text-3);margin-top:4px">${v.dashSpecCount ?? 0} specs linked</div></div>
            <div style="background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:15px 16px;box-shadow:var(--shadow)"><div style="font-size:11.5px;color:var(--text-2)">Mapping drifts</div><div style="display:flex;align-items:baseline;gap:8px;margin-top:8px"><span style="font-size:27px;font-weight:700;letter-spacing:-.5px;color:var(--reg)">${v.dashDrifts ?? '–'}</span></div><div style="font-size:10.5px;color:var(--text-3);margin-top:4px">need re-validation</div></div>
            <div style="background:var(--surface);border:1px solid var(--border);border-radius:12px;padding:15px 16px;box-shadow:var(--shadow)"><div style="font-size:11.5px;color:var(--text-2)">Trace coverage</div><div style="display:flex;align-items:baseline;gap:8px;margin-top:8px"><span style="font-size:27px;font-weight:700;letter-spacing:-.5px">${v.dashCov ?? 0}<span style="font-size:15px">%</span></span></div><div style="height:5px;border-radius:3px;background:var(--surface-2);margin-top:8px;overflow:hidden"><div style="${v.dashCovStyle || ''}"></div></div></div>
          </div>

          <div style="display:grid;grid-template-columns:1.65fr 1fr;gap:18px;margin-top:20px;align-items:start">
            <div style="background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);overflow:hidden">
              <div style="display:flex;align-items:center;gap:8px;padding:13px 16px;border-bottom:1px solid var(--border)"><span style="font-weight:700;font-size:13.5px">Requirement changes</span><span style="font-size:11px;color:var(--text-3)">— all sources</span><div style="flex:1"></div><span ${on(v.setChanges)} style="font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600">Open inbox →</span></div>
              ${v.dashFeed.map((f) => `
                <div ${on(f.open)} style="display:flex;gap:12px;padding:14px 16px;border-bottom:1px solid var(--border);cursor:pointer">
                  <span style="${f.srcChip}">${f.srcIcon} ${esc(f.srcLabel)}</span>
                  <div style="flex:1;min-width:0"><div style="display:flex;align-items:baseline;gap:8px"><span style="font-weight:600;font-size:13px">${esc(f.title)}</span><div style="flex:1"></div><span style="font-family:'IBM Plex Mono';font-size:10.5px;color:var(--text-3)">${f.ago}</span></div><div style="font-size:12px;color:var(--text-2);margin-top:3px;line-height:1.5"><span style="color:var(--ai);font-weight:600">✦</span> ${esc(f.summary)}</div></div>
                </div>`).join('')}
            </div>

            <div style="display:flex;flex-direction:column;gap:18px">
              <div style="background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);overflow:hidden">
                <div style="padding:13px 16px;border-bottom:1px solid var(--border);font-weight:700;font-size:13.5px">Needs your review</div>
                <div style="display:flex;align-items:center;gap:10px;padding:11px 16px;border-bottom:1px solid var(--border)"><span style="width:22px;height:22px;border-radius:6px;background:var(--reg-bg);color:var(--reg);display:flex;align-items:center;justify-content:center;font-size:12px;flex:none">◈</span><div style="flex:1;min-width:0"><div style="font-size:12.5px;font-weight:600">mifid-ii.md</div><div style="font-size:11px;color:var(--text-3)">2 unresolved comments</div></div><span style="font-family:'IBM Plex Mono';font-size:10px;color:var(--reg)">M</span></div>
                <div style="display:flex;align-items:center;gap:10px;padding:11px 16px;border-bottom:1px solid var(--border)"><span style="width:22px;height:22px;border-radius:6px;background:var(--prod-bg);color:var(--prod);display:flex;align-items:center;justify-content:center;font-size:12px;flex:none">⑂</span><div style="flex:1;min-width:0"><div style="font-size:12.5px;font-weight:600">PR #128 · RTS 22 edits</div><div style="font-size:11px;color:var(--text-3)">you were requested</div></div><span ${on(v.setDiff)} style="font-size:11px;color:var(--prod);cursor:pointer;font-weight:600">Open</span></div>
                <div style="display:flex;align-items:center;gap:10px;padding:11px 16px"><span style="width:22px;height:22px;border-radius:6px;background:var(--data-bg);color:var(--data);display:flex;align-items:center;justify-content:center;font-size:12px;flex:none">⇄</span><div style="flex:1;min-width:0"><div style="font-size:12.5px;font-weight:600">trade.md mapping</div><div style="font-size:11px;color:var(--text-3)">1 drift to confirm</div></div></div>
              </div>
              <div style="background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);padding:14px 16px">
                <div style="font-weight:700;font-size:13.5px;margin-bottom:12px">Traceability health</div>
                <div style="display:flex;flex-direction:column;gap:11px">
                  ${v.dashHealth.map((h) => `
                    <div><div style="display:flex;justify-content:space-between;font-size:11.5px;margin-bottom:4px"><span style="color:var(--text-2)">${esc(h.label)}</span><span style="font-family:'IBM Plex Mono';font-weight:600">${h.pct}%</span></div><div style="height:6px;border-radius:3px;background:var(--surface-2);overflow:hidden"><div style="${h.barStyle}"></div></div></div>`).join('')}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>`;
  }

  tplChanges(v) {
    const on = (f) => this.on(f);
    return `
      <div style="flex:1;min-height:0;display:flex;background:var(--bg)">
        <div style="width:328px;flex:none;border-right:1px solid var(--border);background:var(--panel);display:flex;flex-direction:column">
          <div style="padding:12px 14px 10px;border-bottom:1px solid var(--border)">
            <div style="display:flex;align-items:center;gap:8px"><span style="font-weight:700;font-size:14px">Changes</span><span style="font-size:11px;color:var(--text-3)">${v.changeCount} open</span></div>
            <div style="display:flex;gap:4px;margin-top:10px;background:var(--surface-2);border:1px solid var(--border);border-radius:8px;padding:3px">
              <span ${on(v.setCfAll)} style="${v.cfAll}">All</span>
              <span ${on(v.setCfReg)} style="${v.cfReg}">Reg</span>
              <span ${on(v.setCfProd)} style="${v.cfProd}">Product</span>
              <span ${on(v.setCfTech)} style="${v.cfTech}">Tech</span>
            </div>
          </div>
          <div data-sk="chglist" style="flex:1;overflow-y:auto">
            ${v.changeItems.map((c) => `
              <div ${on(c.open)} style="${c.rowStyle}">
                <div style="display:flex;align-items:center;gap:7px"><span style="${c.srcChip}">${c.srcIcon} ${esc(c.srcLabel)}</span><div style="flex:1"></div><span style="font-family:'IBM Plex Mono';font-size:10px;color:var(--text-3)">${c.ago}</span></div>
                <div style="font-weight:600;font-size:12.5px;margin-top:7px">${esc(c.title)}</div>
                <div style="font-size:11.5px;color:var(--text-2);margin-top:3px;line-height:1.45;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden">${esc(c.summary)}</div>
                <div style="display:flex;align-items:center;gap:6px;margin-top:8px"><span style="${c.dotStyle}"></span><span style="font-size:10.5px;color:var(--text-2)">${esc(c.statusLabel)}</span></div>
              </div>`).join('')}
          </div>
        </div>
        <div data-sk="chgdetail" style="flex:1;min-width:0;overflow-y:auto;background:var(--surface)">
          ${v.changeSelected ? `
          <div style="max-width:680px;margin:0 auto;padding:26px 30px 60px">
            <div style="display:flex;align-items:center;gap:9px;flex-wrap:wrap"><span style="${v.selSrcChip}">${v.selSrcIcon} ${esc(v.selSrcLabel)}</span><span style="font-family:'IBM Plex Mono';font-size:11px;color:var(--text-3)">${esc(v.selRef)}</span><span style="font-family:'IBM Plex Mono';font-size:11px;color:var(--text-3)">· ${esc(v.selPublished)}</span></div>
            <h1 style="margin:14px 0 0;font-size:23px;font-weight:700;letter-spacing:-.4px">${esc(v.selTitle)}</h1>
            <div style="margin-top:16px;border:1px solid var(--ai-line);border-radius:11px;overflow:hidden">
              <div style="display:flex;align-items:center;gap:8px;padding:9px 14px;background:var(--ai-bg)">${ICONS.sparkAI(13)}<span style="font-size:12px;font-weight:600;color:var(--ai)">Copilot summary</span></div>
              <div style="padding:12px 14px;font-size:13px;line-height:1.65;color:var(--text)">${esc(v.selSummary)}</div>
            </div>
            <h2 style="margin:24px 0 10px;font-size:14px;font-weight:700;color:var(--text-2)">Impacted artifacts</h2>
            <div style="display:flex;flex-direction:column;gap:8px">
              ${v.selImpacts.map((a) => `
                <div ${on(a.open)} style="${a.style};cursor:pointer">
                  <span style="font-family:'IBM Plex Mono';font-size:11px;color:var(--prod);background:var(--prod-bg);padding:2px 7px;border-radius:5px">${esc(a.badge)}</span>
                  <span style="font-size:12.5px;font-weight:500">${esc(a.label)}</span>
                  <div style="flex:1"></div>
                  <span style="font-size:11px;font-weight:600;color:${a.tagColor}">${esc(a.tag)}</span>
                </div>`).join('')}
            </div>
            <div style="display:flex;gap:8px;margin-top:22px;flex-wrap:wrap">
              ${v.selHasDiff ? `<button ${on(v.openSelDiff)} style="height:34px;padding:0 15px;border:none;border-radius:8px;background:var(--ai);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">✦ Draft edits &amp; open as diff</button>` : ''}
              <button ${on(v.setGraph)} style="height:34px;padding:0 14px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">Open impact graph</button>
              <button style="height:34px;padding:0 14px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;cursor:pointer">Assign</button>
            </div>
          </div>` : ''}
        </div>
      </div>`;
  }

  tplMatrix(v) {
    const on = (f) => this.on(f);
    return `
      <div style="flex:1;min-height:0;display:flex;flex-direction:column;background:var(--bg)">
        <div style="flex:none;display:flex;align-items:center;gap:12px;padding:14px 20px;background:var(--surface);border-bottom:1px solid var(--border)">
          <div><div style="font-weight:700;font-size:15px">Traceability matrix</div><div style="font-size:11px;color:var(--text-3);margin-top:1px">Requirements × artifacts · coverage</div></div>
          <div style="flex:1"></div>
          <div style="display:flex;background:var(--surface-2);border:1px solid var(--border);border-radius:9px;padding:3px">
            <span ${on(v.setGraph)} style="padding:5px 14px;border-radius:6px;font-size:12px;font-weight:600;${v.segGraph}">Graph</span>
            <span style="padding:5px 14px;border-radius:6px;font-size:12px;font-weight:600;${v.segMatrix}">Matrix</span>
          </div>
        </div>
        <div data-sk="matrix" style="flex:1;overflow:auto">
          <div style="display:inline-block;min-width:100%;font-size:12px">
            <div style="position:sticky;top:0;z-index:4">
              <div style="display:flex;background:var(--surface);border-bottom:1px solid var(--border)">
                <div style="position:sticky;left:0;z-index:5;width:210px;flex:none;background:var(--surface);border-right:1px solid var(--border)"></div>
                ${v.mgroups.map((g) => `<div style="width:${g.width}px;flex:none;padding:9px 10px;border-right:1px solid var(--border);font-size:10px;font-weight:700;text-transform:uppercase;letter-spacing:.4px;color:${g.color};white-space:nowrap;overflow:hidden">${esc(g.label)}</div>`).join('')}
                <div style="width:120px;flex:none;background:var(--surface);border-left:1px solid var(--border)"></div>
              </div>
              <div style="display:flex;background:var(--surface);border-bottom:1px solid var(--border)">
                <div style="position:sticky;left:0;z-index:5;width:210px;flex:none;background:var(--surface);border-right:1px solid var(--border);display:flex;align-items:flex-end;padding:0 14px 9px;font-family:'IBM Plex Mono';font-size:10px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px">Requirement</div>
                ${v.mcolumns.map((c) => `<div style="width:26px;flex:none;height:120px;border-right:1px solid var(--border);display:flex;align-items:flex-end;justify-content:center;padding-bottom:9px;overflow:hidden"><span style="writing-mode:vertical-rl;transform:rotate(180deg);font-family:'IBM Plex Mono';font-size:10px;white-space:nowrap;color:var(--text-2)">${esc(c.label)}</span></div>`).join('')}
                <div style="width:120px;flex:none;background:var(--surface);border-left:1px solid var(--border);display:flex;align-items:flex-end;justify-content:flex-end;padding:0 14px 9px;font-family:'IBM Plex Mono';font-size:10px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px">Coverage</div>
              </div>
            </div>
            ${v.mrows.map((r) => `
              <div class="mrow" style="display:flex;border-bottom:1px solid var(--border)">
                <div style="position:sticky;left:0;z-index:3;width:210px;flex:none;background:var(--surface);border-right:1px solid var(--border);padding:8px 14px">
                  <div style="font-size:12.5px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(r.name)}</div>
                  <div style="font-family:'IBM Plex Mono';font-size:10px;color:var(--text-3)">${esc(r.id)}</div>
                </div>
                ${r.cells.map((cell) => `<div style="width:26px;flex:none;height:46px;border-right:1px solid var(--border);display:flex;align-items:center;justify-content:center"><span style="${cell.sq}"></span></div>`).join('')}
                <div style="width:120px;flex:none;background:var(--surface);border-left:1px solid var(--border);display:flex;align-items:center;gap:8px;padding:0 14px">
                  <div style="flex:1;height:5px;border-radius:3px;background:var(--surface-2);overflow:hidden"><div style="${r.covStyle}"></div></div>
                  <span style="font-family:'IBM Plex Mono';font-size:10.5px;width:28px;text-align:right">${r.cov}%</span>
                </div>
              </div>`).join('')}
          </div>
        </div>
        <div style="flex:none;display:flex;align-items:center;flex-wrap:wrap;gap:16px;padding:11px 20px;border-top:1px solid var(--border);background:var(--surface);font-size:11px;color:var(--text-2)">
          <span style="font-family:'IBM Plex Mono';color:var(--text-3)">${esc(v.matrixCaption)}</span>
          <div style="flex:1"></div>
          <span style="display:inline-flex;align-items:center;gap:6px"><span style="width:13px;height:13px;border-radius:4px;background:var(--data)"></span>linked</span>
          <span style="display:inline-flex;align-items:center;gap:6px"><span style="width:13px;height:13px;border-radius:4px;background:var(--reg)"></span>drift / stale</span>
          <span style="display:inline-flex;align-items:center;gap:6px"><span style="width:13px;height:13px;border-radius:4px;border:1.5px dashed var(--prod);box-sizing:border-box"></span>planned</span>
          <span style="display:inline-flex;align-items:center;gap:6px"><span style="width:5px;height:5px;border-radius:50%;background:var(--border-2)"></span>no link</span>
          <span style="margin-left:4px;color:var(--text-3)">Driver: <span style="color:var(--reg)">⚖</span> reg · <span style="color:var(--prod)">◆</span> product · <span style="color:var(--text-2)">⚙</span> tech</span>
        </div>
      </div>`;
  }

  tplModel(v) {
    const on = (f) => this.on(f);
    return `
      <div data-sk="model" style="flex:1;min-height:0;overflow-y:auto;background:var(--bg)">
        <div style="max-width:1000px;margin:0 auto;padding:28px 32px 64px">
          <div style="display:flex;align-items:flex-end;justify-content:space-between;gap:16px;flex-wrap:wrap">
            <div>
              <div style="font-family:'IBM Plex Mono';font-size:11.5px;color:var(--text-3)">.reqbase/config.yml · .reqbase/schema.json</div>
              <h1 style="margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px">Model definitions</h1>
              <div style="font-size:12.5px;color:var(--text-2);margin-top:5px;max-width:560px;line-height:1.5">The workspace taxonomy that everything else is computed from — edit the config files to change drivers, statuses, link types, or property schema.</div>
            </div>
            <div style="display:flex;gap:8px">
              <button ${on(v.openConfig)} style="height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">config.yml</button>
              <button ${on(v.openSchema)} style="height:32px;padding:0 13px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">schema.json</button>
            </div>
          </div>

          <div style="margin-top:24px;font-family:'IBM Plex Mono';font-size:10.5px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.5px">Entities</div>
          <div style="display:grid;grid-template-columns:repeat(5,1fr);gap:12px;margin-top:10px">
            ${v.modelEntities.map((e) => `
              <div ${on(e.open)} style="background:var(--surface);border:1px solid var(--border);border-radius:11px;padding:14px;box-shadow:var(--shadow);cursor:pointer">
                <div style="display:flex;align-items:center;gap:7px"><span style="${e.dot}"></span><span style="font-size:23px;font-weight:700;letter-spacing:-.5px">${e.count}</span></div>
                <div style="font-size:12.5px;font-weight:600;margin-top:6px">${esc(e.label)}</div>
                <div style="${e.kindStyle}">${esc(e.kind)}</div>
              </div>`).join('')}
          </div>

          <div style="display:grid;grid-template-columns:1fr 1fr;gap:18px;margin-top:24px;align-items:start">
            <div style="background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);padding:16px 18px">
              <div style="font-weight:700;font-size:13.5px">Drivers</div>
              <div style="font-size:11.5px;color:var(--text-2);margin-top:2px">What a requirement can be driven by. Regulatory is one of several.</div>
              <div style="display:flex;flex-wrap:wrap;gap:8px;margin-top:13px">
                ${v.modelDrivers.map((d) => `<span style="${d.chip}"><span style="${d.iconStyle}">${d.icon}</span>${esc(d.label)}</span>`).join('')}
              </div>
            </div>
            <div style="background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);padding:16px 18px">
              <div style="font-weight:700;font-size:13.5px">Statuses</div>
              <div style="font-size:11.5px;color:var(--text-2);margin-top:2px">Lifecycle states for requirements and specs.</div>
              <div style="display:flex;flex-wrap:wrap;gap:8px;margin-top:13px">
                ${v.modelStatuses.map((s) => `<span style="${s.style}"><span style="${s.dot}"></span>${esc(s.label)}</span>`).join('')}
              </div>
            </div>
          </div>

          <div style="background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);margin-top:18px;overflow:hidden">
            <div style="padding:14px 18px;border-bottom:1px solid var(--border)"><span style="font-weight:700;font-size:13.5px">Link types</span><span style="font-size:11.5px;color:var(--text-2);margin-left:8px">The typed edges the graph &amp; matrix are computed from</span></div>
            ${v.modelLinks.map((l) => `
              <div style="display:flex;align-items:center;gap:12px;padding:11px 18px;border-top:1px solid var(--border)">
                <span style="font-family:'IBM Plex Mono';font-size:12.5px;font-weight:600;color:var(--prod);width:110px;flex:none">${esc(l.name)}</span>
                <span style="font-family:'IBM Plex Mono';font-size:11.5px;padding:2px 8px;border-radius:5px;background:var(--surface-2);color:var(--text-2)">${esc(l.from)}</span>
                ${ICONS.arrow}
                <span style="font-family:'IBM Plex Mono';font-size:11.5px;padding:2px 8px;border-radius:5px;background:var(--surface-2);color:var(--text-2)">${esc(l.to)}</span>
              </div>`).join('')}
          </div>

          <div style="background:var(--surface);border:1px solid var(--border);border-radius:13px;box-shadow:var(--shadow);margin-top:18px;overflow:hidden">
            <div style="padding:14px 18px;border-bottom:1px solid var(--border);display:flex;align-items:center"><span style="font-weight:700;font-size:13.5px">Property schema</span><span style="font-size:11.5px;color:var(--text-2);margin-left:8px">Frontmatter fields — types &amp; enum values drive the Properties panel</span><div style="flex:1"></div><span ${on(v.openSchema)} style="font-size:11.5px;color:var(--prod);cursor:pointer;font-weight:600">Edit schema.json →</span></div>
            <div style="display:grid;grid-template-columns:160px 90px 1fr;padding:8px 18px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'IBM Plex Mono';font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px"><span>Field</span><span>Type</span><span>Enum values</span></div>
            ${v.modelSchemaFields.map((f) => `
              <div style="display:grid;grid-template-columns:160px 90px 1fr;align-items:center;padding:9px 18px;border-top:1px solid var(--border)">
                <span style="font-size:12.5px;font-weight:500">${esc(f.label)}</span>
                <span><span style="${f.typeStyle}">${esc(f.type)}</span></span>
                <span style="font-family:'IBM Plex Mono';font-size:11px;color:var(--text-2)">${esc(f.values)}</span>
              </div>`).join('')}
          </div>
        </div>
      </div>`;
  }

  tplDiff(v) {
    if (!v.diffReady) {
      return `<div style="flex:1;display:flex;align-items:center;justify-content:center;color:var(--text-3);font-family:'IBM Plex Mono';font-size:12px">loading diff…</div>`;
    }
    return `
      <div style="flex:1;min-height:0;display:flex;flex-direction:column;background:var(--bg)">
        <div style="flex:none;padding:14px 20px;background:var(--surface);border-bottom:1px solid var(--border)">
          <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
            <span style="font-family:'IBM Plex Mono';font-size:12px;color:var(--text-3)">#${esc(v.diffPr || '128')}</span>
            <h1 style="margin:0;font-size:16px;font-weight:700">${esc(v.diffTitle)}</h1>
            <span style="display:inline-flex;align-items:center;gap:5px;padding:3px 9px;border-radius:20px;background:var(--ai-bg);color:var(--ai);font-size:11px;font-weight:600"><svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/></svg>AI-drafted</span>
            <div style="flex:1"></div>
            <button style="height:32px;padding:0 14px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">Request changes</button>
            <button style="height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--data);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer">Approve &amp; merge</button>
          </div>
          <div style="display:flex;align-items:center;gap:12px;margin-top:11px;font-size:11.5px;color:var(--text-2);flex-wrap:wrap">
            <span style="font-family:'IBM Plex Mono';display:inline-flex;align-items:center;gap:5px"><span style="padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)">main</span>←<span style="padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)">${esc(v.diffBranch)}</span></span>
            <span style="display:inline-flex;align-items:center;gap:5px"><span style="width:14px;height:14px;border-radius:50%;background:var(--data);color:#fff;display:flex;align-items:center;justify-content:center;font-size:9px">✓</span>2 checks passing</span>
            <span>·</span>
            <span>Reviewers: <b style="color:var(--text)">S. Grant</b>, A. Okafor</span>
          </div>
        </div>
        <div style="flex:1;min-height:0;display:flex">
          <div style="width:224px;flex:none;border-right:1px solid var(--border);background:var(--panel);padding:12px 8px;overflow-y:auto">
            <div style="font-size:10px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;padding:0 8px 8px">${v.diffFiles.length} file${v.diffFiles.length === 1 ? '' : 's'} changed</div>
            ${v.diffFiles.map((f) => `
              <div style="display:flex;align-items:center;gap:8px;padding:7px 9px;border-radius:7px;font-size:12px;${f.active ? 'background:var(--surface);box-shadow:var(--shadow);font-weight:600' : 'color:var(--text-2)'}"><span style="color:${f.color}">${f.icon}</span><span style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${esc(f.name)}</span>${f.add ? `<span style="font-family:'IBM Plex Mono';font-size:10px;color:var(--add)">${f.add}</span>` : ''}${f.del ? `<span style="font-family:'IBM Plex Mono';font-size:10px;color:var(--del)">${f.del}</span>` : ''}</div>`).join('')}
          </div>
          <div data-sk="diffbody" style="flex:1;min-width:0;overflow-y:auto;background:var(--surface)">
            <div style="max-width:820px;margin:0 auto;padding:18px 22px 60px">
              <div style="border:1px solid var(--border);border-radius:11px;overflow:hidden">
                <div style="display:flex;align-items:center;gap:8px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'IBM Plex Mono';font-size:11.5px"><span style="color:var(--reg)">◈</span>${esc(v.diffFileHeader)}</div>
                <div style="font-family:'IBM Plex Mono';font-size:12px;line-height:1.85">
                  <div style="padding:4px 14px;background:var(--surface-2);color:var(--text-3);border-bottom:1px solid var(--border)">${esc(v.diffHunk)}</div>
                  ${v.diffLines.map((ln) => `<div style="display:flex;${ln.rowStyle}"><span style="width:26px;flex:none;text-align:center;user-select:none;color:${ln.signColor}">${ln.sign}</span><span style="flex:1;white-space:pre-wrap;color:${ln.textColor}">${esc(ln.text)}</span></div>`).join('')}
                </div>
                ${v.diffIsMifid ? `
                <div style="border-top:1px solid var(--border);padding:12px 14px;background:var(--surface-2);display:flex;gap:10px">
                  <div style="width:24px;height:24px;flex:none;border-radius:50%;background:linear-gradient(135deg,var(--ai),var(--prod));color:#fff;display:flex;align-items:center;justify-content:center;font-size:10px;font-weight:600">SG</div>
                  <div style="flex:1"><div style="font-size:12px"><b>S. Grant</b> <span style="color:var(--text-3);font-family:'IBM Plex Mono';font-size:10.5px">· just now</span></div><div style="font-size:12.5px;color:var(--text);margin-top:3px;line-height:1.5">Confirm the ARM accepts μs before we merge — otherwise gate behind a feature flag.</div></div>
                </div>` : ''}
              </div>
            </div>
          </div>
        </div>
      </div>`;
  }

  tplCopilot(v) {
    const on = (f) => this.on(f);
    return `
    <aside style="width:340px;flex:none;background:var(--surface);border-left:1px solid var(--border);display:flex;flex-direction:column">
      <div style="height:46px;flex:none;display:flex;align-items:center;gap:9px;padding:0 14px;border-bottom:1px solid var(--border)">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--ai)" stroke-width="1.8"><path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/></svg>
        <span style="font-weight:700;font-size:13.5px">Copilot</span>
        <span style="display:inline-flex;align-items:center;gap:4px;font-size:10px;color:var(--text-2);background:var(--surface-2);border:1px solid var(--border);padding:2px 7px;border-radius:20px"><span style="width:5px;height:5px;border-radius:50%;background:var(--data);animation:pulse 2s infinite"></span>grounded on repo</span>
        <div style="flex:1"></div>
        <span ${on(v.toggleCopilot)} style="color:var(--text-3);cursor:pointer">⌵</span>
      </div>

      <div data-sk="copilot" style="flex:1;overflow-y:auto;padding:14px;display:flex;flex-direction:column;gap:14px">
        <div style="display:flex;align-items:center;gap:6px;flex-wrap:wrap;font-family:'IBM Plex Mono';font-size:10.5px">
          <span style="color:var(--text-3)">Context</span>
          <span style="padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)">@mifid-ii.md</span>
          <span style="padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)">repo:trading-specs</span>
          <span style="padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border);color:var(--text-2)">+3 docs</span>
        </div>

        <div style="border:1px solid var(--reg-line);border-radius:11px;overflow:hidden;background:var(--surface)">
          <div style="display:flex;align-items:center;gap:8px;padding:9px 13px;background:var(--reg-bg)">
            <span style="font-size:13px">⚖</span><span style="font-size:12px;font-weight:700;color:var(--reg)">Regulatory change detected</span><div style="flex:1"></div><span style="font-family:'IBM Plex Mono';font-size:10px;color:var(--reg)">${esc(v.changePublished)}</span>
          </div>
          <div style="padding:11px 13px;font-size:12.5px;line-height:1.6;color:var(--text)">
            ${esc(v.changeSummary)}
            <div style="margin-top:8px;display:flex;flex-direction:column;gap:5px;font-size:12px;color:var(--text-2)">
              <div style="display:flex;gap:7px"><span style="color:var(--reg)">›</span>4 specs impacted</div>
              <div style="display:flex;gap:7px"><span style="color:var(--reg)">›</span>2 data mappings drifted</div>
              <div style="display:flex;gap:7px"><span style="color:var(--reg)">›</span>1 requirement needs update</div>
            </div>
          </div>
        </div>

        <div style="display:flex;gap:10px">
          <div style="width:24px;height:24px;flex:none;border-radius:7px;background:var(--ai-bg);display:flex;align-items:center;justify-content:center"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="var(--ai)" stroke-width="1.9"><path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/></svg></div>
          <div style="flex:1;font-size:12.5px;line-height:1.62;color:var(--text)">
            I mapped the amendment against the repo. The timestamp change breaks <span style="font-family:'IBM Plex Mono';font-size:11px;color:var(--data);background:var(--data-bg);padding:1px 5px;border-radius:4px">trade.executionTimestamp</span> and touches REQ-042. I can draft the spec edit and the mapping fix for you to review.
            <div style="margin-top:11px;display:flex;flex-direction:column;gap:6px">
              <button ${on(v.setDiff)} style="display:flex;align-items:center;gap:8px;padding:8px 11px;border:1px solid var(--ai-line);border-radius:8px;background:var(--ai-bg);color:var(--ai);font-family:inherit;font-size:12px;font-weight:600;cursor:pointer;text-align:left">✦ Draft edits &amp; open as diff</button>
              <button ${on(v.setGraph)} style="display:flex;align-items:center;gap:8px;padding:8px 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px;cursor:pointer;text-align:left">Open impact graph</button>
              <button style="display:flex;align-items:center;gap:8px;padding:8px 11px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12px;cursor:pointer;text-align:left">Show affected mappings</button>
            </div>
          </div>
        </div>

        <div style="display:flex;flex-wrap:wrap;gap:6px;padding-left:34px">
          <span style="padding:5px 10px;border:1px solid var(--border);border-radius:20px;font-size:11.5px;color:var(--text-2);cursor:pointer;background:var(--surface-2)">Which teams to notify?</span>
          <span style="padding:5px 10px;border:1px solid var(--border);border-radius:20px;font-size:11.5px;color:var(--text-2);cursor:pointer;background:var(--surface-2)">Compare to GDPR spec</span>
        </div>
      </div>

      <div style="flex:none;padding:12px 14px;border-top:1px solid var(--border)">
        <div style="border:1px solid var(--border-2);border-radius:11px;background:var(--surface-2);padding:9px 11px">
          <div style="display:flex;align-items:center;gap:6px;margin-bottom:8px;font-family:'IBM Plex Mono';font-size:10px"><span style="padding:2px 6px;border-radius:5px;background:var(--surface);border:1px solid var(--border);color:var(--text-2)">@ mifid-ii.md</span></div>
          <div style="display:flex;align-items:flex-end;gap:8px">
            <span style="flex:1;font-size:12.5px;color:var(--text-3)">Ask about requirements, changes, mappings…</span>
            <button style="width:28px;height:28px;flex:none;border:none;border-radius:8px;background:var(--ai);color:#fff;display:flex;align-items:center;justify-content:center;cursor:pointer">${ICONS.send}</button>
          </div>
        </div>
      </div>
    </aside>`;
  }
}

if (typeof document !== 'undefined') {
  window.__reqbase = new App(document.getElementById('app'));
}
