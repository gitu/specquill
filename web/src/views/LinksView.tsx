import { sx } from '../lib/sx';
import { useApp } from '../state/AppContext';
import { LinkerCard } from '../components/LinkerCard';
import { LinkCheckCard } from '../components/LinkCheck';

/**
 * Links as their own page: AI link detection (missing typed links between
 * documents, per the configured link types) and link health (internal,
 * ~source and external link verification). The Overview keeps only a
 * compact pointer card.
 */
export function LinksView() {
  const app = useApp();
  return (
    <div style={sx('flex:1;min-height:0;overflow-y:auto;background:var(--bg)')}>
      <div style={sx('max-width:860px;margin:0 auto;padding:28px 32px 64px')}>
        <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11.5px;color:var(--text-3)")}>{app.repoId} · {app.branch}</div>
        <h1 style={sx('margin:5px 0 0;font-size:25px;font-weight:700;letter-spacing:-.5px')}>Links</h1>
        <div style={sx('font-size:12.5px;color:var(--text-2);margin-top:6px;line-height:1.5')}>
          Detect missing typed links between documents (drivers, implements, …) and verify that every
          written link — internal, ~source and external — still resolves.
        </div>

        <div style={sx('display:flex;flex-direction:column;gap:18px;margin-top:22px')}>
          <LinkerCard repo={app.repoId} branch={app.branch} />
          <LinkCheckCard />
        </div>
      </div>
    </div>
  );
}
