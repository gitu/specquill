import { useNavigate, useSearchParams } from 'react-router-dom';
import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { diffLines } from '../lib/derive';
import { scalar, stripFrontmatter } from '../lib/model';
import { Loading } from './Dashboard';
import { IconSpark } from '../components/icons';

// M2: renders the proposed-edit diff embedded in a change record.
// M7 replaces this with real branch-vs-branch PR diffs from the server.
export function DiffView() {
  const nav = useNavigate();
  const app = useApp();
  const [params] = useSearchParams();
  if (!app.model) return <Loading />;

  const changePath = params.get('change') || 'changes/2026-06-mifid-rts22.md';
  const c = app.model.changes.find((x) => x.path === changePath) || app.model.changes.find((x) => !!x.diff);
  if (!c) return <Loading />;
  const raw = app.files?.[c.path] || '';
  const fm = stripFrontmatter(raw).fm;
  const pr = scalar(fm, 'pr');
  const branch = scalar(fm, 'branch');
  const isMifid = c.path === 'changes/2026-06-mifid-rts22.md';
  const files = isMifid
    ? [
        { icon: '◈', color: 'var(--reg)', name: 'mifid-ii.md', add: '+8', del: '−3', active: true },
        { icon: '⇄', color: 'var(--data)', name: 'trade.md', add: '+2', del: '−2', active: false },
        { icon: '▤', color: 'var(--prod)', name: 'REQ-042.md', add: '+4', del: '', active: false },
      ]
    : [
        ...c.impSpecs.map((s, i) => ({ icon: '◈', color: 'var(--reg)', name: s.split('/').pop()!, add: '', del: '', active: i === 0 })),
        ...c.impMaps.map((mp) => ({ icon: '⇄', color: 'var(--data)', name: (mp.split('#')[0] || mp).split('/').pop()!, add: '', del: '', active: false })),
        ...c.impReqs.map((r) => ({ icon: '▤', color: 'var(--prod)', name: r + '.md', add: '', del: '', active: false })),
      ];
  if (files.length && !files.some((f) => f.active)) files[0].active = true;
  const lines = diffLines(c.diff || '');

  return (
    <div style={sx('flex:1;min-height:0;display:flex;flex-direction:column;background:var(--bg)')}>
      <div style={sx('flex:none;padding:14px 20px;background:var(--surface);border-bottom:1px solid var(--border)')}>
        <div style={sx('display:flex;align-items:center;gap:10px;flex-wrap:wrap')}>
          <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12px;color:var(--text-3)")}>#{pr && pr !== 'null' ? pr : '—'}</span>
          <h1 style={sx('margin:0;font-size:16px;font-weight:700')}>{c.title}</h1>
          <span style={sx('display:inline-flex;align-items:center;gap:5px;padding:3px 9px;border-radius:20px;background:var(--ai-bg);color:var(--ai);font-size:11px;font-weight:600')}>
            <IconSpark size={11} width={2} />AI-drafted
          </span>
          <div style={sx('flex:1')} />
          <button style={sx('height:32px;padding:0 14px;border:1px solid var(--border-2);border-radius:8px;background:var(--surface);color:var(--text);font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>Request changes</button>
          <button style={sx('height:32px;padding:0 15px;border:none;border-radius:8px;background:var(--data);color:#fff;font-family:inherit;font-size:12.5px;font-weight:600;cursor:pointer')}>Approve &amp; merge</button>
        </div>
        <div style={sx('display:flex;align-items:center;gap:12px;margin-top:11px;font-size:11.5px;color:var(--text-2);flex-wrap:wrap')}>
          <span style={sx("font-family:'JetBrains Mono',monospace;display:inline-flex;align-items:center;gap:5px")}>
            <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)')}>main</span>←
            <span style={sx('padding:2px 7px;border-radius:5px;background:var(--surface-2);border:1px solid var(--border)')}>{branch && branch !== 'null' ? branch : 'feature/…'}</span>
          </span>
          <span style={sx('display:inline-flex;align-items:center;gap:5px')}>
            <span style={sx('width:14px;height:14px;border-radius:50%;background:var(--data);color:#fff;display:flex;align-items:center;justify-content:center;font-size:9px')}>✓</span>2 checks passing
          </span>
          <span>·</span>
          <span>Reviewers: <b style={sx('color:var(--text)')}>S. Grant</b>, A. Okafor</span>
        </div>
      </div>
      <div style={sx('flex:1;min-height:0;display:flex')}>
        <div style={sx('width:224px;flex:none;border-right:1px solid var(--border);background:var(--panel);padding:12px 8px;overflow-y:auto')}>
          <div style={sx('font-size:10px;font-weight:700;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px;padding:0 8px 8px')}>
            {files.length} file{files.length === 1 ? '' : 's'} changed
          </div>
          {files.map((f, i) => (
            <div key={i} style={sx('display:flex;align-items:center;gap:8px;padding:7px 9px;border-radius:7px;font-size:12px;' + (f.active ? 'background:var(--surface);box-shadow:var(--shadow);font-weight:600' : 'color:var(--text-2)'))}>
              <span style={{ color: f.color }}>{f.icon}</span>
              <span style={sx('flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap')}>{f.name}</span>
              {f.add && <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--add)")}>{f.add}</span>}
              {f.del && <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;color:var(--del)")}>{f.del}</span>}
            </div>
          ))}
        </div>
        <div style={sx('flex:1;min-width:0;overflow-y:auto;background:var(--surface)')}>
          <div style={sx('max-width:820px;margin:0 auto;padding:18px 22px 60px')}>
            <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden')}>
              <div style={sx("display:flex;align-items:center;gap:8px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border);font-family:'JetBrains Mono',monospace;font-size:11.5px")}>
                <span style={sx('color:var(--reg)')}>◈</span>{isMifid ? 'regulations/mifid-ii.md' : (c.impSpecs[0] || c.path)}
              </div>
              <div style={sx("font-family:'JetBrains Mono',monospace;font-size:12px;line-height:1.85")}>
                <div style={sx('padding:4px 14px;background:var(--surface-2);color:var(--text-3);border-bottom:1px solid var(--border)')}>@@ {c.path} @@</div>
                {lines.map((ln, i) => (
                  <div key={i} style={sx('display:flex;' + ln.rowStyle)}>
                    <span style={{ ...sx('width:26px;flex:none;text-align:center;user-select:none'), color: ln.signColor }}>{ln.sign}</span>
                    <span style={{ ...sx('flex:1;white-space:pre-wrap'), color: ln.textColor }}>{ln.text}</span>
                  </div>
                ))}
              </div>
              {isMifid && (
                <div style={sx('border-top:1px solid var(--border);padding:12px 14px;background:var(--surface-2);display:flex;gap:10px')}>
                  <div style={sx('width:24px;height:24px;flex:none;border-radius:50%;background:linear-gradient(135deg,var(--ai),var(--prod));color:#fff;display:flex;align-items:center;justify-content:center;font-size:10px;font-weight:600')}>SG</div>
                  <div style={sx('flex:1')}>
                    <div style={sx('font-size:12px')}><b>S. Grant</b> <span style={sx("color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:10.5px")}>· just now</span></div>
                    <div style={sx('font-size:12.5px;color:var(--text);margin-top:3px;line-height:1.5')}>Confirm the ARM accepts μs before we merge — otherwise gate behind a feature flag.</div>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
