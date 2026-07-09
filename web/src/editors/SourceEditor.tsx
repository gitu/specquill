import { useEffect, useRef } from 'react';
import { EditorState, StateEffect, StateField, RangeSet, RangeSetBuilder } from '@codemirror/state';
import { EditorView, keymap, lineNumbers, highlightActiveLine, gutter, GutterMarker } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { markdown } from '@codemirror/lang-markdown';
import { yaml } from '@codemirror/lang-yaml';
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language';
import { tags } from '@lezer/highlight';
import { diffLinesLCS } from '../lib/linediff';

// ---- changed-line gutter (VS Code-style stripes vs the committed baseline)

class ChangeMarker extends GutterMarker {
  constructor(private color: string) { super(); }
  eq(other: ChangeMarker) { return other.color === this.color; }
  toDOM() {
    const el = document.createElement('div');
    el.style.cssText = `width:3px;height:100%;background:${this.color};border-radius:2px`;
    return el;
  }
}
const ADDED = new ChangeMarker('var(--add)');
const CHANGED = new ChangeMarker('var(--prod)');
const REMOVED = new ChangeMarker('var(--del)');

const setChangeMarkers = StateEffect.define<RangeSet<GutterMarker>>();
const changeMarkersField = StateField.define<RangeSet<GutterMarker>>({
  create: () => RangeSet.empty,
  update(value, tr) {
    for (const e of tr.effects) if (e.is(setChangeMarkers)) return e.value;
    return tr.docChanged ? value.map(tr.changes) : value;
  },
});

function computeMarkers(state: EditorState, baseline: string): RangeSet<GutterMarker> {
  const d = diffLinesLCS(baseline, state.doc.toString());
  const builder = new RangeSetBuilder<GutterMarker>();
  for (let ln = 1; ln <= state.doc.lines; ln++) {
    const from = state.doc.line(ln).from;
    if (d.changed.has(ln)) builder.add(from, from, CHANGED);
    else if (d.added.has(ln)) builder.add(from, from, ADDED);
    else if (d.removedAt.has(ln - 1)) builder.add(from, from, REMOVED);
  }
  return builder.finish();
}

function changeGutter(getBaseline: () => string | undefined) {
  let timer: ReturnType<typeof setTimeout>;
  return [
    changeMarkersField,
    gutter({ class: 'cm-changes-gutter', markers: (view) => view.state.field(changeMarkersField) }),
    EditorView.updateListener.of((u) => {
      if (!u.docChanged) return;
      clearTimeout(timer);
      timer = setTimeout(() => {
        const baseline = getBaseline();
        if (baseline === undefined) return;
        u.view.dispatch({ effects: setChangeMarkers.of(computeMarkers(u.view.state, baseline)) });
      }, 300);
    }),
  ];
}

// theme built from the design's CSS custom properties
const cmTheme = EditorView.theme({
  '&': { fontSize: '12.5px', backgroundColor: 'var(--surface)', color: 'var(--text)' },
  '.cm-content': { fontFamily: "'JetBrains Mono', monospace", lineHeight: '1.8', padding: '10px 0' },
  '.cm-gutters': { backgroundColor: 'var(--surface-2)', color: 'var(--text-3)', border: 'none', borderRight: '1px solid var(--border)', fontFamily: "'JetBrains Mono', monospace", fontSize: '11px' },
  '.cm-activeLine': { backgroundColor: 'var(--surface-2)' },
  '.cm-activeLineGutter': { backgroundColor: 'var(--surface-2)' },
  '&.cm-focused': { outline: 'none' },
  '.cm-cursor': { borderLeftColor: 'var(--text)' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: 'var(--prod-bg) !important' },
});

const cmHighlight = syntaxHighlighting(HighlightStyle.define([
  { tag: tags.heading, color: 'var(--reg)', fontWeight: '600' },
  { tag: tags.quote, color: 'var(--text-2)' },
  { tag: tags.list, color: 'var(--text-2)' },
  { tag: tags.monospace, color: 'var(--ai)' },
  { tag: tags.link, color: 'var(--prod)' },
  { tag: tags.url, color: 'var(--prod)' },
  { tag: tags.strong, fontWeight: '700' },
  { tag: tags.emphasis, fontStyle: 'italic' },
  { tag: tags.propertyName, color: 'var(--prod)' },
  { tag: tags.string, color: 'var(--data)' },
  { tag: tags.comment, color: 'var(--text-3)' },
  { tag: tags.meta, color: 'var(--text-3)' },
]));

export function SourceEditor({ value, lang, onChange, readOnly = false, baseline }: {
  value: string;
  lang: 'markdown' | 'yaml' | 'text';
  onChange: (next: string) => void;
  readOnly?: boolean;
  /** committed content — enables the changed-line gutter */
  baseline?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const baselineRef = useRef(baseline);
  baselineRef.current = baseline;

  useEffect(() => {
    if (!host.current) return;
    const language = lang === 'markdown' ? [markdown()] : lang === 'yaml' ? [yaml()] : [];
    const view = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          highlightActiveLine(),
          history(),
          keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
          ...language,
          cmTheme,
          cmHighlight,
          EditorState.readOnly.of(readOnly),
          ...changeGutter(() => baselineRef.current),
          EditorView.lineWrapping,
          EditorView.updateListener.of((u) => {
            if (u.docChanged) onChangeRef.current(u.state.doc.toString());
          }),
        ],
      }),
    });
    viewRef.current = view;
    return () => view.destroy();
    // recreate only when the file identity changes (value prop is initial-only)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lang, readOnly]);

  // external value change (new file loaded / draft reset)
  useEffect(() => {
    const view = viewRef.current;
    if (view && view.state.doc.toString() !== value) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
    }
  }, [value]);

  // (re)compute gutter markers when the baseline arrives or changes
  useEffect(() => {
    const view = viewRef.current;
    if (!view || baseline === undefined) return;
    view.dispatch({ effects: setChangeMarkers.of(computeMarkers(view.state, baseline)) });
  }, [baseline]);

  return <div ref={host} style={{ border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden', background: 'var(--surface)' }} />;
}
