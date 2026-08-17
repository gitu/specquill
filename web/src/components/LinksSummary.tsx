import { sx } from '../lib/sx';
import { useNav } from '../state/nav';

/**
 * Compact Overview pointer for the Links page — link checking is on-demand
 * (no standing state to summarize), so this only names what lives there.
 */
export function LinksSummary() {
  const nav = useNav();
  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface)')}>
      <div style={sx('display:flex;align-items:center;gap:9px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border)')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
          Links
        </span>
      </div>
      <div onClick={() => nav('/links')} style={sx('padding:11px 14px;cursor:pointer')}>
        <div style={sx('font-size:12px;color:var(--text-2)')}>link suggestions (AI) · link health (internal · sources · external)</div>
        <div style={sx('font-size:11px;color:var(--prod);font-weight:600;margin-top:6px')}>Open links →</div>
      </div>
    </div>
  );
}
