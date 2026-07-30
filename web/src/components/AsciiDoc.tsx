import { useEffect, useState } from 'react';
import DOMPurify from 'dompurify';
import { sx } from '../lib/sx';
import { esc } from '../lib/model';
import { DocBody } from './DocBody';

/**
 * AsciiDoc rendering via asciidoctor.js — a heavy Opal-transpiled bundle, so
 * it loads as its own chunk only when an .adoc file is actually opened.
 * `safe: 'secure'` disables includes and unsafe macros (reference repos are
 * third-party content), and the HTML is sanitized before it hits the DOM —
 * unlike our markdown path, asciidoctor emits author-controlled raw blocks.
 */
export function AsciiDoc({ raw, docPath }: { raw: string; docPath: string }) {
  const [html, setHtml] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    import('@asciidoctor/core')
      .then(async ({ convert }) => {
        const out = await convert(raw, {
          safe: 'secure',
          attributes: { showtitle: true, 'source-highlighter': '' },
        });
        if (!live) return;
        setHtml(DOMPurify.sanitize(String(out)));
      })
      .catch(() => setHtml('<pre style="white-space:pre-wrap"><code>' + esc(raw) + '</code></pre>'));
    return () => { live = false; };
  }, [raw]);
  if (html === null) {
    return <div style={sx("font-family:'JetBrains Mono',monospace;font-size:11px;color:var(--text-3)")}>rendering {docPath}…</div>;
  }
  return <DocBody html={html} docPath={docPath} />;
}
