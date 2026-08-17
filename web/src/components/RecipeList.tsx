import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { sx } from '../lib/sx';
import { projectPath } from '../state/nav';
import { useWorkspace } from '../hooks/useWorkspace';
import { api } from '../api/client';
import { AlignmentRecipe, useAlignmentRecipes } from '../api/hooks';

/**
 * The pipelines this branch can run — the three shipped ones and the project's
 * own, side by side because they are the same kind of thing.
 *
 * This exists because a recipe lives under `.specquill/alignment/`, which the
 * document tree hides behind its all-files toggle. Without a surface here, the
 * only evidence the feature exists is a dropdown you have to already know to
 * look in, and a project with no recipes yet gets no hint at all.
 */
export function RecipeList({ repo, branch, selected, onSelect }: {
  repo: string | undefined; branch: string;
  selected: string; onSelect: (slug: string) => void;
}) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const recipes = useAlignmentRecipes(repo, branch);
  const { ensureWritableBranch } = useWorkspace();
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [naming, setNaming] = useState(false);
  const [slug, setSlug] = useState('my-audit');

  const data = recipes.data;
  const dir = data?.dir ?? '.specquill/alignment/';
  const errors = Object.entries(data?.errors ?? {});
  const shipped = (data?.recipes ?? []).filter((r) => r.builtin);
  const mine = (data?.recipes ?? []).filter((r) => !r.builtin);

  // a new recipe is an ordinary document: written to the workspace branch and
  // opened in the ordinary editor. The starter comes from the server, so the
  // format has exactly one definition.
  const create = () => {
    setErr('');
    const clean = slug.trim().toLowerCase().replace(/[^a-z0-9-]+/g, '-').replace(/^-|-$/g, '');
    if (!clean) { setErr('name the recipe first'); return; }
    if ((data?.recipes ?? []).some((r) => r.slug === clean)) {
      setErr(`${clean} already exists`); return;
    }
    const path = dir + clean + '.md';
    const content = (data?.starter ?? '').replace(/my-audit/g, clean).replace(/My audit/g, title(clean));
    setBusy(true);
    // a recipe is an ordinary document — created on the workspace branch like
    // any other, and opened in the ordinary editor
    void ensureWritableBranch().then(async (on) => {
      await api<{ sha: string }>(
        `/api/repos/${repo}/files/${path}?branch=${encodeURIComponent(on)}`,
        { method: 'PUT', body: JSON.stringify({ content, baseSha: '' }) });
      await qc.invalidateQueries({ queryKey: ['recipes', repo] });
      qc.invalidateQueries({ queryKey: ['status', repo] });
      qc.invalidateQueries({ queryKey: ['snapshot', repo] });
      setNaming(false);
      setBusy(false);
      navigate(projectPath(repo, '/editor/' + path, on));
    }).catch((e) => { setErr(String((e as Error).message ?? e)); setBusy(false); });
  };

  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
      <div style={sx('display:flex;align-items:center;gap:9px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border)')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
          Alignment recipes
        </span>
        <span style={sx('flex:1')} />
        {!naming && (
          <button data-recipe-new onClick={() => { setNaming(true); setErr(''); }}
            style={sx('height:24px;padding:0 10px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none')}>
            ＋ New recipe
          </button>
        )}
      </div>

      <div style={sx('padding:10px 14px;font-size:11.5px;color:var(--text-2);line-height:1.55;border-bottom:1px solid var(--border)')}>
        A recipe is the pipeline a run executes: which sources or documents it
        walks, what it asks the model at each stage, and what it calls a
        finding. The three shipped ones are recipes like any other — start from
        one with ＋ New recipe and rewrite the prompts.
        {' '}Your own live in <code style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3)")}>{dir}</code>{' '}
        in this repository, so they are reviewed and versioned like every other document.
      </div>

      {naming && (
        <div style={sx('display:flex;align-items:center;gap:7px;padding:9px 14px;border-bottom:1px solid var(--border);background:var(--surface-2)')}>
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);flex:none")}>{dir}</span>
          <input data-recipe-slug value={slug} autoFocus spellCheck={false}
            onChange={(e) => setSlug(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') create(); if (e.key === 'Escape') setNaming(false); }}
            style={sx("width:170px;height:24px;padding:0 8px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:'JetBrains Mono',monospace;font-size:10.5px")} />
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--text-3);flex:none")}>.md</span>
          <span style={sx('flex:1')} />
          <button data-recipe-create onClick={create} disabled={busy}
            style={sx('height:24px;padding:0 11px;border:none;border-radius:6px;background:var(--text);color:var(--bg);font-family:inherit;font-size:11px;font-weight:600;cursor:pointer;flex:none')}>
            {busy ? 'Creating…' : 'Create and edit'}
          </button>
          <button onClick={() => setNaming(false)}
            style={sx('height:24px;padding:0 8px;border:none;background:none;color:var(--text-3);font-family:inherit;font-size:11px;cursor:pointer;flex:none')}>
            cancel
          </button>
        </div>
      )}
      {err && (
        <div style={sx('padding:8px 14px;font-size:11.5px;color:var(--reg);background:var(--reg-bg);border-bottom:1px solid var(--border)')}>{err}</div>
      )}

      {/* a recipe that IS there but does not parse — say so, or it is simply
          absent from every picker and nobody can tell why */}
      {errors.map(([bad, msg]) => (
        <div key={bad} style={sx('padding:9px 14px;border-bottom:1px solid var(--border);background:var(--reg-bg)')}>
          <div onClick={() => navigate(projectPath(repo, '/editor/' + dir + bad + '.md', branch))}
            style={sx("font-family:'JetBrains Mono',monospace;font-size:10.5px;color:var(--reg);cursor:pointer;font-weight:600")}>
            {dir}{bad}.md
          </div>
          <div style={sx('font-size:11px;color:var(--reg);margin-top:3px;line-height:1.5')}>{msg}</div>
        </div>
      ))}

      <Group label="This project" empty="none yet — ＋ New recipe starts one from a working example">
        {mine.map((r) => (
          <Row key={r.slug} r={r} selected={selected === r.slug} onSelect={onSelect}
            onOpen={() => navigate(projectPath(repo, '/editor/' + r.path, branch))} />
        ))}
      </Group>
      <Group label="Shipped">
        {shipped.map((r) => (
          <Row key={r.slug} r={r} selected={selected === r.slug} onSelect={onSelect} />
        ))}
      </Group>

      {(data?.models?.length ?? 0) > 0 && (
        <div style={sx("padding:8px 14px;font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);line-height:1.6")}>
          models a stage may name: {data!.models.join(', ')} · ceiling {data!.maxCallsPerRun} calls per run
        </div>
      )}
    </div>
  );
}

function Group({ label, empty, children }: {
  label: string; empty?: string; children: React.ReactNode[];
}) {
  return (
    <>
      <div style={sx("padding:7px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
        {label}
      </div>
      {children.length === 0 && empty && (
        <div style={sx('padding:9px 14px;font-size:11.5px;color:var(--text-3);border-bottom:1px solid var(--border)')}>{empty}</div>
      )}
      {children}
    </>
  );
}

/** One recipe: what it walks, what it asks, and what it calls a finding. */
function Row({ r, selected, onSelect, onOpen }: {
  r: AlignmentRecipe; selected: boolean; onSelect: (slug: string) => void; onOpen?: () => void;
}) {
  return (
    <div data-recipe={r.slug}
      style={sx('padding:10px 14px;border-bottom:1px solid var(--border);background:' + (selected ? 'var(--ai-bg)' : 'transparent'))}>
      <div style={sx('display:flex;align-items:baseline;gap:8px')}>
        <span style={sx('font-size:12.5px;font-weight:600;flex:none')}>{r.name}</span>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);flex:none")}>{r.slug}</span>
        <span style={sx('flex:1;min-width:0;font-size:11.5px;color:var(--text-2);overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>
          {r.description}
        </span>
        <button data-recipe-use onClick={() => onSelect(r.slug)}
          title="Select this pipeline in the run controls above"
          style={sx('height:23px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--text);font-family:inherit;font-size:10.5px;font-weight:600;cursor:pointer;flex:none')}>
          {selected ? 'Selected' : 'Use'}
        </button>
        {onOpen && (
          <button data-recipe-open onClick={onOpen} title="Open the recipe document in the editor"
            style={sx("height:23px;padding:0 9px;border:1px solid var(--border-2);border-radius:6px;background:var(--surface);color:var(--prod);font-family:'JetBrains Mono',monospace;font-size:10.5px;cursor:pointer;flex:none")}>
            ✎ edit
          </button>
        )}
      </div>
      <div style={sx("margin-top:5px;font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--text-3);line-height:1.6")}>
        walks {r.units === 'docs' ? 'documents' : 'sources'} → {r.stages.map((st) => st.label || st.id).join(' → ')}
        {r.output === 'extraction' ? ' → inventory' : ''}
        {r.files.include?.length ? <><br />files: {r.files.include.join(', ')}{r.files.exclude?.length ? ' · not ' + r.files.exclude.join(', ') : ''}</> : null}
        {r.files.describe ? <><br />selects “{r.files.describe}”</> : null}
        {r.path ? <><br />{r.path}</> : null}
      </div>
      {r.findings.length > 0 && (
        <div style={sx('display:flex;flex-wrap:wrap;gap:4px;margin-top:6px')}>
          {r.findings.map((k) => (
            <span key={k.kind} title={k.label + (k.draftable ? ' — proposes a new document' : '')}
              style={sx('padding:2px 7px;border-radius:6px;font-size:10px;font-weight:600;background:var(--surface-2);color:var(--text-3)')}>
              {k.kind}{k.draftable ? ' ✎' : ''}
            </span>
          ))}
        </div>
      )}
      {r.warnings.map((w, i) => (
        <div key={i} style={sx('margin-top:5px;font-size:10.5px;color:var(--prod)')}>⚠ {w}</div>
      ))}
    </div>
  );
}

function title(slug: string) {
  const s = slug.replace(/-/g, ' ');
  return s.charAt(0).toUpperCase() + s.slice(1);
}
