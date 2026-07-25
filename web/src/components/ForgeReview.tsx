import { sx } from '../lib/sx';
import { useForgeRequest } from '../api/hooks';

/**
 * Review feedback from the forge (GitLab/GitHub) for the current branch.
 *
 * SpecQuill merges directly and has no in-app review, so when a team reviews
 * on the forge instead, this is where that conversation shows up. Read-only:
 * replies happen on the forge, which is what the title links to. Renders
 * nothing unless the project opted in and the branch has an open request.
 */
export function ForgeReview({ repo, branch }: { repo: string | undefined; branch: string }) {
  const forge = useForgeRequest(repo, branch);
  const data = forge.data;
  if (!data?.enabled) return null;

  if (data.error) {
    return (
      <div style={sx('border:1px solid var(--reg-line);background:var(--reg-bg);border-radius:11px;padding:10px 14px;margin-top:18px;font-size:12px;color:var(--reg)')}>
        Merge request comments unavailable — {data.error}
      </div>
    );
  }
  const req = data.request;
  if (!req) return null;

  return (
    <div style={sx('border:1px solid var(--border);border-radius:11px;overflow:hidden;background:var(--surface);margin-top:18px')}>
      <div style={sx('display:flex;align-items:center;gap:9px;padding:9px 14px;background:var(--surface-2);border-bottom:1px solid var(--border)')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:10px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.4px")}>
          Review on the forge
        </span>
        <span style={sx('flex:1')} />
        <span style={sx('font-size:10.5px;font-weight:600;padding:2px 8px;border-radius:99px;background:var(--prod-bg);color:var(--prod)')}>{req.state}</span>
      </div>
      <a href={req.url} target="_blank" rel="noreferrer"
        style={sx('display:flex;align-items:baseline;gap:8px;padding:11px 14px;text-decoration:none;color:var(--text);border-bottom:1px solid var(--border)')}>
        <span style={sx("font-family:'JetBrains Mono',monospace;font-size:12px;color:var(--text-3)")}>!{req.number}</span>
        <span style={sx('font-size:13px;font-weight:600;flex:1')}>{req.title}</span>
        <span style={sx('font-size:11.5px;color:var(--text-3)')}>opened by {req.author}</span>
      </a>
      {req.comments.length === 0 ? (
        <div style={sx('padding:11px 14px;font-size:12px;color:var(--text-3)')}>No comments yet.</div>
      ) : (
        req.comments.map((c, i) => (
          <div key={i} style={sx('display:flex;gap:10px;padding:11px 14px;border-bottom:1px solid var(--border)')}>
            <div style={sx('width:24px;height:24px;flex:none;border-radius:50%;background:linear-gradient(135deg,var(--ai),var(--prod));color:#fff;display:flex;align-items:center;justify-content:center;font-size:10px;font-weight:600')}>
              {c.author.slice(0, 2).toUpperCase()}
            </div>
            <div style={sx('flex:1;min-width:0')}>
              <div style={sx('font-size:12px')}>
                <b>{c.author}</b>
                {c.path && (
                  <span style={sx("color:var(--text-3);font-family:'JetBrains Mono',monospace;font-size:10.5px")}>
                    {' · '}{c.path}{c.line ? `:${c.line}` : ''}
                  </span>
                )}
              </div>
              <div style={sx('font-size:12.5px;color:var(--text);margin-top:3px;line-height:1.5;white-space:pre-wrap')}>{c.body}</div>
            </div>
          </div>
        ))
      )}
    </div>
  );
}
